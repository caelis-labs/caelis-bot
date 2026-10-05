import assert from 'node:assert/strict';
import {test} from 'node:test';
import {telegramPhase} from '../frontend/src/telegram-state.ts';
import {finishCaptureChoice} from '../frontend/src/setup-capture-choice.ts';

const base={enabled:false,bot:'',owner:'',paired:false,candidate:'',pairURL:'',issue:''};
test('Telegram uses native facts to distinguish setup, pairing, connection and recovery',()=>{
 assert.equal(telegramPhase(null,false,''),'loading');
 assert.equal(telegramPhase(base,false,''),'unconfigured');
 assert.equal(telegramPhase(base,true,''),'connecting');
 const pairing={...base,enabled:true,bot:'FixtureBot',pairURL:'https://t.me/FixtureBot?start=fixture'};
 assert.equal(telegramPhase(pairing,false,''),'pairing');
 assert.equal(telegramPhase({...pairing,candidate:'Fixture User'},false,''),'candidate');
 assert.equal(telegramPhase({...pairing,paired:true,owner:'Fixture User'},false,''),'connected');
 assert.equal(telegramPhase({...pairing,paired:true,issue:'network'},false,'network'),'reconnecting');
 assert.equal(telegramPhase({...pairing,enabled:false,paired:true},false,''),'paused');
 assert.equal(telegramPhase({...pairing,issue:'pairing_expired'},false,'pairing_expired'),'error');
 assert.equal(telegramPhase({...pairing,issue:'webhook'},false,'webhook'),'error');
});
test('first-run choice saves before guide completion and failed save keeps guide pending',async()=>{
 const calls=[];
 await finishCaptureChoice(async(method,...args)=>{calls.push([method,...args])},false,()=>calls.push(['done']));
 assert.deepEqual(calls,[['SetCaptureEnabled',false],['FinishFeatureGuide'],['done']]);
 const failed=[];
 await assert.rejects(finishCaptureChoice(async(method,...args)=>{failed.push([method,...args]);throw Error('write failed')},false,()=>failed.push(['done'])));
 assert.deepEqual(failed,[['SetCaptureEnabled',false]]);
 const markerFailed=[];
 await assert.rejects(finishCaptureChoice(async(method,...args)=>{markerFailed.push([method,...args]);if(method==='FinishFeatureGuide')throw Error('marker failed')},false,()=>markerFailed.push(['done'])));
 assert.deepEqual(markerFailed,[['SetCaptureEnabled',false],['FinishFeatureGuide']]);
});
