#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
if [[ $(uname -s) != Darwin ]]; then echo 'SKIP: macOS Carbon hotkeys'; exit 0; fi
source script/env.sh
out="$BOT_ROOT/.cache/hotkey-native-test"
mkdir -p "$out"
clang -fobjc-arc -Wno-deprecated-declarations -framework Cocoa -framework Carbon script/hotkey-native-test.m -o "$out/probe"
"$out/probe"
python3 - "$out/probe" <<'PY'
import select,subprocess,sys
binary=sys.argv[1]
def read_status(p):
    assert select.select([p.stdout],[],[],5)[0], 'native registration timed out'
    return int(p.stdout.readline())
def start(mode='owner'):
    p=subprocess.Popen([binary,mode],stdin=subprocess.PIPE,stdout=subprocess.PIPE,text=True)
    return p,read_status(p)
def stop(p):
    if p.poll() is None:
        try:p.communicate('\n',timeout=6)
        except subprocess.TimeoutExpired:p.terminate();p.wait(timeout=5)
processes=[]
try:
    for cycle in range(5):
        owner,status=start();processes.append(owner);assert status==0,status
        blocked,status=start('retry');processes.append(blocked);assert status==-9878,status
        stop(owner)
        status=read_status(blocked);assert status==0,status
        stop(blocked)
        print(f'PASS native_registry: occupy/reject/release/reacquire cycle {cycle+1}',flush=True)
finally:
    for p in processes:stop(p)
PY
