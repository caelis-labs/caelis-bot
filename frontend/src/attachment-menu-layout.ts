export type MenuLayout = { left:number; top:number; width:number; height:number };

// Match the actual composer box above the editor (not a display:contents
// action row). Scroll the menu when its list
// exceeds the available reading area; never grow the composer itself.
export function attachmentMenuLayout(anchor:{left:number;top:number;bottom:number;width:number}, viewport:{width:number;height:number}, desiredHeight:number):MenuLayout {
 const margin=12,gap=8,width=Math.min(anchor.width,Math.max(0,viewport.width-margin*2));
 const above=Math.max(0,Math.min(anchor.top,viewport.height)-gap-margin);
 const height=Math.min(desiredHeight,above);
 return {left:Math.max(margin,Math.min(anchor.left,viewport.width-width-margin)),top:Math.max(margin,anchor.top-gap-height),width,height};
}
