import test from 'node:test';
import assert from 'node:assert/strict';
import {verifyCandidateRun} from './verify-candidate-run.mjs';
import {syncLatest} from './sync-github-latest.mjs';
import {releaseChannel} from './release-version.mjs';
import {updateURL, updateURLFor} from './configure-updates.mjs';

test('Dev and local bundles cannot point Sparkle at the Stable feed', () => {
  assert.equal(updateURLFor('v1.2.3'),updateURL);
  assert.match(updateURLFor('v1.2.3-dev.1'),/\/dev\/appcast\.xml$/);
  assert.match(updateURLFor(),/\/local-disabled\/appcast\.xml$/);
});

test('candidate provenance requires the successful manual preparation workflow on main', () => {
  const run={id:123,repository:{full_name:'caelis-labs/caelis-bot'},path:'.github/workflows/release.yml',event:'workflow_dispatch',head_branch:'main',status:'completed',conclusion:'success'};
  verifyCandidateRun(run,'caelis-labs/caelis-bot','123');
  for (const changed of [{conclusion:'failure'},{head_branch:'feature'},{path:'.github/workflows/ci.yml'},{id:124}]) {
    assert.throws(()=>verifyCandidateRun({...run,...changed},'caelis-labs/caelis-bot','123'),/Candidate/);
  }
});
test('GitHub Latest moves only after Stable receipt and retains a newer version', () => {
  const tag='v1.2.3';
  let latest={tag_name:'v1.2.2'}, editCount=0, published=[{tag_name:tag,draft:false,prerelease:false}];
  const run=(_cmd,args)=>{
    if(args[0]==='release') {editCount++;latest={tag_name:tag};return '';}
    if(args[1].endsWith(`/releases/tags/${tag}`)) return JSON.stringify({tag_name:tag,draft:false,prerelease:false,assets:[{name:'Caelis-Bot-1.2.3-macos-arm64-stable.publication.json'}]});
    if(args[1].endsWith('/releases?per_page=100')) return JSON.stringify(published);
    if(args[1].endsWith('/releases/latest')) return JSON.stringify(latest);
    throw new Error('Unexpected call');
  };
  assert.equal(releaseChannel('v1.2.3-dev.1'),'dev');
  assert.throws(()=>syncLatest('v1.2.3-dev.1',run),/Only Stable/);
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
