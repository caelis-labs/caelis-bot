// Development-only regression using the production component and real GPU pixels.
// Preserve the drawing buffer only in this fixture so assertions can read the
// last still frame after React commits. This does not restore a lost context.
import {StrictMode,useState} from 'react';
import {createRoot} from 'react-dom/client';
import {flushSync} from 'react-dom';
import {CharacterPreview} from '../CharacterPreview';
import type {Appearance} from '../appearance';
import '../style.css';

const contexts=new Map<HTMLCanvasElement,WebGLRenderingContext|WebGL2RenderingContext>();
const getContext=HTMLCanvasElement.prototype.getContext;
HTMLCanvasElement.prototype.getContext=function(this:HTMLCanvasElement,id:string,options?:object){
 const gl=Reflect.apply(getContext,this,[id,/^webgl2?$/.test(id)?{...options,preserveDrawingBuffer:true}:options]);
 if(/^webgl2?$/.test(id)&&gl&&'readPixels' in gl)contexts.set(this,gl);
 return gl;
} as typeof getContext;
const appearance=(basic:boolean):Appearance=>({revision:0,selection:{character:basic?'builtin:stick':'builtin:caelis',avatar:'follow'},model:'',avatar:'',basic,key:basic?'builtin:stick':'builtin:caelis'});
const frame=()=>new Promise<void>(resolve=>requestAnimationFrame(()=>resolve()));
const assert=(value:unknown,message:string)=>{if(!value)throw new Error(message);};
async function pixels() {
 await frame();
 const deadline=performance.now()+15000;
 while(performance.now()<deadline){
  const canvas=document.querySelector<HTMLCanvasElement>('.character-preview canvas');
  const gl=canvas&&contexts.get(canvas);
  if(canvas&&gl&&!document.querySelector('.character-preview figcaption')){
   assert(!gl.isContextLost(),'Preview reported ready with a lost WebGL context');
   const data=new Uint8Array(gl.drawingBufferWidth*gl.drawingBufferHeight*4);
   gl.readPixels(0,0,gl.drawingBufferWidth,gl.drawingBufferHeight,gl.RGBA,gl.UNSIGNED_BYTE,data);
   let visible=0,hash=2166136261;
   for(let i=0;i<data.length;i+=4){if(data[i+3])visible++;for(let j=0;j<4;j++)hash=Math.imul(hash^data[i+j],16777619);}
   assert(visible>100,'Preview reported ready without a rendered character');
   return {canvas,visible,hash:hash>>>0};
  }
  if(document.querySelector('.character-preview figcaption')?.textContent?.includes('Preview unavailable'))throw new Error('Preview failed to render');
  await frame();
 }
 throw new Error('Timed out waiting for a rendered preview');
}
function Regression(){
 const [model,setModel]=useState(false),[mounted,setMounted]=useState(false),[strict,setStrict]=useState(false),[busy,setBusy]=useState(false),[result,setResult]=useState('Not run');
 const [steps,setSteps]=useState<string[]>([]);
 const run=async()=>{
  setBusy(true);setResult('Running');setSteps([]);
  try{
   for(const strictMode of [false,true]){
    flushSync(()=>{setMounted(false);setStrict(strictMode);});
    const frames:{canvas:HTMLCanvasElement;visible:number;hash:number}[]=[];
    for(const basic of [false,true,false]){
     flushSync(()=>{setModel(basic);setMounted(true);});
     const rendered=await pixels();frames.push(rendered);
     assert([...contexts].filter(([node])=>node.isConnected).length===1,'Multiple active preview canvases');
     if(frames.length>1){const previous=frames.at(-2)!;assert(contexts.get(previous.canvas)!.isContextLost(),'Previous preview context was not released');}
     setSteps(old=>[...old,`${strictMode?'StrictMode':'Normal'} / ${basic?'Stick':'Caelis'}: ${rendered.visible} visible pixels, hash ${rendered.hash}`]);
    }
    assert(frames[0].hash!==frames[1].hash,'Changing the model did not change the rendered pixels');
    assert(frames[0].hash===frames[2].hash,'Returning to Caelis did not restore the rendered frame');
    flushSync(()=>setMounted(false));await frame();
    assert([...contexts.values()].every(gl=>gl.isContextLost()),'Unmount left a live preview context');
    setSteps(old=>[...old,`${strictMode?'StrictMode':'Normal'} / unmount: all contexts released`]);
   }
   setResult('PASS');
  }catch(error){setResult(`FAIL: ${String(error)}`);}finally{setBusy(false);}
 };
 const preview=mounted?<CharacterPreview appearance={appearance(model)}/>:null;
 return <main style={{maxWidth:760,margin:'30px auto',padding:24}}><h1>Character preview lifecycle regression</h1><p>Real WebGL: Caelis → Stick → Caelis, then unmount; repeated in StrictMode.</p><button disabled={busy} onClick={()=>void run()}>Run regression</button><h2 role="status">{result}</h2><ol>{steps.map((step,i)=><li key={i}>{step}</li>)}</ol>{strict?<StrictMode>{preview}</StrictMode>:preview}</main>;
}
createRoot(document.getElementById('root')!).render(<Regression/>);
