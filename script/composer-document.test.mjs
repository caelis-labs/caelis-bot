import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readComposer} from '../frontend/src/composer-document.ts';
// Minimal DOM nodes for deterministic CI; shapes below were captured from real
// Chromium clipboard paste. composer-paste-browser.cjs tests the full UI path.
class Element {
 constructor(tagName,children=[],dataset={}){
  this.nodeType=1;this.tagName=tagName;this.dataset=dataset;
  this.childNodes=children.map(child=>typeof child==='string'?{nodeType:3,textContent:child}:child);
  this.childNodes.forEach((child,i)=>{child.previousSibling=this.childNodes[i-1]??null;child.nextSibling=this.childNodes[i+1]??null;});
 }
}
globalThis.HTMLElement=Element;globalThis.Node={TEXT_NODE:3};
const br=()=>new Element('BR'),block=(...children)=>new Element('DIV',children),empty=()=>block(br());
test('clipboard logical lines keep empty, leading and trailing paragraphs',()=>{
 for(const [children,text] of [
  [['alpha',empty(),block('beta')],'alpha\n\nbeta'],
  [['alpha',empty(),empty(),block('beta')],'alpha\n\n\nbeta'],
  [[br(),empty(),block('alpha'),empty(),empty()],'\n\nalpha\n\n'],
  [['```go',block('func main() {'),empty(),block('\tfmt.Println("hello")'),block('}'),block('```')],'```go\nfunc main() {\n\n\tfmt.Println("hello")\n}\n```'],
  [['line one',br(),'line two',br()],'line one\nline two'],
  [[block('one'),'two'],'one\ntwo'],
  [[br()],''],
  [[br(),br()],'\n'],
  [['literal\n\ntext'],'literal\n\ntext'],
 ])assert.equal(readComposer(block(...children)).text,text);
});
test('atomic reference offset includes every preceding blank line',()=>{
 const token=new Element('SPAN',[],{reference:'plugin-original'});
 const doc=readComposer(block('😀 before',empty(),block(token,' after')));
 assert.deepEqual(doc,{text:'😀 before\n\n after',referenceIds:['plugin-original'],referencePositions:[{id:'plugin-original',offset:'😀 before\n\n'.length}]});
});
