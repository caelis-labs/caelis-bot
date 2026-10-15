#!/usr/bin/env python3
"""Verify the pinned DTW release and stage its complete portable plugin."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

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
    if target not in PIN.get("targets", {}):
        raise ValueError(f"Desktop World target unsupported by release pin: {target}")
    spec = PIN["targets"][target]
    artifact = cache / spec["artifact"]
    checked(artifact, spec["sha256"])
    build_path = cache / f"BUILD-{target}.json"
    checked(build_path, spec["build_sha256"])
    build = json.loads(build_path.read_text())
    for key, want in {"tag": PIN["version"], "source_revision": PIN["revision"],
                      "cua_revision": PIN["cua_revision"], "platform": target,
                      "artifact_sha256": spec["sha256"],
                      "adapter_sha256": spec["binary_sha256"],
                      "worker_sha256": spec["worker_sha256"],
                      "license_inventory_sha256": spec["license_inventory_sha256"]}.items():
        if build.get(key) != want:
            raise ValueError(f"Desktop World BUILD mismatch: {key}")
    if build.get("license_packages") != spec["license_packages"] or len(build.get("mpl_source_packages", [])) < 6:
        raise ValueError("Desktop World BUILD licensing inventory incomplete")
    signing = build.get("signing", {})
    if target == "darwin-arm64":
        notary = signing.get("notarization", {})
        if signing.get("type") != "Developer ID Application" or notary.get("status") != "Accepted" or notary.get("team_id") != spec["signing_team"] or not notary.get("embedded_worker_matches_signed_worker"):
            raise ValueError("Desktop World upstream signing provenance mismatch")
    elif signing.get("type") != "unsigned":
        raise ValueError("Desktop World Windows signing provenance changed")
    return artifact, spec


def verify_upstream_signatures(paths, team):
    for path in paths:
        subprocess.run(["codesign", "--verify", "--strict", str(path)], check=True)
        details = subprocess.run(["codesign", "-dvv", str(path)], check=True, capture_output=True, text=True).stderr
        if f"TeamIdentifier={team}\n" not in details or "Authority=Developer ID Application:" not in details or "runtime" not in details or "Timestamp=" not in details:
            raise ValueError("Desktop World upstream Developer ID signature mismatch")


def export_package(artifact, dest, spec, target):
    # Never execute unverified downloads. macOS verifies the upstream identity
    # before export, and both exported executables again before Bot re-signs.
    if target == "darwin-arm64":
        artifact.chmod(0o755)
        verify_upstream_signatures([artifact], spec["signing_team"])
    dest.parent.mkdir(parents=True, exist_ok=True)
    if dest.is_symlink():
        raise ValueError("Desktop World destination is a symlink")
    with tempfile.TemporaryDirectory(prefix=".dtw-stage-", dir=dest.parent) as directory:
        staged = Path(directory) / "desktop-world"  # --to requires a new path
        subprocess.run([str(artifact.resolve()), "install", "plugin", "--to", str(staged.resolve())], check=True, timeout=60, stdout=subprocess.DEVNULL)
        verify_package(staged, spec, target)
        if target == "darwin-arm64":
            verify_upstream_signatures([staged / "bin/dtw", staged / "libexec/cua-driver"], spec["signing_team"])
        # Preserve a previous valid package when export/validation fails.
        if dest.exists():
            shutil.rmtree(dest)
        staged.rename(dest)


def verify_package(dest, spec, target):
    expected = {}
    for line in (dest / "SHA256SUMS").read_text().splitlines():
        hash_, name = line.split(maxsplit=1)
        name = name.removeprefix("./")
        parts = name.split("/")
        if not parts or any(part in ("", ".", "..") for part in parts) or name.startswith("/") or "\\" in name or ":" in name or name in expected:
            raise ValueError("Invalid Desktop World package checksum entry")
        expected[name] = hash_
    files = {f.relative_to(dest).as_posix(): f for f in dest.rglob("*") if f.is_file()}
    if any(f.is_symlink() or not (f.is_file() or f.is_dir()) for f in dest.rglob("*")):
        raise ValueError("Desktop World package has unsupported member")
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
                 "LICENSING/DEPENDENCIES.json",
                 "LICENSING/rust-runtime-licenses/OFL-1.1.txt",
                 "LICENSING/licenses/cursor-overlay-0.34.0/assets/Inter-OFL.txt",
                 "CUA-MIT-LICENSE.txt", "LICENSE", "NOTICE"):
        if name not in files:
            raise ValueError(f"Desktop World missing {name}")
    if target == "darwin-arm64":
        for name in ("bin/dtw", "libexec/cua-driver"):
            if not os.access(dest / name, os.X_OK):
                raise ValueError(f"Desktop World executable mode missing: {name}")
    manifest = json.loads((dest / "plugin.json").read_text())
    mcp = json.loads((dest / "mcp.json").read_text())
    command = "./bin/dtw.exe" if target == "windows-amd64" else "./bin/dtw"
    if manifest.get("name") != "desktop-world" or manifest.get("version") != PIN["version"].removeprefix("v") or mcp.get("mcpServers", {}).get("desktop-world", {}).get("command") != command:
        raise ValueError("Desktop World portable plugin contract changed")
    print(f"Verified complete Desktop World {PIN['version']} {target}: {len(files)} files")


def inventory(dest, target):
    if target not in PIN.get("targets", {}):
        raise ValueError(f"Desktop World target unsupported by release pin: {target}")
    # Bot-owned presentation overlay, applied after upstream verification and
    # native signing; included in the final immutable package inventory.
    assets = dest / "assets"
    assets.mkdir(exist_ok=True)
    shutil.copyfile(ROOT / "resources/desktop-world/assets/logo.png", assets / "logo.png")
    manifest_path = dest / "plugin.json"
    manifest = json.loads(manifest_path.read_text())
    manifest.setdefault("extensions", {})["dev.caelis.bot"] = {"icon": "assets/logo.png"}
    manifest_path.write_text(json.dumps(manifest, indent=2) + "\n")
    files = {f.relative_to(dest).as_posix(): digest(f) for f in dest.rglob("*") if f.is_file()}
    entry = {"ID": "desktop-world", "Title": "Computer Use", "titleZh": "电脑操控", "Version": PIN["version"].removeprefix("v"),
             "Description": "Operate desktop apps through one native CUA exec tool",
             "Source": f"bundled:desktop-world/{PIN['version']}", "builtin": True,
             "UpstreamIntegrity": PIN["targets"][target]["sha256"],
             "executables": ["bin/dtw", "libexec/cua-driver"] if target == "darwin-arm64" else ["bin/dtw.exe", "libexec/cua-driver.exe"],
             "display": {"publisher": "Caelis Labs", "skills": {"desktop-world": {"nameEn": "Computer Use", "nameZh": "电脑操控", "descriptionEn": "Inspect and operate an authorized desktop app.", "descriptionZh": "检查并操作已授权的桌面应用。"}}, "servers": {"desktop-world": {"nameEn": "Computer Use", "nameZh": "电脑操控", "descriptionEn": "One native CUA exec tool.", "descriptionZh": "单一原生 CUA exec 工具。"}}},
             "Files": files}
    (dest.parent / "desktop-world-inventory.json").write_text(json.dumps(entry, indent=2, sort_keys=True) + "\n")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("operation", choices=("stage", "inventory", "verify"))
    parser.add_argument("target", choices=("darwin-arm64", "windows-amd64"))
    parser.add_argument("bundle_plugins", type=Path)
    parser.add_argument("--cache", type=Path, default=ROOT / ".cache/desktop-world-runtime" / PIN["version"])
    args = parser.parse_args()
    dest = args.bundle_plugins / "desktop-world"
    if args.operation == "stage":
        artifact, spec = verify_release(args.cache, args.target)
        export_package(artifact, dest, spec, args.target)
    elif args.operation == "inventory":
        inventory(dest, args.target)
    else:
        artifact, spec = verify_release(args.cache, args.target)
        verify_package(dest, spec, args.target)


if __name__ == "__main__":
    main()
