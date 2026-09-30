#!/usr/bin/env python3
"""Opt-in actual foreground binary/private IPC/framed CLI acceptance.
No model task, credentials, SSH connection or persistent service is used.
"""
import base64
import json
import os
from pathlib import Path
import signal
import socket
import struct
import subprocess
import sys
import tempfile

binary = sys.argv[1]
with tempfile.TemporaryDirectory(prefix='node-agent-', dir='/tmp') as directory:
    os.chmod(directory, 0o700)
    args = [binary, 'serve-agent', '--directory', directory, '--node-id', 'foreground-fixture', '--native-health=false']
    for name, path in zip(('codex', 'caelis'), sys.argv[2:]):
        args += ['--' + name + '-binary', path]
    owner = subprocess.Popen(args, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    try:
        line = owner.stdout.readline()
        if not line:
            raise RuntimeError(owner.stderr.read().decode())
        metadata = json.loads(line)
        os.chmod(directory, 0o700)
        endpoint = metadata['socket']
        assert Path(endpoint).stat().st_mode & 0o777 == 0o600
        def inspect(write, read):
            frame = json.dumps({'id': '1', 'method': 'GET', 'path': '/v1/node/catalog'}).encode()
            write(struct.pack('>I', len(frame)) + frame)
            prefix = read(4)
            assert len(prefix) == 4
            length = struct.unpack('>I', prefix)[0]
            body = b''
            while len(body) < length:
                part = read(length - len(body))
                assert part
                body += part
            result = json.loads(body)
            assert result['status'] == 200
            catalog = json.loads(base64.b64decode(result['body']))
            assert catalog['nodes'][0]['id'] == 'foreground-fixture'
            for runtime in catalog['nodes'][0]['runtimes']:
                assert all(not role['eligible'] for role in runtime['roles'])
                assert runtime['authentication'] == 'unknown'
            return catalog
        with socket.socket(socket.AF_UNIX) as connection:
            connection.settimeout(15)
            connection.connect(endpoint)
            catalog = inspect(connection.sendall, connection.recv)
        proxy = subprocess.Popen([binary, 'proxy-agent', '--socket', endpoint], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        try:
            def write(frame):
                proxy.stdin.write(frame)
                proxy.stdin.flush()
            observed = inspect(write, proxy.stdout.read)
            assert observed == catalog
            proxy.stdin.close()
            assert proxy.wait(timeout=10) == 0
        finally:
            if proxy.poll() is None:
                proxy.kill()
            proxy.wait()
        assert owner.poll() is None, 'observer detach stopped foreground owner'
        print(json.dumps({'foregroundOwner': True, 'privateSocket': True, 'framedDirectAndCLIProxy': True,
                          'detachPreservedOwner': True, 'versions': {r['backend']: r['version'] for r in catalog['nodes'][0]['runtimes']},
                          'runtimeEligibility': False, 'modelTasks': 0, 'credentialsTransferred': False}, sort_keys=True))
    finally:
        if owner.poll() is None:
            owner.send_signal(signal.SIGTERM)
        owner.wait(timeout=10)
