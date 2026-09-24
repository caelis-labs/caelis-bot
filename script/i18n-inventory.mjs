// A migration inventory, not a release gate: Chinese can be legitimate user
// fixtures, catalog text, prompts or comments. English literals need review too.
import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
function scan(dir){
 for(const entry of readdirSync(dir,{withFileTypes:true})){
  const name=join(dir,entry.name);
  if(entry.isDirectory()){if(!['i18n','wire','preview'].includes(entry.name))scan(name);continue;}
  if(!/\.(go|m|tsx?|html)$/.test(name)||name.endsWith('_test.go'))continue;
  const lines=readFileSync(name,'utf8').split('\n').flatMap((line,index)=>/\p{Script=Han}/u.test(line)?[index+1]:[]);
  if(lines.length)console.log(`${name}: ${lines.length} candidate lines (${lines.join(', ')})`);
 }
}
scan('frontend/src');scan('internal');
