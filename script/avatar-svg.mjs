import assert from 'node:assert/strict';

// The finished avatar is inlined in the chat DOM. Accept a small graphics-only
// dialect, never arbitrary SVG (scripts, CSS, links, images or SMIL).
export function validateAvatarSVG(text) {
 assert.ok(Buffer.byteLength(text)<=32*1024,'avatar SVG budget exceeded');
 assert.ok(!/[&!?]/.test(text),'SVG entities and declarations are not allowed');
 const tags=new Set(['svg','defs','linearGradient','stop','g','path','ellipse']);
 const numbers='[-+0-9.eE,\\s]+';
 const rules={
  xmlns:/^http:\/\/www\.w3\.org\/2000\/svg$/,viewBox:/^0 0 128 128$/,
  id:/^[a-z][a-z0-9-]*$/, 'data-avatar-part':/^(head|eye-left|eye-right|look-left|look-right)$/,
  fill:/^(none|#[a-fA-F0-9]{6}|url\(#[a-z][a-z0-9-]*\))$/,stroke:/^#[a-fA-F0-9]{6}$/,
  d:/^[MmLlHhVvCcSsQqTtAaZz0-9.eE,+\s-]+$/,
  transform:/^(?:(?:translate|scale|rotate)\([-+0-9.eE,\s]+\)\s*)+$/,
  gradientUnits:/^userSpaceOnUse$/,'stroke-linecap':/^round$/,'stroke-linejoin':/^round$/,
  'stop-color':/^#[a-fA-F0-9]{6}$/,
 };
 for(const key of ['x1','x2','y1','y2','cx','cy','rx','ry','offset','opacity','stroke-width'])rules[key]=new RegExp(`^${numbers}$`);
 const stack=[],ids=new Set(),references=[],parts=new Set();let offset=0,roots=0;
 for(const match of text.matchAll(/<([^<>]+)>/g)) {
  assert.equal(text.slice(offset,match.index).trim(),'','SVG text is not allowed');offset=match.index+match[0].length;
  const token=match[1],closing=token.startsWith('/'),selfClosing=token.endsWith('/');
  const tag=/^\/?([A-Za-z]+)/.exec(token)?.[1];assert.ok(tags.has(tag),'unsupported SVG element');
  if(closing){assert.equal(token,`/${tag}`);assert.equal(stack.pop(),tag,'unbalanced SVG');continue;}
  if(!stack.length){assert.equal(tag,'svg');assert.equal(++roots,1);}
  else assert.notEqual(tag,'svg','nested SVG is not allowed');
  const attributes=token.slice(tag.length,selfClosing?-1:undefined),seen=new Set();let end=0;
  for(const attr of attributes.matchAll(/\s+([A-Za-z][A-Za-z0-9-]*)="([^"<>]*)"/g)) {
   assert.equal(attributes.slice(end,attr.index).trim(),'','invalid SVG attribute');end=attr.index+attr[0].length;
   const [,key,value]=attr;assert.ok(rules[key]?.test(value),`unsupported SVG attribute: ${key}`);assert.ok(!seen.has(key));seen.add(key);
   if(key==='id'){assert.ok(!ids.has(value),'duplicate SVG id');ids.add(value);}
   if(key==='data-avatar-part'){assert.equal(tag,'g');assert.ok(!parts.has(value),'duplicate avatar layer');parts.add(value);}
   if(value.startsWith('url('))references.push(value.slice(5,-1));
  }
  assert.equal(attributes.slice(end).trim(),'','invalid SVG attributes');
  if(tag==='svg')assert.ok(seen.has('xmlns')&&seen.has('viewBox'));
  if(!selfClosing)stack.push(tag);
 }
 assert.equal(text.slice(offset).trim(),'');assert.equal(roots,1);assert.equal(stack.length,0,'unclosed SVG');
 for(const ref of references)assert.ok(ids.has(ref),'missing SVG gradient');
 assert.deepEqual([...parts].sort(),['head','eye-left','eye-right','look-left','look-right'].sort(),'avatar layers missing');
}
