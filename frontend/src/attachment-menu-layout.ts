export type MenuLayout = { left:number; top:number; width:number; height:number };

// Anchor the popover to +, outside the editor. Scroll the menu when its list
// exceeds the available reading area; never grow the composer itself.
export function attachmentMenuLayout(anchor:{left:number;top:number;bottom:number;width:number}, viewport:{width:number;height:number}, desiredHeight:number, desiredWidth=anchor.width):MenuLayout {
 const margin=12,gap=8,width=Math.min(desiredWidth,Math.max(0,viewport.width-margin*2));
 const above=Math.max(0,anchor.top-gap-margin),below=Math.max(0,viewport.height-anchor.bottom-gap-margin);
 const up=above>=desiredHeight||above>=below;
 const height=Math.min(desiredHeight,up?above:below);
 return {left:Math.max(margin,Math.min(anchor.left,viewport.width-width-margin)),top:up?anchor.top-gap-height:anchor.bottom+gap,width,height};
}
