// Local expression only: these gestures do not infer task progress or authority.
export type AvatarGesture='rest'|'bounce'|'peek'|'nod'|'listen'|'look-around';
export type AvatarPose={gazeX:number;gazeY:number;tilt:number;openness:number;offsetX:number;offsetY:number;scaleX:number;scaleY:number;gesture:AvatarGesture};
export const neutralAvatar:AvatarPose={gazeX:0,gazeY:0,tilt:0,openness:1,offsetX:0,offsetY:0,scaleX:1,scaleY:1,gesture:'rest'};
type Axis={value:number;velocity:number};
function spring(axis:Axis,target:number,dt:number,speed:number) {
 const displacement=axis.value-target,impulse=axis.velocity+speed*displacement,decay=Math.exp(-speed*dt);
 axis.value=target+(displacement+impulse*dt)*decay;
 axis.velocity=(axis.velocity-speed*impulse*dt)*decay;
}
type Beat={duration:number;x?:number;y?:number;tilt?:number;sx?:number;sy?:number;gazeX?:number;gazeY?:number};
const gestures:{name:AvatarGesture;beats:Beat[]}[]=[
 {name:'bounce',beats:[{duration:.24,y:.9,sx:1.05,sy:.95},{duration:.5,y:-3.2,sx:.98,sy:1.04,gazeY:-1.2},{duration:.26,y:.7,sx:1.04,sy:.96},{duration:.7}]},
 {name:'peek',beats:[{duration:.75,y:-2.4,sx:1.03,sy:1.03,gazeY:-2.1},{duration:.6,y:-2.4,gazeY:-2.1},{duration:.75}]},
 {name:'nod',beats:[{duration:.4,y:1.3,sy:.96,gazeY:1.4},{duration:.4,y:-.9,sy:1.02},{duration:.35,y:.9,sy:.97,gazeY:1},{duration:.7}]},
 {name:'listen',beats:[{duration:.65,x:2,y:-.6,tilt:-7,gazeX:3.5},{duration:.75,x:2,y:-.6,tilt:-7,gazeX:3.5},{duration:.8}]},
 {name:'look-around',beats:[{duration:.7,x:-1.4,tilt:-4,gazeX:-3.6},{duration:.85,x:1.4,tilt:4,gazeX:3.6},{duration:.8}]},
];
export function avatarMotion(random:()=>number=Math.random) {
 const axis=(value=0):Axis=>({value,velocity:0});
 const x=axis(),y=axis(),tilt=axis(),sx=axis(1),sy=axis(1),eyeX=axis(),eyeY=axis();
 let elapsed=0,nextGesture=.35,previous=-1,current=-1,beatIndex=0,beatEnd=0;
 let blinkAt=1.3,blinkStart=-1,lookAt=.3,lookX=0,lookY=0;
 return (seconds:number):AvatarPose=>{
  // No burst of missed gestures when returning from a suspended window.
  const dt=Number.isFinite(seconds)?Math.max(0,Math.min(seconds,.05)):0;elapsed+=dt;
  if(current<0&&elapsed>=nextGesture){
   current=previous<0?0:(previous+1+Math.floor(random()*(gestures.length-1)))%gestures.length;
   previous=current;beatIndex=0;beatEnd=elapsed+gestures[current].beats[0].duration;
  }
  if(current>=0&&elapsed>=beatEnd){
   beatIndex++;
   if(beatIndex===gestures[current].beats.length){current=-1;nextGesture=elapsed+.9+random()*1.2;}
   else beatEnd=elapsed+gestures[current].beats[beatIndex].duration;
  }
  if(elapsed>=lookAt){lookX=(random()<.5?-1:1)*(2.2+random()*1.2);lookY=-.8+random()*1.2;lookAt=elapsed+2+random()*1.8;}
  if(elapsed>=blinkAt){blinkStart=elapsed;blinkAt=elapsed+2.6+random()*2;}
  const beat:Beat=current<0?{duration:0}:gestures[current].beats[beatIndex];
  spring(x,beat.x??0,dt,10);spring(y,beat.y??0,dt,14);spring(tilt,beat.tilt??0,dt,8);
  spring(sx,beat.sx??1,dt,14);spring(sy,beat.sy??1,dt,14);
  spring(eyeX,beat.gazeX??lookX,dt,12);spring(eyeY,beat.gazeY??lookY,dt,12);
  const age=elapsed-blinkStart;
  const blink=blinkStart<0||age>.22?0:age<.08?age/.08:1-(age-.08)/.14;
  const clamp=(n:number,min:number,max:number)=>Math.max(min,Math.min(max,n));
  return {gazeX:clamp(eyeX.value,-3.8,3.8),gazeY:clamp(eyeY.value,-2.2,1.5),tilt:clamp(tilt.value,-8,8),openness:1-.96*Math.max(0,blink),
   offsetX:clamp(x.value,-2.2,2.2),offsetY:clamp(y.value,-3.5,1.5),scaleX:clamp(sx.value,.96,1.06),scaleY:clamp(sy.value,.94,1.05),gesture:current<0?'rest':gestures[current].name};
 };
}

type Clock={request:(callback:(time:number)=>void)=>number;cancel:(id:number)=>void};
export function animateAvatar(paint:(pose:AvatarPose)=>void,clock:Clock,random:()=>number=Math.random) {
 let running=false,frame=0,last:number|undefined,disposed=false,advance=avatarMotion(random);
 const tick=(time:number)=>{
  if(!running)return;
  if(last===undefined)last=time;
  if(time-last>=1000/30){paint(advance((time-last)/1000));last=time;}
  frame=clock.request(tick);
 };
 const setRunning=(next:boolean)=>{
  if(disposed||next===running)return;
  running=next;
  if(next){last=undefined;frame=clock.request(tick);}
  else{clock.cancel(frame);advance=avatarMotion(random);paint(neutralAvatar);}
 };
 return {setRunning,dispose(){setRunning(false);disposed=true;}};
}
