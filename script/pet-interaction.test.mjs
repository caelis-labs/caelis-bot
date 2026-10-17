import assert from 'node:assert/strict';
import {test,after} from 'node:test';
import {createServer} from 'vite';
const server=await createServer({server:{middlewareMode:true,hmr:false},appType:'custom'});
after(()=>server.close());
const {DragMotion}=await server.ssrLoadModule('/src/character/drag-run.ts');

test('shared running motion responds to native pointer samples, pauses and clears after release',()=>{
 const motion=new DragMotion();
 const snapshot=(time,x,dragging)=>({sampledAtUptime:time,actor:{width:180},pointer:{x,y:100},interaction:{dragging}});
 for(let i=0;i<30;i++)motion.update(1/30,snapshot(i/30,i*12,true));
 assert.ok(motion.amount>.8,'native dragging samples must produce the running pose');
 assert.ok(motion.pace>0&&motion.yaw>0,'movement speed and direction must reach the shared gait');
 const stationary=snapshot(1,348,true);
 for(let i=0;i<30;i++)motion.update(1/30,stationary);
 assert.ok(motion.amount<.2,'holding without movement must settle');
 for(let i=0;i<60;i++)motion.update(1/30,snapshot(2+i/30,348,false));
 assert.ok(Math.abs(motion.amount)<.01,'release/cancel must end running');
});

test('native 8 Hz observations drive the 30 fps renderer without requiring a new sample each frame',()=>{
 const motion=new DragMotion();let context;
 for(let frame=0;frame<60;frame++){
  const sample=Math.floor(frame/30*8);
  if(context?.revision!==sample)context={revision:sample,sampledAtUptime:sample/8,actor:{width:180},pointer:{x:-sample*60,y:100},interaction:{dragging:true}};
  motion.update(1/30,context);
 }
 assert.ok(motion.amount>.8,'native sampling cadence must sustain running between observations');
 assert.ok(motion.yaw<-.5,'moving left must face left, including negative desktop coordinates');
 for(let frame=0;frame<60;frame++)motion.update(1/30,context);
 assert.ok(motion.amount<.01,'stale pointer observations must not leave the pet running');
 motion.reset();
 assert.equal(motion.amount,0,'hide/reduced-motion reset clears the running pose');
 assert.equal(motion.pace,0);
 assert.equal(motion.yaw,0);
});
