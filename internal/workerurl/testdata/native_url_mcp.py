#!/usr/bin/env python3
"""Temporary credential-free model + real MCP URL elicitation, never a Codex simulator."""
import argparse, http.server, json, os, pathlib, re, secrets, sys, threading, time, urllib.parse, urllib.request

def emit(value): print(json.dumps(value), flush=True)
def request(base, path, data=None):
    req=urllib.request.Request(base+path,data=None if data is None else json.dumps(data).encode(),headers={'Content-Type':'application/json'})
    with urllib.request.urlopen(req,timeout=3) as response:return json.loads(response.read())

def mcp(base):
    original=None;challenge=None
    for line in sys.stdin:
        incoming=json.loads(line);method=incoming.get('method');rid=incoming.get('id')
        if method=='initialize':
            assert 'url' in incoming['params']['capabilities']['elicitation']
            result={'protocolVersion':'2025-11-25','capabilities':{'tools':{}},'serverInfo':{'name':'isolated_url_mock','version':'1'}}
        elif method=='tools/list':
            result={'tools':[{'name':'challenge','description':'One isolated native URL challenge; mock model and MCP only.','inputSchema':{'type':'object','properties':{'case':{'type':'string'}},'required':['case'],'additionalProperties':False},'annotations':{'readOnlyHint':False,'openWorldHint':False}}]}
        elif method=='tools/call':
            assert original is None,'unknown/repeated original native call must not replay'
            original=rid;case=incoming['params']['arguments']['case']
            challenge=request(base,'/register',{'case':case,'mcpPID':os.getpid(),'toolCallID':rid})
            emit({'jsonrpc':'2.0','id':challenge['elicitationID'],'method':'elicitation/create','params':{'mode':'url','message':'Isolated Mock MCP '+case+': complete the original one-use page and native consent.','elicitationId':challenge['elicitationID'],'url':base+'/challenge/'+challenge['nonce']}})
            continue
        elif challenge and rid==challenge['elicitationID'] and 'result' in incoming:
            action=incoming['result']['action']
            request(base,'/decision',{'nonce':challenge['nonce'],'action':action})
            outcome='MOCK_URL_DECLINED'
            if action=='accept':
                deadline=time.monotonic()+20
                while time.monotonic()<deadline:
                    state=request(base,'/state')['cases'][challenge['case']]
                    if state['webEffects']==1:
                        request(base,'/native',{'nonce':challenge['nonce']});outcome='MOCK_URL_NATIVE_CONFIRMED';break
                    if state['expired']:break
                    time.sleep(.05)
            emit({'jsonrpc':'2.0','method':'notifications/elicitation/complete','params':{'elicitationId':challenge['elicitationID']}})
            emit({'jsonrpc':'2.0','id':original,'result':{'content':[{'type':'text','text':outcome}],'isError':outcome!='MOCK_URL_NATIVE_CONFIRMED'}})
            continue
        elif rid is None: continue
        elif method=='ping': result={}
        else:
            emit({'jsonrpc':'2.0','id':rid,'error':{'code':-32601,'message':'Unsupported isolated mock method'}});continue
        emit({'jsonrpc':'2.0','id':rid,'result':result})

