"""Exercise a new native CLI process in our own PTY, not an existing terminal app."""
import json
import os
import pathlib
import pty
import select
import signal
import struct
import subprocess
import termios
import fcntl
import time

root = pathlib.Path(__file__).resolve().parents[2]
host = subprocess.Popen([str(root / '.cache/eventbot-poc'), '-hold'], stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, start_new_session=True)
cli = None
master = None
try:
    line = host.stdout.readline()
    if not line:
        raise RuntimeError(host.stderr.read())
    target = json.loads(line)
    master, slave = pty.openpty()
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack('HHHH', 38, 120, 0, 0))
    env = {'PATH': os.environ['PATH'], 'HOME': str(pathlib.Path(target['home']).parent), 'CODEX_HOME': target['home'], 'TERM': 'xterm-256color', 'LANG': 'en_US.UTF-8'}
    cli = subprocess.Popen([target['binary'], '--remote', 'unix://' + target['socket'], 'resume', target['worker']], stdin=slave, stdout=slave, stderr=slave, cwd=env['HOME'], env=env, start_new_session=True)
    os.close(slave)
    capture = bytearray()
    sent = False
    ready_at = None
    end = time.monotonic() + 35
    while time.monotonic() < end:
        ready, _, _ = select.select([master], [], [], .2)
        if ready:
            try:
                chunk = os.read(master, 65536)
            except OSError:
                break
            if not chunk:
                break
            capture.extend(chunk)
            if b'\x1b[6n' in chunk:
                os.write(master, b'\x1b[1;1R')
            if b'\x1b]10;?' in chunk:
                os.write(master, b'\x1b]10;rgb:ffff/ffff/ffff\x1b\\')
            if b'\x1b]11;?' in chunk:
                os.write(master, b'\x1b]11;rgb:0000/0000/0000\x1b\\')
            if ready_at is None and b'POC_EVENT_ACK' in capture:
                ready_at = time.monotonic() + 2
        if not sent and ready_at is not None and time.monotonic() >= ready_at:
            os.write(master, b'\x1b[200~POC human message from native TUI\x1b[201~')
            time.sleep(.15)
            os.write(master, b'\r')
            sent = True
        if host.poll() is not None:
            break
    (root / '.cache/eventbot-pty.txt').write_bytes(capture)
    if not sent:
        raise RuntimeError('Native TUI did not display the synthetic transcript; see .cache/eventbot-pty.txt')
    remaining, errors = host.communicate(timeout=10)
    if host.returncode:
        raise RuntimeError(errors)
    reports = [json.loads(s) for s in remaining.splitlines() if s.startswith('{')]
    if not reports or not reports[-1]['nativeTuiTurnObserved']:
        raise RuntimeError('Host did not observe the TUI-originated turn')
    print(json.dumps({'nativeTuiTranscriptVisible': True, 'nativeTuiInputObservedByBot': True, 'protocol': reports[-1]}))
finally:
    for proc in (cli, host):
        if proc is not None and proc.poll() is None:
            os.killpg(proc.pid, signal.SIGTERM)
            try:
                proc.wait(timeout=3)
            except subprocess.TimeoutExpired:
                os.killpg(proc.pid, signal.SIGKILL)
                proc.wait()
    if master is not None:
        os.close(master)
