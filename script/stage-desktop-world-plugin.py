#!/usr/bin/env python3
"""Verify the pinned DTW release and stage its complete portable plugin."""

import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import shutil
import tarfile
import zipfile

ROOT = Path(__file__).resolve().parent.parent
PIN = json.loads((ROOT / "resources/desktop-world/release.json").read_text())


def digest(path):
    h = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            h.update(chunk)
    return h.hexdigest()


def checked(path, expected):
    if not path.is_file() or digest(path) != expected:
        raise ValueError(f"Desktop World checksum mismatch: {path.name}")


def verify_release(cache, target):
    spec = PIN["targets"][target]
    archive = cache / spec["archive"]
    checked(archive, spec["sha256"])
    build_path = cache / f"BUILD-{target}.json"
    checked(build_path, spec["build_sha256"])
    build = json.loads(build_path.read_text())
    for key, want in {"tag": PIN["version"], "source_revision": PIN["revision"],
                      "cua_revision": PIN["cua_revision"], "platform": target,
                      "archive_sha256": spec["sha256"],
                      "adapter_sha256": spec["binary_sha256"],
                      "worker_sha256": spec["worker_sha256"],
                      "license_inventory_sha256": spec["license_inventory_sha256"]}.items():
        if build.get(key) != want:
            raise ValueError(f"Desktop World BUILD mismatch: {key}")
    if build.get("license_packages", 0) < 300 or len(build.get("mpl_source_packages", [])) < 6:
        raise ValueError("Desktop World BUILD licensing inventory incomplete")
    for name, expected in (("ACCEPTANCE.json", PIN["acceptance_sha256"]),
                           (PIN["source"]["archive"], PIN["source"]["sha256"])):
        checked(cache / name, expected)
    acceptance = json.loads((cache / "ACCEPTANCE.json").read_text())
    if acceptance.get("release") != PIN["version"] or acceptance.get("cua_revision") != PIN["cua_revision"]:
        raise ValueError("Desktop World acceptance tag mismatch")
    return archive, spec


def extract_package(archive, dest):
    if dest.exists():
        shutil.rmtree(dest)
    dest.mkdir(parents=True)
    if archive.suffix == ".zip":
        with zipfile.ZipFile(archive) as z:
            entries = [(item.filename, item.is_dir(), lambda i=item: z.open(i)) for item in z.infolist()]
            for name, is_dir, opener in entries:
                copy_entry(name, is_dir, opener, dest)
    else:
        with tarfile.open(archive) as t:
            for member in t:
                if not (member.isdir() or member.isfile()):
                    raise ValueError("Desktop World archive has unsupported member")
                copy_entry(member.name, member.isdir(), lambda m=member: t.extractfile(m), dest)


def copy_entry(name, is_dir, opener, dest):
    if "\\" in name or name.startswith("/"):
        raise ValueError("Desktop World archive path escapes package")
    path = PurePosixPath(name)
    if not path.parts or path.parts[0] != "desktop-world" or any(p in ("", ".", "..") for p in path.parts):
        raise ValueError("Desktop World archive path escapes package")
    relative = Path(*path.parts[1:])
    if str(relative) == ".":
        return
    target = dest / relative
    if is_dir:
        target.mkdir(parents=True, exist_ok=True)
        return
    target.parent.mkdir(parents=True, exist_ok=True)
    with opener() as src, target.open("wb") as out:
        shutil.copyfileobj(src, out)
    target.chmod(0o755 if relative.as_posix() in ("bin/dtw", "libexec/cua-driver", "bin/dtw.exe", "libexec/cua-driver.exe") else 0o644)


