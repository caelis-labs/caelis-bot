"""Filesystem/user-service fixture only; never invokes the host's systemctl."""
import importlib.util
import json
import pathlib
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

spec=importlib.util.spec_from_file_location('installer',pathlib.Path(__file__).parents[1]/'deploy/headless/install.py')
installer=importlib.util.module_from_spec(spec);spec.loader.exec_module(installer)

class InstallerTests(unittest.TestCase):
    def test_idempotence_explicit_upgrade_and_profile_preservation(self):
        with tempfile.TemporaryDirectory(prefix='caelis-installer-fixture-') as temp:
            home=pathlib.Path(temp);bundle=home/'bundle';bundle.mkdir()
            (bundle/'caelisbot-launcher-1.2.3-linux-arm64').write_bytes(b'fixture launcher')
            calls=[]
            def run(args,**kwargs):
                calls.append(args)
                if len(args)>1 and args[1]=='init':
                    root=pathlib.Path(args[args.index('--root')+1]);profile=pathlib.Path(args[args.index('--data-dir')+1])
                    root.mkdir();profile.mkdir();(profile/'history').write_text('original')
                    (root/'installation.json').write_text(json.dumps({'schema':1,'qa':False,'profile':str(profile)}))
                return subprocess.CompletedProcess(args,0,b'',b'')
            argv=['install.py','--bundle',str(bundle),'--platform','linux-arm64','--version','1.2.3']
            with patch.object(sys,'argv',argv),patch.object(installer.os,'getuid',return_value=1000),patch.object(installer.pathlib.Path,'home',return_value=home),patch.dict(installer.os.environ,{'XDG_DATA_HOME':str(home/'data'),'XDG_CONFIG_HOME':str(home/'config')}),patch.object(installer.shutil,'which',return_value='/fixture/tool'),patch.object(installer.subprocess,'run',side_effect=run):
                installer.install();installer.install()
                self.assertEqual(sum(len(c)>1 and c[1]=='init' for c in calls),1)
                sys.argv=argv+['--upgrade'];installer.install()
                self.assertEqual(sum(len(c)>1 and c[1]=='upgrade' for c in calls),1)
            self.assertEqual((home/'data/caelisbot/history').read_text(),'original')
            unit=(home/'config/systemd/user/caelisbot.service').read_text()
            self.assertIn('UMask=0077',unit);self.assertIn('caelisbot-launcher',unit)
            self.assertFalse(any(c[0]=='sudo' or 'linger' in c for c in calls))
    def test_missing_user_manager_refuses_without_creating_files(self):
        with patch.object(sys,'argv',['install.py','--bundle','/unused','--platform','linux-amd64','--version','1.2.3']),patch.object(installer.os,'getuid',return_value=1000),patch.object(installer.shutil,'which',return_value='/fixture/tool'),patch.object(installer.subprocess,'run',return_value=subprocess.CompletedProcess([],1,b'',b'')):
            with self.assertRaisesRegex(SystemExit,'systemd user manager'):
                installer.install()

if __name__=='__main__': unittest.main()
