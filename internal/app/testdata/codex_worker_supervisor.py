#!/usr/bin/env python3
"""Foreground acceptance owner. Only its explicit random /tmp root is written.
Existing Codex login stays in the native CLI; this helper never reads auth files.
"""
import hashlib, json, os, pathlib, re, signal, subprocess, sys
os.umask(0o077)
root=pathlib.Path(sys.argv[1])
if not re.fullmatch(r'/tmp/caelis-worker-gate-[0-9a-f]{16}',str(root)) or root.is_symlink() or root.stat().st_mode&0o077:
    raise SystemExit('invalid isolated Worker gate root')
config=json.loads(sys.stdin.readline())
if config.get('directory')!=str(root/'owner') or config.get('socket')!=str(root/'owner/worker.sock') or config.get('execution')!={'model':'gpt-6-luna','effort':'medium','serviceTier':''}:
    raise SystemExit('gate configuration changed')
with (root/'worker-config.json').open('x') as f:json.dump(config,f)
private=(root/'owner.private.log').open('wb')
child=subprocess.Popen([str(root/'node'),'serve-worker','--config-file',str(root/'worker-config.json')],cwd=root,stdout=subprocess.PIPE,stderr=private)
owned=[]
def children(pid):
    try: values=(pathlib.Path('/proc')/str(pid)/'task'/str(pid)/'children').read_text().split()
    except FileNotFoundError:return []
    result=[]
    for value in values:
        try:
            stat=(pathlib.Path('/proc')/value/'stat').read_text();birth=stat[stat.rfind(')')+2:].split()[19]
        except FileNotFoundError:continue
        result.append((value,birth));result.extend(children(value))
    return result
try:
    line=child.stdout.readline(16385)
    if not line or len(line)>16384:raise RuntimeError('native Worker failed before readiness')
    metadata=json.loads(line)
    if metadata.get('pair')!=config['pair'] or metadata.get('socket')!=config['socket']:raise RuntimeError('native readiness identity changed')
    owned=children(child.pid)
    print(json.dumps(metadata),flush=True)
    command=json.loads(sys.stdin.readline())
    if command!={'stop':True}:raise RuntimeError('explicit owned stop required')
finally:
    if child.poll() is None:child.send_signal(signal.SIGTERM)
    try:child.wait(timeout=40)
    except subprocess.TimeoutExpired:child.kill();child.wait();raise RuntimeError('owned native shutdown exceeded bound')
    private.close()
    alive=False
    for pid,birth in owned:
        try:
            stat=(pathlib.Path('/proc')/pid/'stat').read_text();same=stat[stat.rfind(')')+2:].split()[19]==birth
        except FileNotFoundError:same=False
        alive=alive or same
    bindings=json.loads((root/'owner/worker-bindings.json').read_text())
    model_settings=[];receipts=[]
    for task in bindings.get('tasks',{}).values():
        settings=task.get('execution',{});model_settings.append({'model':settings.get('model',''),'effort':settings.get('effort','')})
        source=task.get('workerSource',{})
        receipts.append({'bindingDigest':hashlib.sha256(source.get('BindingID','').encode()).hexdigest(),'operationDigest':hashlib.sha256(source.get('OperationID','').encode()).hexdigest(),'sourceNode':source.get('NodeID',''),'sourceBackend':source.get('Backend',''),'sourceKind':source.get('Kind',''),'requestDigest':task.get('workerStartDigest',''),'requestID':task.get('workerStartId',''),'nativeThreadPresent':bool(task.get('thread')),'nativeTurnPresent':bool(task.get('run')),'nativeReceiptCount':len(task.get('requests',{}))})
    summary={'ownerStopped':child.returncode==0,'nativeChildrenStopped':not alive,'ownedNativeChildren':len(owned),'modelSettings':model_settings,'retainedReceipts':receipts,'residentThreadCreated':bool(bindings.get('threadId')),'acceptedOriginalReplyDropped':(root/'reply-dropped.json').exists()}
    (root/'stopped.json').write_text(json.dumps(summary));print(json.dumps(summary),flush=True)
