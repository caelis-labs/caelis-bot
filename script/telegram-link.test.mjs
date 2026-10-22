import {test} from 'node:test';
import assert from 'node:assert/strict';
import {telegramURL,telegramPhase} from '../frontend/src/telegram-state.ts';
const status={enabled:true,bot:'fixture_caelis_bot',owner:'',paired:false,candidate:'',pairURL:'https://t.me/fixture_caelis_bot?start=SYNTHETIC_pair%2D9&extra=keep%2Fvalue#fixture',issue:''};
test('pairing retains the complete payload and cannot degrade to a plain Bot URL',()=>{
 assert.equal(telegramURL(status,true),status.pairURL);
 assert.equal(new URL(telegramURL(status,true)).searchParams.get('start'),'SYNTHETIC_pair-9');
 assert.equal(telegramURL({...status,pairURL:''},true),'');
 assert.equal(telegramURL({...status,pairURL:'',paired:true},true),'https://t.me/fixture_caelis_bot');
 assert.equal(telegramURL(null,false),'https://t.me/BotFather');
});
test('pair links reject unsafe protocols, credentials, other origins or another Bot without exposing the URL',()=>{
 for(const pairURL of ['javascript:alert(1)','tg://resolve?domain=fixture_caelis_bot','http://t.me/fixture_caelis_bot?start=x','https://t.me.evil.invalid/fixture_caelis_bot?start=x','https://name:secret@t.me/fixture_caelis_bot?start=x','https://t.me:444/fixture_caelis_bot?start=x','https://t.me/other_bot?start=x','https://t.me/fixture_caelis_bot','https://t.me/fixture_caelis_bot?start=','not a URL'])assert.equal(telegramURL({...status,pairURL},true),'');
 assert.equal(telegramURL({...status,bot:'../other'},true),'');
});
test('waiting, candidate, connected, expired and network states remain distinct',()=>{
 assert.equal(telegramPhase(status,false,''),'pairing');
 assert.equal(telegramPhase({...status,candidate:'Synthetic Owner'},false,''),'candidate');
 assert.equal(telegramPhase({...status,paired:true,pairURL:''},false,''),'connected');
 assert.equal(telegramPhase({...status,pairURL:''},false,'pairing_expired'),'error');
 assert.equal(telegramPhase(status,false,'network'),'reconnecting');
 assert.equal(telegramPhase(status,false,'open_failed'),'pairing');
});
