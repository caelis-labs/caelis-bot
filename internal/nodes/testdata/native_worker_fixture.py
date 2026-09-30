#!/usr/bin/env python3
"""Temporary, secretless public NativeHost acceptance fixture; never production setup.
Owns only its supplied random /tmp root and generated fixture credential. The
management credential remains target-local. All provider requests stay loopback.
"""
import hashlib, http.client, http.server, json, os, pathlib, re, signal, socket, subprocess, sys, threading, time, urllib.parse, uuid
root = pathlib.Path(sys.argv[1])
if not re.fullmatch(r'/tmp/caelis-bot-issue47-worker-[a-f0-9]{16}', str(root)) or root.is_symlink() or root.stat().st_mode & 0o077:
    raise SystemExit('invalid isolated fixture root')
os.umask(0o077)
hold_artifact_completion = '--hold-artifact-completion' in sys.argv[2:]
stop = threading.Event()
lock = threading.Lock()
counts, requests, dropped, released, session_titles, native_receipts = {}, {}, set(), set(), {}, {}
class Server(http.server.ThreadingHTTPServer):
    daemon_threads = True
class Quiet(http.server.BaseHTTPRequestHandler):
    def log_message(self, *_): pass
    def reply(self, value):
        raw = json.dumps(value).encode(); self.send_response(200); self.send_header('Content-Type','application/json'); self.send_header('Content-Length',str(len(raw))); self.end_headers(); self.wfile.write(raw)
class Provider(Quiet):
    def do_POST(self):
        raw = self.rfile.read(int(self.headers.get('Content-Length','0')))
        body = json.loads(raw); keys = re.findall(rb'CASE_[A-Z_]+', raw); key = keys[-1].decode() if keys else ''
        with lock:
            index = counts.get(key,0); counts[key] = index+1
        if key in ('CASE_SSH_CANCEL','CASE_SSH_DETACH') or (hold_artifact_completion and key == 'CASE_SSH_ARTIFACT' and index == 2):
            while key not in released and not stop.wait(.05): pass
        name, args = '', {}
        if key == 'CASE_SSH_ARTIFACT' and index == 0: name,args = 'Write',{'path':'artifact.txt','content':'SSH_NATIVE_ARTIFACT_SENTINEL'}
        if key == 'CASE_SSH_ARTIFACT' and index == 1: name,args = 'PublishArtifact',{'path':'artifact.txt','name':'artifact.txt','media_type':'text/plain'}
        if key == 'CASE_SSH_APPROVAL' and index == 0: name,args = 'RunCommand',{'command':'printf approved > approval.txt','yield_time_ms':10000,'sandbox_permissions':'require_escalated','justification':'Synthetic SSH fixture approval'}
        identifier = 'fixture-'+uuid.uuid4().hex
        arguments = json.dumps(args)
        self.send_response(200); self.send_header('Content-Type','text/event-stream'); self.end_headers()
        def emit(value): self.wfile.write(('data: '+json.dumps(value)+'\n\n').encode())
        try:
            if 'input' in body:
                if name:
                    emit({'type':'response.output_item.added','output_index':0,'item':{'id':identifier,'type':'function_call','call_id':identifier,'name':name}})
                    emit({'type':'response.function_call_arguments.delta','item_id':identifier,'output_index':0,'delta':arguments})
                    output = {'id':identifier,'type':'function_call','call_id':identifier,'name':name,'arguments':arguments}
                else:
                    emit({'type':'response.output_text.delta','item_id':identifier,'output_index':0,'delta':'SSH_FIXTURE_COMPLETE'})
                    output = {'id':identifier,'type':'message','role':'assistant','content':[{'type':'output_text','text':'SSH_FIXTURE_COMPLETE'}]}
                emit({'type':'response.completed','response':{'model':body['model'],'status':'completed','output':[output]}})
            else:
                delta = {'role':'assistant','content':'SSH_FIXTURE_COMPLETE'}
                if name: delta={'role':'assistant','tool_calls':[{'index':0,'id':identifier,'type':'function','function':{'name':name,'arguments':arguments}}]}
                emit({'choices':[{'index':0,'delta':delta,'finish_reason':'tool_calls' if name else 'stop'}]})
            self.wfile.write(b'data: [DONE]\n\n'); self.wfile.flush()
        except (BrokenPipeError, ConnectionResetError): pass
