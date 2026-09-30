#!/usr/bin/env python3
"""Owned, temporary public NativeHost/product fixture. No user credential reads.
All credentials below are generated fixture-only and remain on this target.
"""
import http.client, http.server, json, os, pathlib, re, signal, socket, subprocess, sys, threading, time, urllib.parse, uuid
root = pathlib.Path(sys.argv[1])
if not re.fullmatch(r'/tmp/caelis-bot-issue47-product-[a-f0-9]{16}', str(root)) or root.is_symlink() or root.stat().st_mode & 0o077:
    raise SystemExit('invalid isolated product fixture root')
os.umask(0o077)
action = sys.argv[2]
if action != 'serve':
    ready=json.loads((root/'fixture-ready.json').read_text())
    url=urllib.parse.urlparse(ready['control'])
    connection=http.client.HTTPConnection(url.hostname,url.port,timeout=10)
    connection.request('POST','/'+action)
    response=connection.getresponse();data=response.read();connection.close()
    if response.status!=200: raise SystemExit('fixture control failed')
    print(data.decode());raise SystemExit(0)
stop=threading.Event();lock=threading.Lock();counts={};released=False
class Server(http.server.ThreadingHTTPServer): daemon_threads=True
class Quiet(http.server.BaseHTTPRequestHandler):
    def log_message(self,*_): pass
    def reply(self,data):
        raw=json.dumps(data).encode();self.send_response(200);self.send_header('Content-Type','application/json');self.send_header('Content-Length',str(len(raw)));self.end_headers();self.wfile.write(raw)
class Provider(Quiet):
    def do_POST(self):
        raw=self.rfile.read(int(self.headers.get('Content-Length','0')));body=json.loads(raw)
        keys=re.findall(rb'CASE_PRODUCT_[A-Z_]+',raw);key=keys[-1].decode() if keys else 'introduction'
        with lock: counts[key]=counts.get(key,0)+1
        if key=='CASE_PRODUCT_DETACH':
            while not released and not stop.wait(.02): pass
        identifier='fixture-'+uuid.uuid4().hex;reply='PRODUCT_NATIVE_COMPLETE'
        self.send_response(200);self.send_header('Content-Type','text/event-stream');self.end_headers()
        def emit(value): self.wfile.write(('data: '+json.dumps(value)+'\n\n').encode())
        try:
            if 'input' in body:
                emit({'type':'response.output_text.delta','item_id':identifier,'output_index':0,'delta':reply})
                emit({'type':'response.completed','response':{'model':body['model'],'status':'completed','output':[{'id':identifier,'type':'message','role':'assistant','content':[{'type':'output_text','text':reply}]}]}})
            else: emit({'choices':[{'index':0,'delta':{'role':'assistant','content':reply},'finish_reason':'stop'}]})
            self.wfile.write(b'data: [DONE]\n\n');self.wfile.flush()
        except (BrokenPipeError,ConnectionResetError): pass