def verify_package(dest, spec, target):
    expected = {}
    for line in (dest / "SHA256SUMS").read_text().splitlines():
        hash_, name = line.split(maxsplit=1)
        name = name.removeprefix("./")
        parts = PurePosixPath(name).parts
        if not parts or any(part in ("", ".", "..") for part in parts) or name.startswith("/") or "\\" in name or name in expected:
            raise ValueError("Invalid Desktop World package checksum entry")
        expected[name] = hash_
    files = {f.relative_to(dest).as_posix(): f for f in dest.rglob("*") if f.is_file()}
    if set(files) != set(expected) | {"SHA256SUMS"}:
        raise ValueError("Desktop World package file inventory differs")
    for name, hash_ in expected.items():
        checked(files[name], hash_)
    if digest(dest / spec["binary"]) != spec["binary_sha256"]:
        raise ValueError("Desktop World adapter differs from BUILD")
    worker = "libexec/cua-driver.exe" if target == "windows-amd64" else "libexec/cua-driver"
    checked(dest / worker, spec["worker_sha256"])
    checked(dest / "LICENSING/DEPENDENCIES.json", spec["license_inventory_sha256"])
    for name in ("plugin.json", "mcp.json", "skills/desktop-world/SKILL.md",
                 "skills/desktop-world/api.d.ts", "skills/desktop-world/RECIPES.md",
                 "LICENSING/MPL-SOURCES.tar.gz", "LICENSING/THIRD-PARTY.md",
                 "CUA-MIT-LICENSE.txt", "LICENSE", "NOTICE"):
        if name not in files:
            raise ValueError(f"Desktop World missing {name}")
    if not any("OFL" in name for name in files):
        raise ValueError("Desktop World font licenses missing")
    manifest = json.loads((dest / "plugin.json").read_text())
    mcp = json.loads((dest / "mcp.json").read_text())
    command = "./bin/dtw.exe" if target == "windows-amd64" else "./bin/dtw"
    if manifest.get("name") != "desktop-world" or manifest.get("version") != "0.1.0-rc.4" or mcp.get("mcpServers", {}).get("desktop-world", {}).get("command") != command:
        raise ValueError("Desktop World portable plugin contract changed")
    print(f"Verified complete Desktop World {PIN['version']} {target}: {len(files)} files")


def inventory(dest, target):
    files = {f.relative_to(dest).as_posix(): digest(f) for f in dest.rglob("*") if f.is_file()}
    entry = {"ID": "desktop-world", "Title": "Desktop World", "Version": "0.1.0-rc.4",
             "Description": "Operate desktop apps through one native CUA exec tool",
             "Source": f"bundled:desktop-world/{PIN['version']}", "builtin": True,
             "UpstreamIntegrity": PIN["targets"][target]["sha256"],
             "executables": ["bin/dtw", "libexec/cua-driver"] if target == "darwin-arm64" else ["bin/dtw.exe", "libexec/cua-driver.exe"],
             "display": {"publisher": "Caelis Labs", "skills": {"desktop-world": {"nameEn": "Desktop World", "nameZh": "Desktop World", "descriptionEn": "Inspect and operate an authorized desktop app.", "descriptionZh": "检查并操作已授权的桌面应用。"}}, "servers": {"desktop-world": {"nameEn": "Desktop World", "nameZh": "Desktop World", "descriptionEn": "One native CUA exec tool.", "descriptionZh": "单一原生 CUA exec 工具。"}}},
             "Files": files}
    (dest.parent / "desktop-world-inventory.json").write_text(json.dumps(entry, indent=2, sort_keys=True) + "\n")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("operation", choices=("stage", "inventory", "verify"))
    parser.add_argument("target", choices=("darwin-arm64", "windows-amd64"))
    parser.add_argument("bundle_plugins", type=Path)
    parser.add_argument("--cache", type=Path, default=ROOT / ".cache/desktop-world-runtime")
    args = parser.parse_args()
    dest = args.bundle_plugins / "desktop-world"
    if args.operation == "stage":
        archive, spec = verify_release(args.cache, args.target)
        extract_package(archive, dest)
        verify_package(dest, spec, args.target)
    elif args.operation == "inventory":
        inventory(dest, args.target)
    else:
        archive, spec = verify_release(args.cache, args.target)
        verify_package(dest, spec, args.target)


if __name__ == "__main__":
    main()
