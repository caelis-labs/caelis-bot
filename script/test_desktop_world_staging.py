"""Build-only contract tests; no native worker, network, or GUI is started."""
import copy
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
import zipfile
import tarfile
import io
from unittest.mock import patch

module_spec = importlib.util.spec_from_file_location('staging', Path(__file__).with_name('stage-desktop-world-plugin.py'))
staging = importlib.util.module_from_spec(module_spec)
module_spec.loader.exec_module(staging)


class StagingTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='dtw contract with spaces ')
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.package = self.root / 'fixture'
        names = ['plugin.json', 'mcp.json', 'bin/dtw.exe', 'libexec/cua-driver.exe',
                 'skills/desktop-world/SKILL.md', 'skills/desktop-world/api.d.ts', 'skills/desktop-world/RECIPES.md',
                 'skills/desktop-world/BROWSER.md',
                 'LICENSING/MPL-SOURCES.tar.gz', 'LICENSING/THIRD-PARTY.md', 'LICENSING/DEPENDENCIES.json',
                 'LICENSING/rust-runtime-licenses/OFL-1.1.txt',
                 f"LICENSING/licenses/cursor-overlay-{staging.PIN['cua_version']}/assets/Inter-OFL.txt", 'CUA-MIT-LICENSE.txt', 'LICENSE', 'NOTICE',
                 '.claude-plugin/plugin.json', '.dtw-bundle-sha256']
        for name in names:
            path = self.package / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(name + '\n')
        (self.package / 'plugin.json').write_text(json.dumps({'name':'desktop-world','version':staging.PIN['version'][1:]}))
        (self.package / 'mcp.json').write_text(json.dumps({'mcpServers':{'desktop-world':{'command':'./bin/dtw.exe'}}}))
        self.checksums()
        self.spec = copy.deepcopy(staging.PIN['targets']['windows-amd64'])
        for key, name in [('binary_sha256','bin/dtw.exe'),('worker_sha256','libexec/cua-driver.exe'),('license_inventory_sha256','LICENSING/DEPENDENCIES.json')]:
            self.spec[key] = staging.digest(self.package / name)
        self.artifact = self.root / self.spec['artifact']
        with zipfile.ZipFile(self.artifact, 'w') as archive:
            for path in self.package.rglob('*'):
                if path.is_file(): archive.write(path, path.relative_to(self.package).as_posix())
        self.spec['sha256'] = staging.digest(self.artifact)

    def checksums(self):
        files = sorted(p for p in self.package.rglob('*') if p.is_file() and p.name != 'SHA256SUMS')
        (self.package / 'SHA256SUMS').write_text(''.join(staging.digest(p) + '  ' + p.relative_to(self.package).as_posix() + '\n' for p in files))

    def test_presentation_overlay_is_in_final_inventory(self):
        staging.verify_package(self.package, self.spec, 'windows-amd64')
        worker_hash = staging.digest(self.package / 'libexec/cua-driver.exe')
        staging.inventory(self.package, 'windows-amd64')
        manifest = json.loads((self.package / 'plugin.json').read_text())
        self.assertEqual(manifest['extensions']['dev.caelis.bot']['icon'], 'assets/logo.png')
        entry = json.loads((self.root / 'desktop-world-inventory.json').read_text())
        for name in ('plugin.json', 'assets/logo.png'):
            self.assertEqual(entry['Files'][name], staging.digest(self.package / name))
        self.assertEqual(worker_hash, staging.digest(self.package / 'libexec/cua-driver.exe'))

    def test_complete_export_including_hidden_files(self):
        staging.verify_package(self.package, self.spec, 'windows-amd64')
        destination = self.root / 'Plugins' / 'desktop-world'
        destination.mkdir(parents=True)
        (destination / 'old').write_text('keep until verified')
        with patch.object(staging.subprocess, 'run', side_effect=AssertionError('archive must not execute')):
            staging.export_package(self.artifact, destination, self.spec, 'windows-amd64')
        self.assertTrue((destination / '.claude-plugin/plugin.json').is_file())
        self.assertFalse((destination / 'old').exists())

    def test_failed_export_preserves_previous_package(self):
        destination = self.root / 'Plugins' / 'desktop-world'
        destination.mkdir(parents=True)
        (destination / 'old').write_text('unchanged')
        with patch.object(staging, 'extract_plugin', side_effect=ValueError('corrupt archive')):
            with self.assertRaises(ValueError):
                staging.export_package(self.artifact, destination, self.spec, 'windows-amd64')
        self.assertEqual((destination / 'old').read_text(), 'unchanged')
        self.assertEqual(list(destination.parent.iterdir()), [destination])

    def test_unexpected_hidden_or_corrupt_file_rejected(self):
        path = self.package / '.extra'
        path.write_text('unexpected')
        with self.assertRaises(ValueError): staging.verify_package(self.package, self.spec, 'windows-amd64')
        path.unlink()
        (self.package / 'bin/dtw.exe').write_text('tampered')
        with self.assertRaises(ValueError): staging.verify_package(self.package, self.spec, 'windows-amd64')

    def test_missing_required_skill_even_with_valid_checksums_rejected(self):
        for name in ('api.d.ts', 'BROWSER.md'):
            with self.subTest(name=name):
                path = self.package / 'skills/desktop-world' / name
                body = path.read_bytes()
                path.unlink()
                self.checksums()
                with self.assertRaisesRegex(ValueError, 'missing'):
                    staging.verify_package(self.package, self.spec, 'windows-amd64')
                path.write_bytes(body)

    def test_missing_pinned_font_license_even_with_valid_checksums_rejected(self):
        (self.package / f"LICENSING/licenses/cursor-overlay-{staging.PIN['cua_version']}/assets/Inter-OFL.txt").unlink()
        self.checksums()
        with self.assertRaisesRegex(ValueError, 'Inter-OFL'):
            staging.verify_package(self.package, self.spec, 'windows-amd64')

    def test_checksum_path_cannot_escape_on_either_platform(self):
        for name in ['../escape', '/escape', 'C:/escape', 'bin//dtw.exe', 'bin/./dtw.exe', 'bin\\dtw.exe']:
            with self.subTest(name=name):
                (self.package / 'SHA256SUMS').write_text('0' * 64 + '  ' + name + '\n')
                with self.assertRaises(ValueError): staging.verify_package(self.package, self.spec, 'windows-amd64')

    @unittest.skipIf(os.name == 'nt', 'Windows symlink privilege is not presumed')
    def test_symlink_member_rejected(self):
        worker = self.package / 'libexec/cua-driver.exe'
        external = self.root / 'external-worker'
        shutil.copyfile(worker, external)
        worker.unlink()
        worker.symlink_to(external)
        with self.assertRaises(ValueError): staging.verify_package(self.package, self.spec, 'windows-amd64')

    def test_download_and_build_provenance_checked_before_execution(self):
        build = {'tag':staging.PIN['version'], 'source_revision':staging.PIN['revision'],
                 'cua_revision':staging.PIN['cua_revision'], 'platform':'windows-amd64',
                 'artifact_sha256':self.spec['standalone_sha256'],
                 'plugin':{'asset':self.spec['artifact'],'sha256':self.spec['sha256'],'adapter_sha256':self.spec['binary_sha256']},
                 'worker_sha256':self.spec['worker_sha256'], 'license_inventory_sha256':self.spec['license_inventory_sha256'],
                 'license_packages':self.spec['license_packages'], 'mpl_source_packages':['fixture'] * 6,
                 'signing':{'type':'unsigned'}}
        path = self.root / 'BUILD-windows-amd64.json'
        path.write_text(json.dumps(build))
        self.spec['build_sha256'] = staging.digest(path)
        pin = copy.deepcopy(staging.PIN)
        pin['targets']['windows-amd64'] = self.spec
        with patch.object(staging, 'PIN', pin):
            staging.verify_release(self.root, 'windows-amd64')
            build['artifact_sha256'] = '0' * 64
            path.write_text(json.dumps(build))
            self.spec['build_sha256'] = staging.digest(path)
            with self.assertRaisesRegex(ValueError, 'BUILD mismatch'):
                staging.verify_release(self.root, 'windows-amd64')
            self.artifact.write_text('tampered')
            with self.assertRaisesRegex(ValueError, 'checksum mismatch'):
                staging.verify_release(self.root, 'windows-amd64')

    def test_archive_traversal_and_symlink_are_rejected(self):
        for name in ['../escape', '/escape', 'C:/escape', 'bin\\dtw.exe']:
            with self.subTest(name=name):
                malicious = self.root / 'bad.zip'
                # ZipInfo normalizes Windows separators in its constructor; preserve
                # the malicious on-disk spelling so every host tests the same bytes.
                member = zipfile.ZipInfo('placeholder')
                member.filename = name
                with zipfile.ZipFile(malicious, 'w') as archive: archive.writestr(member, 'bad')
                target = self.root / 'unpacked'
                with self.assertRaises(ValueError): staging.extract_plugin(malicious, target)
                shutil.rmtree(target)
        malicious = self.root / 'bad.zip'
        with zipfile.ZipFile(malicious, 'w') as archive:
            member = zipfile.ZipInfo('link'); member.external_attr = 0o120777 << 16
            archive.writestr(member, '/outside')
        with self.assertRaises(ValueError): staging.extract_plugin(malicious, self.root / 'unpacked')

    def test_zip_normalization_cannot_hide_unsafe_raw_names(self):
        for raw in ['bin\\dtw.exe', 'bin/dtw.exe\x00hidden']:
            with self.subTest(raw=raw):
                member = zipfile.ZipInfo('bin/dtw.exe')
                member.orig_filename = raw
                member.file_size = 3
                target = self.root / 'normalized-unpacked'
                with patch.object(zipfile.ZipFile, 'infolist', return_value=[member]):
                    with self.assertRaisesRegex(ValueError, 'Invalid Desktop World archive member'):
                        staging.extract_plugin(self.artifact, target)
                shutil.rmtree(target)

    def test_tar_preserves_executable_mode_and_rejects_hardlinks(self):
        archive_path = self.root / 'plugin.tar.gz'
        with tarfile.open(archive_path, 'w:gz') as archive:
            member = tarfile.TarInfo('bin/dtw'); member.mode = 0o755; member.size = 3
            archive.addfile(member, io.BytesIO(b'dtw'))
        unpacked = self.root / 'tar-unpacked'
        staging.extract_plugin(archive_path, unpacked)
        self.assertEqual((unpacked / 'bin/dtw').read_bytes(), b'dtw')
        if os.name != 'nt': self.assertEqual((unpacked / 'bin/dtw').stat().st_mode & 0o777, 0o755)
        with tarfile.open(archive_path, 'w:gz') as archive:
            member = tarfile.TarInfo('link'); member.type = tarfile.LNKTYPE; member.linkname = '../outside'
            archive.addfile(member)
        with self.assertRaises(ValueError): staging.extract_plugin(archive_path, self.root / 'bad-tar')

    def test_upstream_adhoc_signature_is_not_developer_id(self):
        def run(args, **kwargs):
            return subprocess.CompletedProcess(args, 0, stderr='Signature=adhoc\n')
        with patch.object(staging.subprocess, 'run', side_effect=run):
            with self.assertRaisesRegex(ValueError, 'Developer ID'):
                staging.verify_upstream_signatures([self.artifact], 'ABCDE12345')


if __name__ == '__main__':
    unittest.main()
