// Reference implementation from main 7dd17b9b, kept outside production code.
export function legacyHitMask(rgba,width,height) {
 const mask=new Uint8Array(width*height);
 for(let y=0;y<height;y++)for(let x=0;x<width;x++){
  if(rgba[(y*width+x)*4+3]<=16)continue;
  for(let dy=-2;dy<=2;dy++)for(let dx=-2;dx<=2;dx++)if(x+dx>=0&&x+dx<width&&y+dy>=0&&y+dy<height)mask[(y+dy)*width+x+dx]=1;
 }
 const packed=new Uint8Array(Math.ceil(mask.length/8));
 for(let i=0;i<mask.length;i++)if(mask[i])packed[i>>3]|=1<<(i&7);
 return btoa(String.fromCharCode(...packed));
}

export function maskFrames(width=180,height=240) {
 const blank=new Uint8Array(width*height*4),body=new Uint8Array(blank.length),shifted=new Uint8Array(blank.length),dense=new Uint8Array(blank.length);
 for(let y=0;y<height;y++)for(let x=0;x<width;x++){
  const i=(y*width+x)*4+3;
  dense[i]=255;
  if(y>height/12&&y<height*11/12&&Math.abs(x-width/2)<width*(.12+.16*Math.sin(y/height*Math.PI)))body[i]=255;
  if(y>height/12&&y<height*11/12&&Math.abs(x-width/2-3)<width*(.12+.16*Math.sin(y/height*Math.PI)))shifted[i]=255;
 }
 return {blank,body,shifted,dense};
}
