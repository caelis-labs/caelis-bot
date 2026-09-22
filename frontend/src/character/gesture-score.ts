import type { PerformanceFrame } from './performance';

const smooth=(x:number)=>{x=Math.max(0,Math.min(1,x));return x*x*x*(x*(x*6-15)+10);};
type Point=[number,number,number];
type Score={wrists:Partial<Record<'left'|'right',Point>>;poles:Partial<Record<'left'|'right',Point>>;shift:number;drop:number;roll:number;pitch:number;chest:Point;head:Point;rock:number};
/** Choreography in character space (Y up, Z toward the viewer).
 * Elbows have their own intent: a lifted hand must not keep the rest-pose pole.
 * These are local performances, never task state or permission signals.
 */
export function gestureScore(frame:PerformanceFrame,relaxed=false):Score{
 const p=frame.phase,w=frame.amount;
 const beat=Math.sin((p-.22)*Math.PI*6)*smooth((p-.18)/.12)*(1-smooth((p-.70)/.1));
 const look=smooth((p-.46)/.16);
 const common={shift:0,drop:0,roll:0,pitch:0,chest:[0,0,0] as Point,head:[0,0,0] as Point,rock:0};
 switch(frame.gesture){
  case 'wave':return {...common,
   wrists:{left:[(relaxed?.36:.335)+.022*beat,relaxed?1.07:1.105,.215] as Point},
   poles:{left:[.44,.79,.22] as Point},
   shift:.010*w,drop:.010*w,roll:-.025*w,pitch:-.015*w,
   chest:[-.025*w,-.025*w,-.030*w] as Point,
   head:[-.035*w,.055*w,(-.105+.016*beat)*w] as Point,rock:.18*beat*w};
  case 'think':return {...common,
   wrists:{...(!relaxed?{left:[.22,.83,.29] as Point}:{}),right:[-.125,1.065,.24] as Point},
   poles:{...(!relaxed?{left:[.36,.74,.10] as Point}:{}),right:[relaxed?-.30:-.34,.77,relaxed?.14:.11] as Point},
   shift:.015*w,drop:.008*w,roll:-.038*w,pitch:.035*w,
   chest:[.025*w,.075*w,.06*w] as Point,
   head:[(.085-.035*look)*w,-.105*look*w,.125*w] as Point};
  case 'ask':return {...common,
   wrists:{left:[relaxed?.39:.405,relaxed?.86:.945,.235] as Point,...(!relaxed?{right:[-.405,.925,.235] as Point}:{})},
   poles:{left:[relaxed?.28:.30,relaxed?.77:.76,.09] as Point,...(!relaxed?{right:[-.30,.76,.09] as Point}:{})},
   shift:-.012*w,drop:.012*w,roll:.025*w,pitch:.055*w,
   chest:[.025*w,-.04*w,-.035*w] as Point,
   head:[-.035*w,.055*w,-.14*w] as Point};
  default:return {...common,wrists:{},poles:{}};
 }
}