provider = Server(('127.0.0.1',0),Provider)
threading.Thread(target=provider.serve_forever,daemon=True).start()
for name in ('home','tmp','workspaces'): (root/name).mkdir(mode=0o700)
env={'HOME':str(root/'home'),'XDG_CONFIG_HOME':str(root/'home/config'),'XDG_DATA_HOME':str(root/'home/data'),'XDG_CACHE_HOME':str(root/'home/cache'),'TMPDIR':str(root/'tmp'),'PATH':'/usr/bin:/bin:/usr/sbin:/sbin','SHELL':'/bin/bash','LANG':'C.UTF-8'}
log=open(root/'host.private.log','wb')
host=subprocess.Popen([str(root/'caelis'),'serve','--store-dir',str(root/'store'),'--listen','127.0.0.1:0'],cwd=root,env=env,stdout=log,stderr=log)
(root/'supervisor.pid').write_text(str(os.getpid()))
def finish(*_): stop.set()
signal.signal(signal.SIGTERM,finish); signal.signal(signal.SIGINT,finish)
try:
    discovery=root/'store/runtime/service/discovery.json'
    for _ in range(300):
        if discovery.exists(): break
        if host.poll() is not None: raise RuntimeError('native Host exited')
        time.sleep(.05)
    d=json.loads(discovery.read_text()); owner=(root/'store/runtime/service/auth.token').read_text().strip()
    origin=urllib.parse.urlparse(d['endpoint'])
    def native(method,path,body=None):
        conn=http.client.HTTPConnection(origin.hostname,origin.port,timeout=30)
        headers={'Authorization':'Bearer '+owner}
        if body is not None:
            headers['Content-Type']='application/json'
            if 'operation_id' in body: headers['Idempotency-Key']=body['operation_id']
            if 'expected_revision' in body: headers['If-Match']='"'+body['expected_revision']+'"'
        conn.request(method,'/api/control/v1'+path,json.dumps(body) if body is not None else None,headers)
        res=conn.getresponse(); data=res.read(); conn.close()
        if res.status >= 300: raise RuntimeError('fixture native public setup failed '+str(res.status))
        return json.loads(data)
    status=native('GET','/status')
    result=native('POST','/configuration/connect-model',{'operation_id':'fixture-connect-model','expected_revision':status['configuration']['revision'],'config':{'provider':'openai','model':'gpt-5.4-mini','base_url':'http://127.0.0.1:'+str(provider.server_port)+'/v1','api_key':'SYNTHETIC_FIXTURE_ONLY'}})
    if result['outcome'] not in ('committed','accepted'): raise RuntimeError('fixture model not configured')
    class Proxy(Quiet):
        def do_GET(self): self.handle_request()
        def do_POST(self): self.handle_request()
        def handle_request(self):
            if self.path.startswith('/fixture/'):
                if self.path == '/fixture/state':
                    with lock: self.reply({'model_counts':dict(counts),'request_counts':dict(requests),'dropped':sorted(dropped),'host_alive':host.poll() is None,'artifact_completion_held':hold_artifact_completion and counts.get('CASE_SSH_ARTIFACT',0)>=3 and 'CASE_SSH_ARTIFACT' not in released})
                elif self.path in ('/fixture/cancel-receipt','/fixture/approval-receipt'):
                    category='cancel' if self.path=='/fixture/cancel-receipt' else 'approval'
                    receipt=native_receipts.get(category,{})
                    credential=json.loads(next((root/'store/runtime/bot-worker-enrollments').glob('*.json')).read_text())['Token']
                    connection=http.client.HTTPConnection(origin.hostname,origin.port,timeout=10)
                    connection.request('GET','/api/control/v1/application/operations/'+receipt['operation_id'],headers={'Authorization':'Bearer '+credential})
                    response=connection.getresponse(); response.read(); connection.close()
                    self.reply({'native_version':'0.65.0','native_response_outcome':receipt['outcome'],'native_recovery_http':response.status,'response_dropped':category in dropped})
                elif self.path in ('/fixture/artifact-status','/fixture/approval-status'):
                    artifact=self.path=='/fixture/artifact-status'
                    sid=session_titles.get('artifact' if artifact else 'approval')
                    tool='PublishArtifact' if artifact else 'RunCommand'
                    connection=http.client.HTTPConnection(origin.hostname,origin.port,timeout=10)
                    connection.request('GET','/api/control/v1/sessions/'+sid+'/reconnect?history_turns=64',headers={'Authorization':'Bearer '+owner})
                    feed=connection.getresponse(); evidence={}
                    while True:
                        line=feed.readline()
                        if not line: break
                        if not line.startswith(b'data:'): continue
                        frame=json.loads(line[5:])
                        for envelope in frame.get('events',[]):
                            update=envelope.get('update',{})
                            if update.get('sessionUpdate')=='tool_call_update' and update.get('name')==tool:
                                evidence={'native_version':'0.65.0','tool':update['name'],'status':update.get('status'),'error_code':update.get('rawOutput',{}).get('error_code')}
                        if frame.get('kind')=='sync': break
                    connection.close(); self.reply(evidence)
                elif self.path.startswith('/fixture/release/'):
                    released.add(self.path.rsplit('/',1)[1]); self.reply({'released':True})
                elif self.path == '/fixture/stop': self.reply({'stopping':True}); stop.set()
                else: self.send_error(404)
                return
            raw=self.rfile.read(int(self.headers.get('Content-Length','0'))) if self.command=='POST' else None
            headers={k:v for k,v in self.headers.items() if k.lower() not in ('host','connection','transfer-encoding')}
            conn=http.client.HTTPConnection(origin.hostname,origin.port,timeout=120)
            try:
                conn.request(self.command,self.path,raw,headers); res=conn.getresponse()
                if 'text/event-stream' in res.getheader('Content-Type',''):
                    self.send_response(res.status); self.send_header('Content-Type','text/event-stream'); self.end_headers(); self.wfile.flush()
                    while not stop.is_set():
                        chunk=res.read1(65536)
                        if not chunk: break
                        self.wfile.write(chunk); self.wfile.flush()
                    return
                data=res.read()
                if self.command=='POST' and self.path.endswith('/application/workers') and res.status<300:
                    session_titles[json.loads(raw)['title']]=json.loads(data)['session_id']
                if self.command=='POST' and self.path.endswith('/application/sessions') and res.status<300:
                    profile=json.loads(raw)['profile']; cwd=profile.get('workspace',{}).get('cwd','')
                    for task in ('artifact','approval','cancel','detach'):
                        if cwd.endswith('/task-'+hashlib.sha256(task.encode()).hexdigest()[:24]):session_titles[task]=json.loads(data)['session_id']
                category=''
                if self.command=='POST':
                    if self.path.endswith('/prompt'): category='prompt'
                    elif self.path.endswith('/resolve'): category='approval'
                    elif self.path.endswith('/cancel'): category='cancel'
                    elif self.path.endswith('/application/workers') or self.path.endswith('/application/sessions'): category='create'
                if category:
                    native_result=json.loads(data) if res.status<300 else {}
                    if native_result.get('operation_id'): native_receipts[category]={'operation_id':native_result['operation_id'],'outcome':native_result.get('outcome')}
                    with lock:
                        requests[category]=requests.get(category,0)+1
                        lose=category in ('prompt','approval','cancel') and category not in dropped and res.status<300
                        if lose: dropped.add(category)
                    if lose:
                        self.close_connection=True; self.connection.shutdown(socket.SHUT_RDWR); self.connection.close(); return
                self.send_response(res.status)
                for k,v in res.getheaders():
                    if k.lower() not in ('content-length','connection','transfer-encoding'): self.send_header(k,v)
                self.send_header('Content-Length',str(len(data))); self.end_headers(); self.wfile.write(data)
            except (BrokenPipeError,ConnectionResetError): pass
            finally: conn.close()
    proxy=Server(('127.0.0.1',0),Proxy)
    threading.Thread(target=proxy.serve_forever,daemon=True).start()
    d['endpoint']='http://127.0.0.1:'+str(proxy.server_port)
    discovery.write_text(json.dumps(d)); discovery.chmod(0o600)
    (root/'ready.json').write_text(json.dumps({'endpoint':d['endpoint'],'os':'linux','arch':'arm64','native_version':'0.65.0','provider':'synthetic-loopback','management_secret_stays_target':True}))
    while not stop.wait(.1):
        if host.poll() is not None: raise RuntimeError('native Host exited')
finally:
    stop.set()
    if host.poll() is None:
        host.send_signal(signal.SIGINT)
        try: host.wait(timeout=10)
        except subprocess.TimeoutExpired: host.kill(); host.wait()
    provider.shutdown(); log.close()
