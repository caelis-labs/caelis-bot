#!/usr/bin/env python3
"""Foreground, synthetic Linux Codex owner. Never discover or read user auth."""
import hashlib,http.client,http.server,json,os,pathlib,re,signal,socket,subprocess,sys,threading,uuid
os.umask(0o077)
root=pathlib.Path(sys.argv[1])
if not re.fullmatch(r'/tmp/caelis-bot-issue47-codex-product-[a-f0-9]{16}',str(root)) or root.is_symlink() or root.stat().st_mode&0o077:raise SystemExit('invalid fixture root')
if sys.argv[2]!='serve':
    ready=json.loads((root/'fixture-ready.json').read_text());port=ready['control_port']
    c=http.client.HTTPConnection('127.0.0.1',port,timeout=10);c.request('POST','/'+sys.argv[2]);r=c.getresponse();data=r.read();c.close()
    if r.status!=200:raise SystemExit('control unavailable')
    print(data.decode());raise SystemExit(0)
binary=sys.argv[3]
if not pathlib.Path(binary).is_absolute() or not os.access(binary,os.X_OK):raise SystemExit('explicit existing binary required')
stop=threading.Event();release=threading.Event();lock=threading.Lock();counts={};owned={};lost=False;tool_attempt=False;tool_names=set()
class Server(http.server.ThreadingHTTPServer):daemon_threads=True
class Quiet(http.server.BaseHTTPRequestHandler):
    def log_message(self,*_):pass
    def reply(self,data):
        raw=json.dumps(data).encode();self.send_response(200);self.send_header('Content-Type','application/json');self.send_header('Content-Length',str(len(raw)));self.end_headers();self.wfile.write(raw)
def descendants(pid):
    values=set()
    try:
        for task in (pathlib.Path('/proc')/str(pid)/'task').iterdir():
            try:values.update((task/'children').read_text().split())
            except FileNotFoundError:pass
    except FileNotFoundError:return
    for p in values:
        try:
            stat=(pathlib.Path('/proc')/p/'stat').read_text();birth=stat[stat.rfind(')')+2:].split()[19]
            with lock:owned[p]=birth
        except FileNotFoundError:continue
        descendants(p)
def identities():
    with lock:return dict(owned)
def same_process(pid,birth):
    try:
        stat=(pathlib.Path('/proc')/pid/'stat').read_text();fields=stat[stat.rfind(')')+2:].split();return fields[19]==birth
    except FileNotFoundError:return False
def native_facts():
    path=root/'profile/conversation.json'
    try:b=json.loads(path.read_text())
    except (FileNotFoundError,json.JSONDecodeError):b={}
    tasks=[]
    for task in b.get('tasks',{}).values():
        tasks.append({'thread_present':bool(task.get('thread')),'turn_present':bool(task.get('run')),'status':task.get('view',{}).get('status'),'outcome':task.get('view',{}).get('outcome')})
    native_turn=False;dispatch_recorded=False;dispatch_turn=False;dispatch_digest=''
    for session in (root/'codex-home/sessions').rglob('*.jsonl'):
        sid='';turn=''
        for line in session.open():
            try:event=json.loads(line)
            except json.JSONDecodeError:continue
            payload=event.get('payload',{})
            if event.get('type')=='session_meta':sid=payload.get('id','')
            if event.get('type')=='turn_context':
                turn=payload.get('turn_id','')
                if sid==b.get('threadId') and turn:native_turn=True
            if sid==b.get('threadId') and event.get('type')=='response_item' and payload.get('type')=='function_call' and payload.get('name')=='bot_task_start' and payload.get('namespace')=='mcp__caelis_bot':
                try:arguments=json.loads(payload.get('arguments','{}'))
                except json.JSONDecodeError:continue
                if arguments.get('requestId')=='synthetic-local-worker':
                    dispatch_recorded=True;dispatch_turn=bool(turn);dispatch_digest=hashlib.sha256(turn.encode()).hexdigest()
    return {'thread_present':bool(b.get('threadId')),'thread_digest':hashlib.sha256(b.get('threadId','').encode()).hexdigest(),'native_turn_present':native_turn,'tasks':tasks,'native_mcp_dispatch_recorded':dispatch_recorded,'dispatch_native_turn_present':dispatch_turn,'dispatch_native_turn_digest':dispatch_digest}

