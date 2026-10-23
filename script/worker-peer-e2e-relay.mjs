// Transparent, test-only HTTP fault relay. All product responses originate at B.
import http from 'node:http';
export async function faultRelay(address){
 const target=new URL(address);let armed,held;const effects=[];
 const server=http.createServer((incoming,outgoing)=>{
  let selected,prefix='',upstream;
  upstream=http.request({hostname:target.hostname,port:target.port,path:incoming.url,method:incoming.method,headers:{...incoming.headers,host:target.host}},response=>{
   if(selected?.kind==='drop'){
    response.resume();response.once('end',()=>{selected.responseLost=true;outgoing.destroy();});return;
   }
   outgoing.writeHead(response.statusCode,response.headers);response.pipe(outgoing);
  });
  upstream.on('error',()=>outgoing.destroy());
  incoming.on('data',chunk=>{
   if(!prefix){prefix=chunk.subarray(0,8192).toString();const action=prefix.match(/"action":"([^"]+)"/)?.[1];
    if(incoming.url==='/worker/v1/call'&&armed?.action===action){
     selected={kind:armed.kind,action,requestId:prefix.match(/"requestId":"([^"]+)"/)?.[1],task:prefix.match(/"ID":"([^"]+)"/)?.[1],received:+incoming.headers['content-length'],forwarded:0,cancelled:false};effects.push(selected);armed=undefined;
    }
   }
   if(selected?.kind==='hold'){
    if(!held){const part=chunk.subarray(0,1024);upstream.write(part);selected.forwarded+=part.length;held=selected;}return;
   }
   upstream.write(chunk);
  });
  // Drain A while delaying the rest on B. Pausing the incoming socket would
  // also hide the client's FIN, making cancellation itself unobservable.
  incoming.on('end',()=>{if(selected?.kind!=='hold')upstream.end();});
  const abort=()=>{if(selected?.kind==='hold')selected.cancelled=true;upstream.destroy();};
  incoming.on('aborted',abort);outgoing.on('close',()=>{if(!outgoing.writableEnded)abort();});
 });
 await new Promise(r=>server.listen(0,'127.0.0.1',r));
 return {url:'http://127.0.0.1:'+server.address().port,effects,
  drop(action){armed={kind:'drop',action};},holdStart(){held=undefined;armed={kind:'hold',action:'start'};},held:()=>held,
  async close(){server.closeAllConnections();await new Promise(r=>server.close(r));},
 };
}
