export type MenuLayout = { left:number; top:number; width:number; height:number };

// Keep a history menu inside its viewport, above the entire composer whenever
// possible. The menu scrolls; the composer and conversation do not reflow.
export function attachmentMenuLayout(anchor:{left:number;top:number;bottom:number;width:number}, viewport:{width:number;height:number}, desiredHeight:number):MenuLayout {
 const margin=12,gap=8,width=Math.min(anchor.width,Math.max(0,viewport.width-margin*2));
 const above=Math.max(0,anchor.top-gap-margin),below=Math.max(0,viewport.height-anchor.bottom-gap-margin);
 const up=above>=desiredHeight||above>=below;
 const height=Math.min(desiredHeight,up?above:below);
 return {left:Math.max(margin,Math.min(anchor.left,viewport.width-width-margin)),top:up?anchor.top-gap-height:anchor.bottom+gap,width,height};
}
