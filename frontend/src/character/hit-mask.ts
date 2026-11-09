// Each renderer owns its buffers. Rows retain WebGL's bottom-up orientation;
// native hit testing consumes bit i as pixel i, least-significant bit first.
export function createHitMaskEncoder(width:number,height:number):(rgba:Uint8Array)=>string {
 const horizontal=new Uint8Array(width*height),columns=new Uint8Array(width);
 const packed=new Uint8Array(Math.ceil(width*height/8)),previous=new Uint8Array(packed.length);
 let encoded:string|undefined;
 function encodePacked():string {
  let same=encoded!==undefined;
  for(let i=0;same&&i<packed.length;i++)same=packed[i]===previous[i];
  if(same)return encoded!;
  previous.set(packed);
  encoded=btoa(String.fromCharCode(...packed));return encoded;
 }
 return rgba=>{
  packed.fill(0);
  // A transparent readback needs no dilation, including immediately after an
  // opaque pose. Do not leave this common loading/clearing case more expensive.
  let source=3;
  for(;source<rgba.length&&rgba[source]<=16;source+=4){/* Find the first covered pixel. */}
  if(source>=rgba.length)return encodePacked();
  // A square radius-two dilation is exactly the horizontal radius-two pass
  // followed by the vertical pass. Sliding counts avoid 25 writes per source.
  for(let y=0;y<height;y++){
   const row=y*width;let count=0;
   for(let x=0;x<Math.min(3,width);x++)if(rgba[(row+x)*4+3]>16)count++;
   for(let x=0;x<width;x++){
    horizontal[row+x]=count>0?1:0;
    if(x>=2&&rgba[(row+x-2)*4+3]>16)count--;
    if(x+3<width&&rgba[(row+x+3)*4+3]>16)count++;
   }
  }
  columns.fill(0);
  for(let y=0;y<Math.min(3,height);y++)for(let x=0;x<width;x++)columns[x]+=horizontal[y*width+x];
  for(let y=0;y<height;y++){
   const row=y*width,remove=(y-2)*width,add=(y+3)*width;
   for(let x=0;x<width;x++){
    const i=row+x;if(columns[x]>0)packed[i>>3]|=1<<(i&7);
    if(y>=2)columns[x]-=horizontal[remove+x];
    if(y+3<height)columns[x]+=horizontal[add+x];
   }
  }
  // An unchanged pose still gets a fresh GPU readback and exact mask, but
  // needs neither another binary string nor another base64 conversion.
  return encodePacked();
 };
}
