import test from 'node:test';
import assert from 'node:assert/strict';
import {syncLatest} from './sync-github-latest.mjs';
import {updateURL} from './configure-updates.mjs';

test('the installable app uses the existing stable Sparkle feed', () => {
  assert.equal(updateURL, 'https://releases.caelis.dev/caelis-bot/appcast.xml');
});

test('GitHub Latest follows a published receipt and retains a newer version', () => {
  const tag='v1.2.3';
  let latest={tag_name:'v1.2.2'}, editCount=0, published=[{tag_name:tag,draft:false,prerelease:false}];
  const run=(_cmd,args)=>{
    if(args[0]==='release') {editCount++;latest={tag_name:tag};return '';}
    if(args[1].endsWith(`/releases/tags/${tag}`)) return JSON.stringify({tag_name:tag,draft:false,prerelease:false,assets:[{name:'Caelis-Bot-1.2.3-macos-arm64-stable.publication.json'}]});
    if(args[1].endsWith('/releases?per_page=100')) return JSON.stringify(published);
    if(args[1].endsWith('/releases/latest')) return JSON.stringify(latest);
    throw new Error('Unexpected call');
  };
  assert.throws(()=>syncLatest('v1.2.3-dev.1',run),/stable semantic/);
  assert.match(syncLatest(tag,run),/now/);
  assert.equal(editCount,1);
  assert.match(syncLatest(tag,run),/already/);
  latest={tag_name:'v1.2.4'};
  assert.match(syncLatest(tag,run),/Newer/);
  assert.equal(editCount,1);
  latest={tag_name:'v1.2.2'};
  published=[...published,{tag_name:'v1.2.4',draft:false,prerelease:false}];
  assert.match(syncLatest(tag,run),/Newer published/);
  assert.equal(editCount,1);
});
