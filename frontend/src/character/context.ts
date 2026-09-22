export type Rect = { x:number; y:number; width:number; height:number };
export type DesktopContext = {
 revision:number;
 sampledAtUptime?:number;
 desktop:{ id:string; frame:Rect; workArea:Rect; scale:number };
 dock:{ edge:'left'|'right'|'bottom'|'unknown'; confidence:'estimated'|'unknown'; source:string; frame:Rect|null };
 activeWindow:{ application:string; id:number|null; observedAtUptime:number; frame:Rect|null; source:string };
 actor:Rect;
 pointer:{ x:number; y:number; hovering:boolean };
 interaction:{ pressing:boolean; dragging:boolean; input:boolean; menu:boolean };
};
export const interactionBusy = (v?:DesktopContext) => !!v && (v.interaction.pressing || v.interaction.dragging || v.interaction.input || v.interaction.menu);
