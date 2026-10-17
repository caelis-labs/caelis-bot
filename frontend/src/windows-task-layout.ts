export type TaskLayout={width:number;height:number;vertical:boolean;reverse:boolean};
export function taskCardFrames(layout:TaskLayout,count:number) {
 const {width,height,vertical,reverse}=layout;
 const stackHeight=Math.max(1,height-(vertical?48:0)),cardWidth=Math.min(224,Math.max(1,width-16)),cardHeight=Math.min(vertical?144:192,Math.max(1,stackHeight-4));
 const span=Math.max(0,vertical?stackHeight-cardHeight-4:width-cardWidth-32),stride=count>1?Math.min(vertical?116:156,span/(count-1)):0;
 return Array.from({length:count},(_,index)=>({left:vertical?8:8+index*stride,top:vertical&&reverse?stackHeight-cardHeight-4-index*stride:vertical?index*stride:0,width:cardWidth,height:cardHeight}));
}
