import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {join} from 'node:path';

const [views,flow]=process.argv.slice(2);
if(!views||!flow)throw Error('Provide the native view and interaction fixture directories');
const read=(dir,name)=>JSON.parse(readFileSync(join(dir,`plugins-${name}-zh-CN-light-${name==='plugin-mixed'||name==='plugin-disable-error'?'640':'860'}.png.json`),'utf8'));
const skill=read(views,'empty'),installed=read(views,'plugin-installed'),mcp=read(views,'plugin-mcp'),mixed=read(views,'plugin-mixed'),issue=read(views,'plugin-error'),failed=read(views,'plugin-disable-error'),loading=read(views,'plugin-disable-loading');
assert.deepEqual([skill.skills,skill.mcp,mcp.skills,mcp.mcp,mixed.skills,mixed.mcp],[1,0,0,1,1,1]);
for(const item of [skill,installed,mcp,mixed,issue])assert.deepEqual(item.calls,[],'Viewing a package must not activate it');
assert.equal(installed.rows,1);
assert.equal(failed.error,true);
assert.deepEqual(failed.calls,[['PluginAction','markdown-work','disable']]);
assert.equal(loading.loading,true);
assert.deepEqual(loading.calls,[['PluginAction','markdown-work','disable']]);
if('issue' in issue)assert.equal(issue.issue,true);
if('installedTab' in installed)assert.equal(installed.installedTab,true);
const interaction=JSON.parse(readFileSync(join(flow,'plugins-empty-zh-CN-light-860.png.json'),'utf8'));
for(const key of ['opened','detail','installed','disabled','enabled','confirmed','available'])assert.equal(interaction[key],true,key);
if('keyboardFocus' in interaction){assert.equal(interaction.keyboardFocus,true);assert.equal(interaction.escapeFocus,true)}
assert.deepEqual(interaction.calls,[['PluginAction','markdown-work','install'],['PluginAction','markdown-work','disable'],['PluginAction','markdown-work','enable'],['PluginAction','markdown-work','uninstall']]);
console.log('Native plugin view and action contracts passed');
