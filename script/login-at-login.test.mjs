import {test} from 'node:test';
import assert from 'node:assert/strict';
import {loginAtLoginMessage} from '../frontend/src/login-at-login.ts';

test('Windows Run registration has its own supported status, distinct from OS approval',()=>{
 assert.equal(loginAtLoginMessage({state:'registered',supported:true,registered:true,enabled:true}),'settings.loginAtLoginRegistered');
 assert.equal(loginAtLoginMessage({state:'unavailable',supported:true,registered:false,enabled:false}),'settings.loginAtLoginUnavailable');
 assert.equal(loginAtLoginMessage({state:'future-supported-state',supported:true}),'settings.loginAtLoginUnavailable');
 assert.equal(loginAtLoginMessage({state:'unsupported',supported:false}),'settings.loginAtLoginUnsupported');
});
test('Mac approval and on/off descriptions retain their native semantics',()=>{
 for(const [state,key] of [['on','On'],['off','Off'],['needsApproval','NeedsApproval']]){
  assert.equal(loginAtLoginMessage({state,supported:true}),`settings.loginAtLogin${key}`);
 }
});