class Provider(Quiet):
    def do_POST(self):
        global tool_attempt
        body=json.loads(self.rfile.read(int(self.headers.get('Content-Length','0'))));raw=json.dumps(body.get('input',[])).encode()
        keys=re.findall(rb'CASE_(?:CODEX_LOCAL_WORKER|PRODUCT_[A-Z_]+)',raw);key=keys[-1].decode() if keys else 'introduction'
        if b'CASE_PRODUCT_WORKER' in raw:key='CASE_PRODUCT_WORKER'
        review=body.get('text',{}).get('format') or {}
        is_review=review.get('type')=='json_schema' and review.get('schema',{}).get('properties',{}).get('outcome',{}).get('enum')==['allow','deny']
        if is_review:key='CASE_PRODUCT_REVIEW'
        with lock:counts[key]=counts.get(key,0)+1
        for tool in body.get('tools',[]):
            name=tool.get('name','')
            if name:tool_names.add(name)
            if tool.get('type')=='namespace':
                for nested in tool.get('tools',[]):tool_names.add(name+'.'+nested.get('name',''))
        if key=='CASE_PRODUCT_DETACH':
            while not release.wait(.02) and not stop.is_set():pass
        identifier='fixture-'+uuid.uuid4().hex
        text='PRODUCT_NATIVE_COMPLETE';item=None
        if key=='CASE_PRODUCT_WORKER' and not tool_attempt:
            tool=next((n for n in tool_names if n.endswith('bot_task_start')),None)
            if tool:
                tool_attempt=True;item={'id':identifier+'-tool','type':'function_call','name':tool.rsplit('.',1)[-1],'namespace':tool.rsplit('.',1)[0],'call_id':identifier+'-call','arguments':json.dumps({'requestId':'synthetic-local-worker','title':'Synthetic local Worker','prompt':'CASE_CODEX_LOCAL_WORKER: Reply LOCAL_WORKER_NATIVE_COMPLETE using only this loopback provider; do not use tools or read user files.'})}
        if key=='CASE_CODEX_LOCAL_WORKER':text='LOCAL_WORKER_NATIVE_COMPLETE'
        if is_review:
            allowed=b'bot_task_start' in raw and b'synthetic-local-worker' in raw and b'CASE_CODEX_LOCAL_WORKER' in raw
            text=json.dumps({'outcome':'allow' if allowed else 'deny','risk_level':'low','user_authorization':'high' if allowed else 'unknown','rationale':'Explicitly authorized one isolated loopback-only Worker; no user files, credentials, external requests or tool usage.' if allowed else 'Outside the synthetic fixture authorization.'})
        if item is None:item={'id':identifier+'-message','type':'message','role':'assistant','status':'completed','content':[{'type':'output_text','text':text,'annotations':[]}]}
        self.send_response(200);self.send_header('Content-Type','text/event-stream');self.end_headers()
        def emit(value):self.wfile.write(('data: '+json.dumps(value)+'\n\n').encode());self.wfile.flush()
        try:
            emit({'type':'response.created','response':{'id':identifier,'status':'in_progress','output':[]}})
            emit({'type':'response.output_item.done','output_index':0,'item':item})
            emit({'type':'response.completed','response':{'id':identifier,'status':'completed','output':[item],'usage':{'input_tokens':1,'output_tokens':1,'total_tokens':2}}})
        except (BrokenPipeError,ConnectionResetError):pass
