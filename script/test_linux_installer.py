"""Filesystem/user-service fixture only; never invokes the host's systemctl."""
import importlib.util
import contextlib
import io
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
                return subprocess.CompletedProcess(args,0,'{"state":"healthy"}' if len(args)>1 and args[1]=='upgrade' else '', '')
            argv=['install.py','--bundle',str(bundle),'--platform','linux-arm64','--version','1.2.3']
            with patch.object(sys,'argv',argv),patch.object(installer.os,'getuid',return_value=1000),patch.object(installer.pathlib.Path,'home',return_value=home),patch.dict(installer.os.environ,{'XDG_DATA_HOME':str(home/'data'),'XDG_CONFIG_HOME':str(home/'config')}),patch.object(installer.shutil,'which',return_value='/fixture/tool'),patch.object(installer.subprocess,'run',side_effect=run):
                installer.install();installer.install()
                self.assertEqual(sum(len(c)>1 and c[1]=='init' for c in calls),1)
                sys.argv=argv+['--upgrade'];installer.install()
                self.assertEqual(sum(len(c)>1 and c[1]=='upgrade' for c in calls),1)
                self.assertFalse(any('stop' in c or 'restart' in c for c in calls))
                # A busy/unknown owner must leave the live service, launcher
                # and profile intact, with no stop/start fallback.
                original=(home/'.local/bin/caelisbot-launcher').read_bytes()
                (bundle/'caelisbot-launcher-1.2.3-linux-arm64').write_bytes(b'new fixture launcher')
                calls.clear()
                def busy(args,**kwargs):
                    calls.append(args)
                    if len(args)>1 and args[1]=='upgrade':
                        self.assertEqual((home/'.local/bin/caelisbot-launcher').read_bytes(),original)
                        raise subprocess.CalledProcessError(1,args)
                    return subprocess.CompletedProcess(args,0,b'',b'')
                with patch.object(installer.subprocess,'run',side_effect=busy):
                    with self.assertRaises(subprocess.CalledProcessError): installer.install()
                self.assertEqual((home/'.local/bin/caelisbot-launcher').read_bytes(),original)
                self.assertFalse(any('stop' in c or 'start' in c or 'restart' in c or 'enable' in c for c in calls))
            self.assertEqual((home/'data/caelisbot/history').read_text(),'original')
            unit=(home/'config/systemd/user/caelisbot.service').read_text()
            self.assertIn('UMask=0077',unit);self.assertIn('caelisbot-launcher',unit)
            self.assertFalse(any(c[0]=='sudo' or 'linger' in c for c in calls))
    def test_missing_user_manager_refuses_without_creating_files(self):
        with patch.object(sys,'argv',['install.py','--bundle','/unused','--platform','linux-amd64','--version','1.2.3']),patch.object(installer.os,'getuid',return_value=1000),patch.object(installer.shutil,'which',return_value='/fixture/tool'),patch.object(installer.subprocess,'run',return_value=subprocess.CompletedProcess([],1,b'',b'')):
            with self.assertRaisesRegex(SystemExit,'systemd user manager'):
                installer.install()

    def test_stopped_service_waits_for_health_before_replacing_launcher(self):
        for outcome in ('healthy','health_failed','start_failed','still_scheduled'):
            with self.subTest(outcome=outcome),tempfile.TemporaryDirectory(prefix='caelis-stopped-installer-') as temp:
                home=pathlib.Path(temp);root=home/'data/caelisbot-install';profile=home/'data/caelisbot'
                binary=home/'.local/bin/caelisbot-launcher';unit=home/'config/systemd/user/caelisbot.service'
                bundle=home/'bundle';source=bundle/'caelisbot-launcher-1.2.3-linux-arm64'
                for directory in (root,profile,binary.parent,unit.parent,bundle): directory.mkdir(parents=True,exist_ok=True)
                (root/'installation.json').write_text(json.dumps({'schema':1,'qa':False,'profile':str(profile)}))
                (profile/'history').write_text('original history')
                binary.write_bytes(b'original launcher');source.write_bytes(b'verified new launcher')
                original_unit='# Managed by CaelisBot Linux installer v1\noriginal unit'
                unit.write_text(original_unit)
                calls=[];healthy=False
                def run(args,**kwargs):
                    nonlocal healthy
                    args=list(args)
                    calls.append(args)
                    if not healthy:
                        self.assertEqual(binary.read_bytes(),b'original launcher')
                        self.assertEqual(unit.read_text(),original_unit)
                    if args[:3]==['systemctl','--user','start'] and outcome=='start_failed':
                        raise subprocess.CalledProcessError(1,args)
                    if len(args)>1 and args[1]=='upgrade':
                        if '--wait' not in args:
                            return subprocess.CompletedProcess(args,0,'{"state":"scheduled"}','')
                        self.assertIn(['systemctl','--user','start','caelisbot.service'],calls)
                        if outcome=='health_failed': raise subprocess.CalledProcessError(1,args)
                        healthy=outcome=='healthy'
                        return subprocess.CompletedProcess(args,0,json.dumps({'state':'healthy' if healthy else 'scheduled'}),'')
                    return subprocess.CompletedProcess(args,0,'','')
                argv=['install.py','--upgrade','--bundle',str(bundle),'--platform','linux-arm64','--version','1.2.3']
                output=io.StringIO()
                with patch.object(sys,'argv',argv),patch.object(installer.os,'getuid',return_value=1000),patch.object(installer.pathlib.Path,'home',return_value=home),patch.dict(installer.os.environ,{'XDG_DATA_HOME':str(home/'data'),'XDG_CONFIG_HOME':str(home/'config')}),patch.object(installer.shutil,'which',return_value='/fixture/tool'),patch.object(installer.subprocess,'run',side_effect=run),contextlib.redirect_stdout(output):
                    if outcome=='healthy': installer.install()
                    else:
                        with self.assertRaises((subprocess.CalledProcessError,SystemExit)): installer.install()
                self.assertEqual(sum(len(c)>1 and c[1]=='upgrade' and '--wait' not in c for c in calls),1)
                self.assertEqual(sum('--wait' in c for c in calls),0 if outcome=='start_failed' else 1)
                self.assertFalse(any('stop' in c or 'restart' in c for c in calls))
                self.assertEqual(binary.read_bytes(),b'verified new launcher' if healthy else b'original launcher')
                self.assertEqual('Updated Bot health confirmed' in output.getvalue(),healthy)
                self.assertEqual((profile/'history').read_text(),'original history')

if __name__=='__main__': unittest.main()
