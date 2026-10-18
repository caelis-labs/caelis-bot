import {test} from 'node:test';
import assert from 'node:assert/strict';
import {connectionNoticeGrace,connectionNoticeKey} from '../frontend/src/connection-notice.ts';

const state={connection:'ready',connectionIssue:'',message:'',phase:'idle',introductionPending:false};
test('Windows handshake and empty automatic recovery get a bounded presentation grace',()=>{
 for(const connection of ['connecting','offline']) {
  const value={...state,connection,connectionIssue:'connection_lost'};
  assert.equal(connectionNoticeGrace(value,true),750);
  assert.notEqual(connectionNoticeKey(value),'');
  assert.equal(value.connection,connection); // never project a false ready state
  assert.equal(connectionNoticeGrace(value,false),0); // Mac behavior is unchanged
 }
});
test('ready and pending introductions cancel a delayed reconnect notice',()=>{
 assert.equal(connectionNoticeKey(null),'');
 assert.equal(connectionNoticeKey(state),'');
 assert.equal(connectionNoticeKey({...state,connection:'connecting',introductionPending:true}),'');
});
test('authentication, setup, explicit failures and uncertain work are immediate',()=>{
 const failures=[
  {...state,connection:'login'},
  {...state,connection:'offline',message:'Host refused the connection'},
  {...state,connection:'offline',connectionIssue:'runtime_missing'},
  {...state,connection:'offline',connectionIssue:'runtime_protocol'},
  {...state,connection:'offline',connectionIssue:'setup_required'},
  {...state,connection:'connecting',phase:'unknown'},
 ];
 for(const value of failures) {
  assert.equal(connectionNoticeGrace(value,true),0);
  assert.notEqual(connectionNoticeKey(value),'');
 }
 assert.notEqual(connectionNoticeKey(failures[1]),connectionNoticeKey({...state,connection:'offline'}));
});
