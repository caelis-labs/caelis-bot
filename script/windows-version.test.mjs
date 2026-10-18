import {test} from 'node:test';
import assert from 'node:assert/strict';
import {windowsVersion} from './windows-version.mjs';
import {releaseVersion} from './release-version.mjs';

test('Windows preview qualification is independent from the accepted macOS stable tag',()=>{
 assert.equal(windowsVersion('0.14.0','preview','v0.14.0-windows.preview.3',3).version,'0.14.0-windows.preview.3');
 assert.equal(releaseVersion('0.14.0','v0.14.0').version,'0.14.0');
 assert.throws(()=>releaseVersion('0.14.0','v0.14.0-windows.preview.3'));
 for(const tag of ['v0.14.0','v0.13.0-windows.preview.3','v0.14.0-windows.preview.4'])assert.throws(()=>windowsVersion('0.14.0','preview',tag,3));
 assert.throws(()=>windowsVersion('0.14.0','development','v0.14.0'));
});

test('untagged previews use local version, Dev identity and no release updates',()=>{
 const local=windowsVersion('0.14.0','preview','',1),release=windowsVersion('0.14.0','preview','v0.14.0-windows.preview.1',1);
 assert.equal(local.version,'0.14.0-windows.preview.1-local');
 assert.equal(local.identity,'development');assert.equal(local.releaseUpdates,false);
 assert.equal(release.identity,'windows-preview');assert.equal(release.releaseUpdates,true);
 assert.notEqual(local.version,release.version);
});