provider=Server(('127.0.0.1',0),Provider);threading.Thread(target=provider.serve_forever,daemon=True).start()
for name in ('home','tmp','profile'): (root/name).mkdir(mode=0o700)
env={'HOME':str(root/'home'),'XDG_CONFIG_HOME':str(root/'home/config'),'XDG_DATA_HOME':str(root/'home/data'),'XDG_CACHE_HOME':str(root/'home/cache'),'TMPDIR':str(root/'tmp'),'PATH':'/usr/bin:/bin:/usr/sbin:/sbin','SHELL':'/bin/bash','LANG':'C.UTF-8'}
host_log=open(root/'host.private.log','wb');node_log=open(root/'node.private.log','wb');node_output=open(root/'node-ready.json','wb')
host=subprocess.Popen([str(root/'caelis'),'serve','--store-dir',str(root/'store'),'--listen','127.0.0.1:0'],cwd=root,env=env,stdout=host_log,stderr=host_log)
node=None
def finish(*_):stop.set()
signal.signal(signal.SIGTERM,finish);signal.signal(signal.SIGINT,finish)
try:
    discovery=root/'store/runtime/service/discovery.json'
    for _ in range(400):
        if discovery.exists():break
        if host.poll() is not None:raise RuntimeError('fixture Host exited')
        if stop.wait(.025):raise RuntimeError('fixture stopped')
    discovered=json.loads(discovery.read_text());owner=(root/'store/runtime/service/auth.token').read_text().strip()
    origin=urllib.parse.urlparse(discovered['endpoint'])
    connection=http.client.HTTPConnection(origin.hostname,origin.port,timeout=30)
    connection.request('GET','/api/control/v1/status',headers={'Authorization':'Bearer '+owner});response=connection.getresponse();status=json.loads(response.read());connection.close()
    request={'operation_id':'fixture-connect-model','expected_revision':status['configuration']['revision'],'config':{'provider':'openai','model':'gpt-5.4-mini','base_url':'http://127.0.0.1:'+str(provider.server_port)+'/v1','api_key':'SYNTHETIC_FIXTURE_ONLY'}}
    connection=http.client.HTTPConnection(origin.hostname,origin.port,timeout=30)
    connection.request('POST','/api/control/v1/configuration/connect-model',json.dumps(request),{'Authorization':'Bearer '+owner,'Content-Type':'application/json','Idempotency-Key':request['operation_id'],'If-Match':'"'+str(request['expected_revision'])+'"'})
    response=connection.getresponse();result=json.loads(response.read());connection.close()
    if result.get('outcome') not in ('accepted','committed'):raise RuntimeError('synthetic public model configuration rejected')
    (root/'profile/runtime.json').write_text(json.dumps({'runtime':'caelis','cliPath':str(root/'caelis'),'caelisStore':str(root/'store')}))
    directory=root/'profile/providers/caelis';directory.mkdir(parents=True,mode=0o700)
    (directory/'execution.json').write_text(json.dumps({'model':'openai/gpt-5.4-mini','approvalMode':'workspace-write'}))
    (root/'product.auth').write_text(uuid.uuid4().hex+uuid.uuid4().hex)
    node=subprocess.Popen([str(root/'caelis-node'),'serve-bot','--profile',str(root/'profile'),'--listen','127.0.0.1:0','--auth-file',str(root/'product.auth'),'--runtime-directory',str(root/'home/managed-runtimes')],cwd=root,env=env,stdout=node_output,stderr=node_log)
    for _ in range(800):
        if (root/'node-ready.json').stat().st_size:break
        if node.poll() is not None:raise RuntimeError('headless owner exited before metadata')
        if stop.wait(.025):raise RuntimeError('fixture stopped')
    metadata=json.loads((root/'node-ready.json').read_text())
    node_origin=urllib.parse.urlparse(metadata['endpoint']);lost=False
    class ProductProxy(Quiet):
        def do_GET(self):self.forward()
        def do_POST(self):self.forward()
        def do_PUT(self):self.forward()
        def forward(self):
            global lost
            raw=self.rfile.read(int(self.headers.get('Content-Length','0')))
            headers={k:v for k,v in self.headers.items() if k.lower() not in ('host','connection','transfer-encoding')}
            connection=http.client.HTTPConnection(node_origin.hostname,node_origin.port,timeout=120)
            try:
                connection.request(self.command,self.path,raw,headers);response=connection.getresponse();data=response.read()
                command=json.loads(raw) if self.path=='/v1/commands' else {}
                if command.get('id')=='native-lost' and response.status==200 and not lost:
                    lost=True;self.close_connection=True;self.connection.shutdown(socket.SHUT_RDWR);self.connection.close();return
                self.send_response(response.status)
                for key,value in response.getheaders():
                    if key.lower() not in ('content-length','connection','transfer-encoding'):self.send_header(key,value)
                self.send_header('Content-Length',str(len(data)));self.end_headers();self.wfile.write(data)
            except (BrokenPipeError,ConnectionResetError,OSError):pass
            finally:connection.close()
    proxy=Server(('127.0.0.1',0),ProductProxy);threading.Thread(target=proxy.serve_forever,daemon=True).start()
    class Control(Quiet):
        def do_POST(self):
            global released
            if self.path=='/status':
                listener=socket.socket();listener.settimeout(.5)
                alive=listener.connect_ex((node_origin.hostname,node_origin.port))==0;listener.close()
                with lock:data={'counts':dict(counts),'node_alive':node.poll() is None,'host_alive':host.poll() is None,'listener_alive':alive,'response_dropped':lost}
                self.reply(data)
            elif self.path=='/release':released=True;self.reply({'released':True})
            elif self.path=='/stop':self.reply({'stopping':True});stop.set()
            else:self.send_error(404)
    control=Server(('127.0.0.1',0),Control);threading.Thread(target=control.serve_forever,daemon=True).start()
    ready={'endpoint':'http://127.0.0.1:'+str(proxy.server_port),'identity':metadata['identity'],'control':'http://127.0.0.1:'+str(control.server_port),'synthetic':True,'native_version':'0.65.0'}
    (root/'processes.json').write_text(json.dumps({'supervisor':os.getpid(),'host':host.pid,'node':node.pid}))
    (root/'fixture-ready.json').write_text(json.dumps(ready))
    while not stop.wait(.1):
        if host.poll() is not None:raise RuntimeError('owned native Host exited')
finally:
    stop.set();released=True
    for child in (node,host):
        if child is not None and child.poll() is None:
            child.send_signal(signal.SIGTERM)
            try:child.wait(timeout=15)
            except subprocess.TimeoutExpired:child.kill();child.wait()
    provider.shutdown();node_output.close();node_log.close();host_log.close()
    (root/'fixture-stopped.json').write_text(json.dumps({'owned_children_reaped':True}))
