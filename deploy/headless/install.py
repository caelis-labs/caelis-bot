#!/usr/bin/env python3
"""Executed only after bootstrap verified the signed inventory. User service only."""
import argparse
import json
import os
import pathlib
import shutil
import subprocess
import sys


def run(*args, **kwargs):
    return subprocess.run(args, check=True, **kwargs)


def install():
    parser = argparse.ArgumentParser(description='Install CaelisBot as a systemd user service (Linux/WSL)')
    parser.add_argument('--upgrade', action='store_true', help='upgrade launcher and schedule the verified Bot package at idle')
    parser.add_argument('--bundle', required=True)
    parser.add_argument('--platform', required=True, choices=['linux-amd64', 'linux-arm64'])
    parser.add_argument('--version', required=True)
    args = parser.parse_args()
    if os.getuid() == 0:
        sys.exit('Run as your ordinary user, without sudo.')
    for tool in ('systemctl', 'bwrap'):
        if not shutil.which(tool):
            sys.exit(f'Install {"bubblewrap" if tool == "bwrap" else "systemd"} using your distribution package manager, then rerun. No system settings were changed.')
    result = subprocess.run(['systemctl', '--user', 'show-environment'], capture_output=True)
    if result.returncode:
        sys.exit('A running systemd user manager is required. On WSL enable systemd in /etc/wsl.conf ([boot] systemd=true), restart WSL yourself, then retry from a user login. No foreground fallback or sudo was used.')
    sandbox = subprocess.run(['bwrap', '--ro-bind', '/', '/', '--unshare-user', '--unshare-pid', '--proc', '/proc', '--dev', '/dev', '--', '/bin/true'], capture_output=True)
    if sandbox.returncode:
        sys.exit('bubblewrap could not create its sandbox. Check your distribution user-namespace/AppArmor policy for bubblewrap; the installer will not disable sandboxing or change system policy.')
    home = pathlib.Path.home()
    data = pathlib.Path(os.environ.get('XDG_DATA_HOME', home / '.local/share'))
    config = pathlib.Path(os.environ.get('XDG_CONFIG_HOME', home / '.config'))
    if not data.is_absolute() or not config.is_absolute():
        sys.exit('XDG_DATA_HOME and XDG_CONFIG_HOME must be absolute paths.')
    root, profile = data / 'caelisbot-install', data / 'caelisbot'
    binary = home / '.local/bin/caelisbot-launcher'
    unit = config / 'systemd/user/caelisbot.service'
    marker = '# Managed by CaelisBot Linux installer v1\n'
    # A foreign service/path is never claimed just because it has the same name.
    if unit.exists() and not unit.read_text().startswith(marker):
        sys.exit('An existing caelisbot.service is not owned by this installer; keep it and choose a separate user installation.')
    if root.exists() and (not unit.exists() or not (root/'installation.json').is_file()):
        sys.exit('An existing or incomplete installation needs inspection: '+str(root)+'. No profile was removed.')
    if binary.exists() and not root.exists():
        sys.exit('An existing launcher is not owned by this installation: '+str(binary))
    for path in (root, profile, binary, unit):
        if path.is_symlink(): sys.exit('Refusing symlink installation target: '+str(path))
    if root.exists():
        state=json.loads((root/'installation.json').read_text())
        if state.get('schema')!=1 or state.get('qa') or state.get('profile')!=str(profile): sys.exit('Existing installation identity differs; no changes made.')
    bundle = pathlib.Path(args.bundle)
    source = bundle / f'caelisbot-launcher-{args.version}-{args.platform}'
    common = ['--root',str(root),'--archive',str(bundle/f'caelisbot-{args.version}-{args.platform}.tar.gz'),
              '--manifest',str(bundle/f'{args.platform}-update.json'),'--signature',str(bundle/f'{args.platform}-update.json.sig')]
    source.chmod(0o755)
    upgrade_completed=False
    if not root.exists() or args.upgrade:
        binary.parent.mkdir(parents=True,exist_ok=True)
        root.parent.mkdir(parents=True,exist_ok=True)
        # Scheduled is not healthy. Start a stopped installation with its
        # original launcher, then only read the existing transaction's health.
        # Atomic replacement affects the launcher's next start, not its live PID.
        existing=root.exists()
        if existing:
            def upgrade_state(wait=False):
                result=run(str(source),'upgrade',*common,*(['--wait'] if wait else []),stdout=subprocess.PIPE,text=True)
                state=json.loads(result.stdout).get('state')
                if state not in ('scheduled','healthy'):
                    sys.exit('Upgrade health was not confirmed; the original launcher was preserved.')
                return state
            state=upgrade_state()
            if state=='scheduled':
                run('systemctl','--user','start','caelisbot.service')
                state=upgrade_state(wait=True)
            if state!='healthy':
                sys.exit('Upgrade is scheduled but health is not confirmed; the original launcher was preserved.')
            upgrade_completed=True
        else:
            run(str(source),'init',*common,'--data-dir',str(profile))
        temporary=binary.with_name(binary.name+'.installing')
        with temporary.open('xb') as out,source.open('rb') as inp:
            shutil.copyfileobj(inp,out);os.fchmod(out.fileno(),0o755);out.flush();os.fsync(out.fileno())
        os.replace(temporary,binary)
    def quote(value):
        return '"'+str(value).replace('\\','\\\\').replace('"','\\"').replace('%','%%').replace('$','$$')+'"'
    unit.parent.mkdir(parents=True,exist_ok=True)
    content=marker+'[Unit]\nDescription=CaelisBot\nAfter=network-online.target\n\n[Service]\nType=simple\nExecStart='+quote(binary)+' serve --root '+quote(root)+'\nUMask=0077\nRestart=on-failure\nRestartSec=5\nTimeoutStopSec=40\nKillMode=process\nSendSIGKILL=no\n\n[Install]\nWantedBy=default.target\n'
    unit.write_text(content);unit.chmod(0o644)
    run('systemctl','--user','daemon-reload')
    run('systemctl','--user','enable','--now','caelisbot.service')
    print('CaelisBot: http://127.0.0.1:9317')
    print('Status: systemctl --user status caelisbot.service')
    print('Stop: systemctl --user stop caelisbot.service')
    print('Start: systemctl --user start caelisbot.service')
    print('Logs: journalctl --user -u caelisbot.service')
    if upgrade_completed: print('Updated Bot health confirmed; the launcher is installed for its next start.')
    print('Profile preserved at '+str(profile)+'. In a VM/WSL, guest loopback is not host loopback: use an explicit loopback SSH forward if needed. No remote listener or linger was enabled.')


if __name__ == '__main__':
    install()
