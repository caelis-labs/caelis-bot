#!/usr/bin/python3
"""Fixture-only observation proxy: drop one accepted original start response.
Closed frames are inspected only to select the fault; no source is fabricated.
"""
import json, os, pathlib, struct, subprocess, sys, threading
root=pathlib.Path(__file__).parent
child=subprocess.Popen([str(root/'node')]+sys.argv[1:],stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.DEVNULL)
requests={};lock=threading.Lock()
def frame(stream):
    prefix=stream.read(4)
    if not prefix:return None
    if len(prefix)!=4:raise RuntimeError('partial fixture prefix')
    length=struct.unpack('>I',prefix)[0]
    if not 0<length<=24*1024*1024:raise RuntimeError('fixture frame limit')
    raw=stream.read(length)
    if len(raw)!=length:raise RuntimeError('partial fixture frame')
    return prefix+raw,json.loads(raw)
def send():
    try:
        while True:
            entry=frame(sys.stdin.buffer)
            if entry is None:break
            raw,value=entry
            with lock:requests[value['ID']]=value.get('Method')=='start' and bool(value.get('Current'))
            child.stdin.write(raw);child.stdin.flush()
    finally:child.stdin.close()
threading.Thread(target=send,daemon=True).start()
try:
    while True:
        entry=frame(child.stdout)
        if entry is None:break
        raw,value=entry
        with lock:fresh_start=requests.get(value['ID'],False)
        if fresh_start and not value.get('Fault') and value.get('Task',{}).get('outcome')=='accepted' and not (root/'reply-dropped.json').exists():
            with (root/'reply-dropped.json').open('x') as marker:json.dump({'acceptedOriginalReplyDropped':True},marker);marker.flush();os.fsync(marker.fileno())
            break
        sys.stdout.buffer.write(raw);sys.stdout.buffer.flush()
finally:
    # Only this helper's observation child is stopped. The Unix owner is separate.
    if child.poll() is None:child.terminate()
    try:child.wait(timeout=5)
    except subprocess.TimeoutExpired:child.kill();child.wait()
