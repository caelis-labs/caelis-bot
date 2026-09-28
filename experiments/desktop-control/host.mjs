import {createInterface} from 'node:readline';
import {SemanticDesktop} from './semantic-driver.mjs';
const desktop=new SemanticDesktop();
const reply=value=>process.stdout.write(JSON.stringify(value)+'\n');
try {
  await desktop.bindFixture();
  const input=createInterface({input:process.stdin,crlfDelay:Infinity});
  for await (const line of input) {
    try {
      if(Buffer.byteLength(line)>65536)throw new Error('request_too_large');
      const request=JSON.parse(line);
      let state;
      if(request.name==='bot_desktop_observe') {
        if(!request.arguments||Object.keys(request.arguments).length)throw new Error('invalid_arguments');
        state=await desktop.observe();
      } else if(request.name==='bot_desktop_perform') state=await desktop.perform(request.arguments);
      else throw new Error('unknown_tool');
      reply({content:[{type:'text',text:JSON.stringify(state)}],structuredContent:state,isError:false});
    } catch(error) {
      const state={status:error.mayHaveActed?'unknown':'rejected',message:error.message};
      reply({content:[{type:'text',text:JSON.stringify(state)}],structuredContent:state,isError:true});
    }
  }
} catch {reply({content:[{type:'text',text:'Cua fixture driver unavailable'}],isError:true});process.exitCode=1;}
finally {await desktop.close();}
