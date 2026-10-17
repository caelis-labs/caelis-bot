"""Release-package guards run without executing any downloaded code."""
import hashlib
import importlib.util
import io
import json
import pathlib
import tarfile
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('caelis_runtime', pathlib.Path(__file__).with_name('caelis-runtime.py'))
runtime = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runtime)


class ReleaseGuards(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = pathlib.Path(self.temp.name)
        self.bundle = self.root / 'Bot.app'
        self.manifest = {'version': 'v1.2.3', 'commit': 'verified-source', 'team': 'TESTTEAM01',
                         'targets': {'darwin-arm64': {'artifact': 'caelis.tar.gz', 'sha256': ''}}}
        self.patch = patch.object(runtime, 'ROOT', self.root)
        self.patch.start()
        self.addCleanup(self.patch.stop)

    def archive(self, symlink=False):
        cache = self.root / '.cache/caelis-runtime/v1.2.3'
        cache.mkdir(parents=True)
        archive = cache / 'caelis.tar.gz'
        with tarfile.open(archive, 'w:gz') as tar:
            for name in ['caelis', 'LICENSE']:
                entry = tarfile.TarInfo(name)
                if name == 'caelis' and symlink:
                    entry.type = tarfile.SYMTYPE
                    entry.linkname = '/outside'
                    tar.addfile(entry)
                else:
                    entry.size = 7
                    tar.addfile(entry, io.BytesIO(b'fixture'))
        self.manifest['targets']['darwin-arm64']['sha256'] = hashlib.sha256(archive.read_bytes()).hexdigest()
        path = self.root / 'resources/caelis-runtime/release.json'
        path.parent.mkdir(parents=True)
        path.write_text(json.dumps(self.manifest))
        return archive

    def test_corrupt_cache_is_rejected_before_execution(self):
        self.archive().write_bytes(b'corrupt')
        with patch.object(runtime.subprocess, 'run') as run:
            with self.assertRaisesRegex(ValueError, 'checksum mismatch'):
                runtime.stage(self.bundle)
            run.assert_not_called()

    def test_symlink_member_is_rejected_before_execution(self):
        self.archive(symlink=True)
        with patch.object(runtime.subprocess, 'run') as run:
            with self.assertRaisesRegex(ValueError, 'Invalid Caelis release member'):
                runtime.stage(self.bundle)
            run.assert_not_called()

    def test_release_identity_mismatch_never_installs_helper(self):
        self.archive()
        wrong = json.dumps({'version': 'v1.2.3', 'commit': 'different', 'build_kind': 'release'}).encode()
        with patch.object(runtime.subprocess, 'run'), patch.object(runtime.subprocess, 'check_output', return_value=wrong):
            with self.assertRaisesRegex(ValueError, 'identity mismatch'):
                runtime.stage(self.bundle)
        self.assertFalse((self.bundle / 'Contents/Helpers/caelis').exists())


class WindowsReleaseGuards(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='Windows runtime 中文 ')
        self.addCleanup(self.temp.cleanup)
        self.root = pathlib.Path(self.temp.name)
        self.bundle = self.root / 'Bot Preview'
        self.manifest = {'version': 'v1.2.3', 'commit': 'verified-source', 'targets': {
            'windows-amd64': {'artifact': 'caelis-windows.tar.gz', 'sha256': '',
                              'binary_sha256': hashlib.sha256(b'fixture-exe').hexdigest()}}}
        self.patch = patch.object(runtime, 'ROOT', self.root)
        self.patch.start()
        self.addCleanup(self.patch.stop)

    def archive(self, redirect=False):
        cache = self.root / '.cache/caelis-runtime/v1.2.3'
        cache.mkdir(parents=True)
        archive = cache / 'caelis-windows.tar.gz'
        with tarfile.open(archive, 'w:gz') as tar:
            for name, body in [('caelis.exe', b'fixture-exe'), ('LICENSE', b'license')]:
                entry = tarfile.TarInfo(name)
                if redirect and name == 'caelis.exe':
                    entry.type, entry.linkname = tarfile.LNKTYPE, '../outside'
                    tar.addfile(entry)
                else:
                    entry.size = len(body)
                    tar.addfile(entry, io.BytesIO(body))
        self.manifest['targets']['windows-amd64']['sha256'] = hashlib.sha256(archive.read_bytes()).hexdigest()
        path = self.root / 'resources/caelis-runtime/release.json'
        path.parent.mkdir(parents=True)
        path.write_text(json.dumps(self.manifest))
        return archive

    def test_valid_stage_and_tamper_rejection(self):
        self.archive()
        version = json.dumps({'version': 'v1.2.3', 'commit': 'verified-source', 'build_kind': 'release'}).encode()
        with patch.object(runtime.subprocess, 'check_output', return_value=version):
            runtime.stage_windows(self.bundle)
            runtime.verify_windows(self.bundle)
            (self.bundle / 'Helpers/caelis.exe').write_bytes(b'tampered')
            with self.assertRaisesRegex(ValueError, 'checksum mismatch'):
                runtime.verify_windows(self.bundle)
        self.assertEqual((self.bundle / 'Resources/Caelis-Runtime-LICENSE').read_bytes(), b'license')

    def test_corrupt_download_rejected_before_execution(self):
        self.archive().write_bytes(b'corrupt')
        with patch.object(runtime.subprocess, 'check_output') as run:
            with self.assertRaisesRegex(ValueError, 'checksum mismatch'):
                runtime.stage_windows(self.bundle)
            run.assert_not_called()

    def test_redirected_member_rejected_before_execution(self):
        self.archive(redirect=True)
        with patch.object(runtime.subprocess, 'check_output') as run:
            with self.assertRaisesRegex(ValueError, 'Invalid Caelis release member'):
                runtime.stage_windows(self.bundle)
            run.assert_not_called()


if __name__ == '__main__':
    unittest.main()