provider=Server(('127.0.0.1',0),Provider);threading.Thread(target=provider.serve_forever,daemon=True).start()
for name in ('home','tmp','profile','codex-home'):(root/name).mkdir(mode=0o700)
(root/'codex-home/config.toml').write_text('model = "worker-primary-fixture"\nmodel_provider = "worker-primary-fixture"\n[model_providers.worker-primary-fixture]\nname = "Isolated native primary"\nbase_url = "http://127.0.0.1:'+str(provider.server_port)+'"\nwire_api = "responses"\nrequires_openai_auth = false\nrequest_max_retries = 0\nstream_max_retries = 0\nsupports_websockets = false\n')
(root/'profile/runtime.json').write_text(json.dumps({'runtime':'codex','cliPath':binary}))
(root/'product.auth').write_text(uuid.uuid4().hex+uuid.uuid4().hex)
env={'HOME':str(root/'home'),'CODEX_HOME':str(root/'codex-home'),'CODEX_BIN':binary,'XDG_CONFIG_HOME':str(root/'home/config'),'XDG_DATA_HOME':str(root/'home/data'),'XDG_CACHE_HOME':str(root/'home/cache'),'TMPDIR':str(root/'tmp'),'PATH':'/usr/bin:/bin:/usr/sbin:/sbin','SHELL':'/bin/bash','LANG':'C.UTF-8'}
node_log=open(root/'node.private.log','wb');output=open(root/'node-ready.json','wb');node=None
signal.signal(signal.SIGTERM,lambda *_:stop.set());signal.signal(signal.SIGINT,lambda *_:stop.set())
try:
    version=subprocess.check_output([binary,'--version'],env=env,cwd=root,text=True).strip()
    if version!='codex-cli 0.159.2':raise RuntimeError('existing binary version changed')
    node=subprocess.Popen([str(root/'caelis-node'),'serve-bot','--profile',str(root/'profile'),'--listen','127.0.0.1:0','--auth-file',str(root/'product.auth')],env=env,cwd=root,stdout=output,stderr=node_log)
    for _ in range(1200):
        if (root/'node-ready.json').stat().st_size:break
        if node.poll() is not None:raise RuntimeError('owner exited before metadata')
        if stop.wait(.025):raise RuntimeError('fixture stopped')
    metadata=json.loads((root/'node-ready.json').read_text());port=int(metadata['endpoint'].rsplit(':',1)[1])
    class Proxy(Quiet):
        def do_GET(self):self.forward()
        def do_POST(self):self.forward()
        def do_PUT(self):self.forward()
        def forward(self):
            global lost
            data=self.rfile.read(int(self.headers.get('Content-Length','0')));headers={k:v for k,v in self.headers.items() if k.lower() not in ('host','connection','transfer-encoding')};c=http.client.HTTPConnection('127.0.0.1',port,timeout=120)
            try:
                c.request(self.command,self.path,data,headers);r=c.getresponse();reply=r.read();command=json.loads(data) if self.path=='/v1/commands' else {}
                if command.get('id')=='native-lost' and r.status==200 and not lost:
                    lost=True;self.close_connection=True;self.connection.shutdown(socket.SHUT_RDWR);self.connection.close();return
                self.send_response(r.status)
                for k,v in r.getheaders():
                    if k.lower() not in ('content-length','connection','transfer-encoding'):self.send_header(k,v)
                self.send_header('Content-Length',str(len(reply)));self.end_headers();self.wfile.write(reply)
            except (OSError,BrokenPipeError):pass
            finally:c.close()
    proxy=Server(('127.0.0.1',0),Proxy);threading.Thread(target=proxy.serve_forever,daemon=True).start()
    class Control(Quiet):
        def do_POST(self):
            if self.path=='/release':release.set();self.reply({'released':True});return
            if self.path=='/stop':self.reply({'stopping':True});stop.set();return
            if self.path!='/status':self.send_error(404);return
            descendants(node.pid);s=socket.socket();s.settimeout(.3);alive=s.connect_ex(('127.0.0.1',port))==0;s.close()
            captured=identities()
            with lock:data={'counts':dict(counts),'node_alive':node.poll() is None,'listener_alive':alive,'response_dropped':lost,'tool_attempt':tool_attempt,'tool_names':sorted(tool_names),'task_tool_discovered':any(n.endswith('bot_task_start') for n in tool_names),'owned_native_children':len(owned),'native_children_alive':sum(same_process(p,b) for p,b in captured.items()),**native_facts()}
            self.reply(data)
    control=Server(('127.0.0.1',0),Control);threading.Thread(target=control.serve_forever,daemon=True).start()
    ready={'endpoint':'http://127.0.0.1:'+str(proxy.server_port),'identity':metadata['identity'],'control_port':control.server_port,'synthetic':True,'native_version':'0.159.2'}
    (root/'fixture-ready.json').write_text(json.dumps(ready));(root/'processes.json').write_text(json.dumps({'supervisor':os.getpid(),'node':node.pid}))
    print(json.dumps(ready),flush=True)
    previous={}
    while not stop.wait(.02):
        descendants(node.pid);current=identities()
        if current!=previous:
            temporary=root/'.processes.tmp';temporary.write_text(json.dumps({'supervisor':os.getpid(),'node':node.pid,'children':current}));os.replace(temporary,root/'processes.json');previous=current
finally:
    release.set()
    if node is not None and node.poll() is None:
        node.send_signal(signal.SIGTERM)
        try:node.wait(timeout=40)
        except subprocess.TimeoutExpired:node.kill();node.wait();raise RuntimeError('owner cleanup exceeded bound')
    provider.shutdown();output.close();node_log.close()
    alive=[p for p,b in identities().items() if same_process(p,b)]
    summary={'owner_stopped':node is not None and node.returncode==0,'owned_children_reaped':not alive,'owned_native_children':len(owned),**native_facts()}
    (root/'fixture-stopped.json').write_text(json.dumps(summary));print(json.dumps(summary),flush=True)
    if alive:raise RuntimeError('owned native descendants remain')
