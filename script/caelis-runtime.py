#!/usr/bin/env python3
"""Stage one pinned public CLI. No Core imports, credentials, or runtime Store."""
import hashlib
import json
import pathlib
import plistlib
import shutil
import subprocess
import sys
import tarfile
import tempfile
import urllib.request

ROOT = pathlib.Path(__file__).resolve().parent.parent


def stage(bundle):
    manifest = json.loads((ROOT / 'resources/caelis-runtime/release.json').read_text())
    target = manifest['targets']['darwin-arm64']
    cache = ROOT / '.cache/caelis-runtime' / manifest['version']
    cache.mkdir(parents=True, exist_ok=True)
    archive = cache / target['artifact']
    if not archive.exists():
        with tempfile.NamedTemporaryFile(dir=cache) as download:
            subprocess.run(['curl', '--fail', '--location', '--retry', '3', '--max-time', '180',
                            'https://github.com/caelis-labs/caelis/releases/download/' + manifest['version'] + '/' + target['artifact'],
                            '--output', download.name], check=True)
            if hashlib.sha256(pathlib.Path(download.name).read_bytes()).hexdigest() != target['sha256']:
                raise ValueError('Caelis archive checksum mismatch')
            shutil.copyfile(download.name, archive)
    if hashlib.sha256(archive.read_bytes()).hexdigest() != target['sha256']:
        raise ValueError('Cached Caelis archive checksum mismatch; remove the damaged cache file')
    with tempfile.TemporaryDirectory(dir=cache) as temporary:
        binary = pathlib.Path(temporary) / 'caelis'
        with tarfile.open(archive) as tar:
            # Extract only two fixed regular members; no links or archive paths.
            for name in ['caelis', 'LICENSE']:
                matches = [m for m in tar.getmembers() if m.name in (name, './' + name)]
                if len(matches) != 1 or not matches[0].isfile():
                    raise ValueError('Invalid Caelis release member: ' + name)
                (pathlib.Path(temporary) / name).write_bytes(tar.extractfile(matches[0]).read())
        binary.chmod(0o755)
        subprocess.run(['codesign', '--verify', '--strict', '--test-requirement',
                        '=anchor apple generic and certificate leaf[field.1.2.840.113635.100.6.1.13] exists and certificate leaf[subject.OU] = "' + manifest['team'] + '" and identifier "caelis"', str(binary)], check=True)
        subprocess.run(['lipo', str(binary), '-verify_arch', 'arm64'], check=True)
        check_version(binary, manifest)
        helpers = bundle / 'Contents/Helpers'
        helpers.mkdir(parents=True, exist_ok=True)
        shutil.copy2(binary, helpers / 'caelis')
        resources = bundle / 'Contents/Resources'
        shutil.copyfile(pathlib.Path(temporary) / 'LICENSE', resources / 'Caelis-Runtime-LICENSE')
        shutil.copyfile(ROOT / 'resources/caelis-runtime/release.json', resources / 'Caelis-Runtime.json')
    plist = bundle / 'Contents/Info.plist'
    data = plistlib.loads(plist.read_bytes())
    data['CaelisRuntimeVersion'] = manifest['version']
    plist.write_bytes(plistlib.dumps(data))


def check_version(binary, manifest):
    version = json.loads(subprocess.check_output([str(binary), 'version', '--format', 'json'], timeout=10))
    if version['version'] != manifest['version'] or version['commit'] != manifest['commit'] or version['build_kind'] != 'release':
        raise ValueError('Embedded CLI release identity mismatch')


def verify(bundle):
    expected = json.loads((ROOT / 'resources/caelis-runtime/release.json').read_text())
    embedded = json.loads((bundle / 'Contents/Resources/Caelis-Runtime.json').read_text())
    if embedded != expected:
        raise ValueError('Embedded CLI manifest differs from release pin')
    plist = plistlib.loads((bundle / 'Contents/Info.plist').read_bytes())
    if plist.get('CaelisRuntimeVersion') != expected['version']:
        raise ValueError('Missing or mismatched CaelisRuntimeVersion')
    binary = bundle / 'Contents/Helpers/caelis'
    if binary.is_symlink() or not binary.is_file():
        raise ValueError('Embedded CLI must be a regular file')
    check_version(binary, expected)


def stage_windows(bundle):
    manifest = json.loads((ROOT / 'resources/caelis-runtime/release.json').read_text())
    target = manifest['targets']['windows-amd64']
    cache = ROOT / '.cache/caelis-runtime' / manifest['version']
    cache.mkdir(parents=True, exist_ok=True)
    archive = cache / target['artifact']
    if not archive.exists():
        # NamedTemporaryFile cannot be reopened by another process on Windows.
        # Download closed bytes and verify before publishing or executing.
        with tempfile.TemporaryDirectory(dir=cache) as directory:
            downloaded = pathlib.Path(directory) / 'download.tar.gz'
            url = 'https://github.com/caelis-labs/caelis/releases/download/' + manifest['version'] + '/' + target['artifact']
            with urllib.request.urlopen(url, timeout=180) as response, downloaded.open('wb') as out:
                shutil.copyfileobj(response, out)
            if hashlib.sha256(downloaded.read_bytes()).hexdigest() != target['sha256']:
                raise ValueError('Caelis archive checksum mismatch')
            downloaded.replace(archive)
    if hashlib.sha256(archive.read_bytes()).hexdigest() != target['sha256']:
        raise ValueError('Cached Caelis archive checksum mismatch')
    with tempfile.TemporaryDirectory(dir=cache) as directory:
        temporary = pathlib.Path(directory)
        with tarfile.open(archive) as tar:
            for name in ['caelis.exe', 'LICENSE']:
                matches = [m for m in tar.getmembers() if m.name in (name, './' + name)]
                if len(matches) != 1 or not matches[0].isfile():
                    raise ValueError('Invalid Caelis release member: ' + name)
                (temporary / name).write_bytes(tar.extractfile(matches[0]).read())
        binary = temporary / 'caelis.exe'
        if hashlib.sha256(binary.read_bytes()).hexdigest() != target['binary_sha256']:
            raise ValueError('Caelis executable checksum mismatch')
        check_version(binary, manifest)
        helpers, resources = bundle / 'Helpers', bundle / 'Resources'
        helpers.mkdir(parents=True, exist_ok=True)
        resources.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(binary, helpers / 'caelis.exe')
        shutil.copyfile(temporary / 'LICENSE', resources / 'Caelis-Runtime-LICENSE')
        shutil.copyfile(ROOT / 'resources/caelis-runtime/release.json', resources / 'Caelis-Runtime.json')


def verify_windows(bundle):
    expected = json.loads((ROOT / 'resources/caelis-runtime/release.json').read_text())
    if json.loads((bundle / 'Resources/Caelis-Runtime.json').read_text()) != expected:
        raise ValueError('Embedded CLI manifest differs from release pin')
    binary = bundle / 'Helpers/caelis.exe'
    if binary.is_symlink() or not binary.is_file() or hashlib.sha256(binary.read_bytes()).hexdigest() != expected['targets']['windows-amd64']['binary_sha256']:
        raise ValueError('Embedded CLI executable checksum mismatch')
    if not (bundle / 'Resources/Caelis-Runtime-LICENSE').is_file():
        raise ValueError('Embedded CLI license missing')
    check_version(binary, expected)


if __name__ == '__main__':
    {'stage': stage, 'verify': verify, 'stage-windows': stage_windows, 'verify-windows': verify_windows}[sys.argv[1]](pathlib.Path(sys.argv[2]))
