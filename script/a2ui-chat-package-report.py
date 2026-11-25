#!/usr/bin/env python3
"""Measure both local macOS application archives and their frontend inputs."""

import gzip
import hashlib
import json
import os
import plistlib
import stat
import sys
import zipfile
from pathlib import Path


def file_digest(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def contents(root):
    entries = {}
    for path in root.rglob('*'):
        mode = path.lstat().st_mode
        key = path.relative_to(root).as_posix()
        if stat.S_ISLNK(mode):
            entries[key] = ('link', os.readlink(path))
        elif stat.S_ISREG(mode):
            entries[key] = ('file', path.stat().st_size, file_digest(path))
    return entries


def measure(checkout, package_dir):
    bundle = package_dir / 'Caelis Bot Dev.app'
    archive = package_dir.with_suffix('.zip')
    extracted = package_dir.parent / ('extracted-' + package_dir.name) / bundle.name
    entries = contents(bundle)
    assert entries and entries == contents(extracted), 'Archive extraction changed bundle files or links'
    files = {key: entry for key, entry in entries.items() if entry[0] == 'file'}
    result = {
        'commit': plistlib.loads((bundle / 'Contents/Info.plist').read_bytes())['CaelisSourceCommit'],
        'appLogicalBytes': sum(entry[1] for entry in files.values()),
        'appRegularFiles': len(files),
        'appSymlinks': len(entries) - len(files),
        'appSymlinkTargetBytes': sum(len(os.fsencode(entry[1])) for entry in entries.values() if entry[0] == 'link'),
        'mainExecutable': files['Contents/MacOS/caelis-bot'][1],
        'coreSHA256': files['Contents/Helpers/caelis'][2],
        'zipBytes': archive.stat().st_size,
        'zipSHA256': file_digest(archive),
        'extractedRegularFilesAndSymlinksMatch': True,
    }
    with zipfile.ZipFile(archive) as zip_file:
        result['zipMethods'] = sorted({item.compress_type for item in zip_file.infolist()})
    assets = checkout / 'frontend/dist/assets'
    for suffix in ['js', 'css']:
        payloads = [path.read_bytes() for path in assets.glob(f'*.{suffix}')]
        assert payloads, f'No built {suffix} assets in {assets}'
        result[f'{suffix}Raw'] = sum(map(len, payloads))
        result[f'{suffix}Gzip'] = sum(len(gzip.compress(data, compresslevel=6, mtime=0)) for data in payloads)
    for group in ['Contents/Frameworks/', 'Contents/Resources/']:
        result[group] = {key: entry[2] for key, entry in files.items() if key.startswith(group)}
    return result


base_checkout, base_package, poc_checkout, poc_package = map(Path, sys.argv[1:])
baseline = measure(base_checkout, base_package)
poc = measure(poc_checkout, poc_package)
same = {'core': baseline['coreSHA256'] == poc['coreSHA256']}
for group in ['Contents/Frameworks/', 'Contents/Resources/']:
    same[group] = baseline.pop(group) == poc.pop(group)
assert all(same.values()), 'Embedded Core, resources and frameworks must match'
delta = {key: poc[key]-value for key, value in baseline.items() if type(value) is int}
print(json.dumps({'baseline': baseline, 'poc': poc, 'delta': delta, 'same': same}, indent=2))
