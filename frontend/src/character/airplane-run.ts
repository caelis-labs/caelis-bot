/** A playful spread-arm run for the current small, long-haired character.
 * Angles are art direction, not a new rig or a native movement constraint.
 */
export function airplaneRun(phase:number,pace:number,side:1|-1=1){
 const beat=Math.sin(phase+(side===1?0:Math.PI));
 return {
  // Outward wings with a gentle rear sweep; retain a soft elbow, never a T-pose lock.
  upper:[side*.94,-.25+.028*beat,-.23-.035*pace] as const,
  flexion:.24+.035*Math.sin(phase+(side===1?0:Math.PI)-.35),
  lean:.22+.12*pace,
  bank:.018*Math.sin(phase),
  headLift:-.12-.035*pace,
  chestPitch:-.020*Math.sin(phase*2-.20),
  hairTrail:.38+.025*Math.sin(phase-.55),
  sideHairTrail:.38+.020*Math.sin(phase-.55),
 };
}