def serve():
    lock=threading.RLock();cases={};model_steps={};environment='';model_evidence=[]
    def by_nonce(value):return next((v for v in cases.values() if v['nonce']==value),None)
    class Handler(http.server.BaseHTTPRequestHandler):
        protocol_version='HTTP/1.1'
        def log_message(self,*_):pass
        def send(self,status,body=None,kind='application/json'):
            raw=body if isinstance(body,bytes) else json.dumps(body or {}).encode()
            self.send_response(status);self.send_header('Content-Type',kind);self.send_header('Content-Length',str(len(raw)));self.end_headers();self.wfile.write(raw)
        def do_GET(self):
            if self.path=='/state':
                with lock:self.send(200,{'cases':cases,'models':model_evidence})
                return
            if self.path.startswith('/challenge/'):
                nonce=self.path.split('/')[-1]
                with lock:v=by_nonce(nonce)
                if not v or v['expired']:return self.send(410)
                if self.headers.get('Host')!=authority:return self.send(403)
                self.send_response(200);self.send_header('Content-Type','text/html; charset=utf-8');self.send_header('Set-Cookie','challenge='+nonce+'; HttpOnly; SameSite=Strict; Path=/challenge/');self.send_header('Connection','close');self.end_headers()
                self.wfile.write(('<!doctype html><title>Isolated '+v['case']+' challenge</title><form method="POST"><input name="nonce" type="hidden" value="'+nonce+'"><label for="answer">Mock confirmation</label><input id="answer" name="answer" required><button>Complete original challenge</button></form>').encode());self.close_connection=True;return
            self.send(404)
        def do_POST(self):
            nonlocal environment
            assert not self.headers.get('Authorization'),'no account credential belongs in this mock'
            data=self.rfile.read(min(int(self.headers.get('Content-Length','0')),4<<20))
            if self.path.startswith('/challenge/'):
                nonce=self.path.split('/')[-1]
                with lock:
                    v=by_nonce(nonce)
                    if not v or v['expired']:return self.send(410)
                    values=urllib.parse.parse_qs(data.decode())
                    if self.headers.get('Host')!=authority or self.headers.get('Origin')!=base or values!={'nonce':[nonce],'answer':['synthetic-confirmation']} or 'challenge='+nonce not in self.headers.get('Cookie',''):return self.send(403)
                    if v['webEffects']:return self.send(409)
                    v['webEffects']+=1
                    if v['case']=='WEB_LOSS':self.close_connection=True;self.connection.shutdown(2);self.connection.close();return
                return self.send(200,{'complete':True})
            value=json.loads(data or '{}')
            with lock:
                if self.path=='/environment':environment=value['id'];return self.send(200)
                if self.path=='/register':
                    case=value['case'];assert case not in cases,'native tool replayed'
                    v={**value,'nonce':secrets.token_hex(16),'elicitationID':'mock-native-'+secrets.token_hex(10),'webEffects':0,'nativeEffects':0,'decisions':[],'expired':False,'continuations':0};cases[case]=v
                    return self.send(200,v)
                if self.path=='/expire':cases[value['case']]['expired']=True;return self.send(200)
                if self.path=='/decision':
                    v=by_nonce(value['nonce']);assert v is not None
                    v['decisions'].append(value['action']);return self.send(200)
                if self.path=='/native':
                    v=by_nonce(value['nonce']);assert v and v['webEffects']==1 and v['decisions']==['accept'] and v['nativeEffects']==0
                    v['nativeEffects']+=1;return self.send(200)
            match=re.fullmatch('/([AB])/v1/responses',self.path)
            if not match:return self.send(404)
            owner=match[1];inp=value.get('input',[])
            # Synthetic test prompts only; never store the whole native context.
            text=json.dumps(inp);tags=re.findall(r'URL_CASE=([A-Z_]+)',text);case=tags[-1] if tags else 'IDLE'
            with lock:
                key=(owner,case);step=model_steps.get(key,0)+1;model_steps[key]=step
                model_evidence.append({'owner':owner,'case':case,'step':step,'toolOutputs':[str(v.get('output',''))[:2000] for v in inp if v.get('type')=='function_call_output']})
                if case!='IDLE' and step==1:
                    namespace='mcp__caelis_tasks' if owner=='A' else 'mcp__isolated_url_mock'
                    name='bot_delegate' if owner=='A' else 'challenge'
                    args={'prompt':'URL_CASE='+case,'title':'Real URL '+case,'environment':environment} if owner=='A' else {'case':case}
                    item={'type':'function_call','id':'mock-tool-'+owner+'-'+case,'namespace':namespace,'name':name,'arguments':json.dumps(args),'call_id':'mock-call-'+owner+'-'+case}
                else:
                    confirmed=any(v.get('type')=='function_call_output' and 'MOCK_URL_NATIVE_CONFIRMED' in str(v.get('output','')) for v in inp)
                    if owner=='B' and confirmed:cases[case]['continuations']+=1
                    text=('REAL_CODEX_URL_'+case+'_CONFIRMED' if confirmed else 'REAL_CODEX_'+owner+'_'+case+'_IDLE')
                    item={'type':'message','id':'mock-final-'+owner+'-'+case+'-'+str(step),'role':'assistant','status':'completed','phase':'final_answer','content':[{'type':'output_text','text':text,'annotations':[]}]}
            rid='mock-'+owner+'-'+case+'-'+str(step)
            events=[{'type':'response.created','response':{'id':rid,'status':'in_progress','output':[]}}, {'type':'response.output_item.done','output_index':0,'item':item}, {'type':'response.completed','response':{'id':rid,'status':'completed','output':[item],'usage':{'input_tokens':1,'output_tokens':1,'total_tokens':2}}}]
            self.send(200,''.join('data: '+json.dumps(v)+'\n\n' for v in events).encode(),'text/event-stream')
    server=http.server.ThreadingHTTPServer(('127.0.0.1',0),Handler);server.daemon_threads=True
    authority='127.0.0.1:'+str(server.server_port);base='http://'+authority
    emit({'url':base})
    try:server.serve_forever()
    finally:server.server_close()
if __name__=='__main__':
    parser=argparse.ArgumentParser();parser.add_argument('--mcp');args=parser.parse_args()
    if args.mcp:mcp(args.mcp)
    else:serve()
