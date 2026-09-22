/** Xiaoai expression semantics; weights are presentation data, never task facts. */
export const facialTargets = ['blinkLeft','blinkRight','happy','mouthClosed','mouthRound','eyeSmile','eyeFocused','browRaised','browFocused','mouthSmile','mouthFlat'] as const;
export type FacialTarget = typeof facialTargets[number];
export type ExpressionName = 'relaxed'|'soft_smile'|'joy'|'focused'|'expectant'|'curious'|'concerned'|'relieved';
export const expressionProfiles:Record<ExpressionName,Partial<Record<FacialTarget,number>>> = {
 relaxed:{mouthClosed:.88},
 soft_smile:{mouthSmile:.82,browRaised:.16},
 joy:{eyeSmile:1,browRaised:.25,mouthClosed:.10},
 focused:{eyeFocused:.82,browFocused:.5,mouthFlat:.92},
 expectant:{browRaised:.68,mouthClosed:.57,mouthSmile:.14},
 curious:{browRaised:.82,mouthRound:.62,mouthClosed:.12},
 // Reserved, restrained compositions; narrative failure/relief mapping is deferred to M3.
 concerned:{browRaised:.18,browFocused:.30,mouthFlat:.93},
 relieved:{eyeSmile:.45,mouthSmile:.88},
};
export const ease=(value:number)=>{const t=Math.max(0,Math.min(1,value));return t*t*t*(t*(t*6-15)+10);};
export const envelope=(phase:number,start=0,end=1,fade=.18)=>ease((phase-start)/fade)*ease((end-phase)/fade);
export const neutralFace=()=>Object.fromEntries(facialTargets.map(k=>[k,k==='mouthClosed'?.88:0])) as Record<FacialTarget,number>;
