import {test} from 'node:test';
import assert from 'node:assert/strict';
import {setupStage} from '../frontend/src/setup-stage.ts';
test('first run prepares a Runtime before optional choices and introduction',()=>{
 assert.equal(setupStage(false,true,true),'runtime');
 assert.equal(setupStage(true,true,true),'features');
 assert.equal(setupStage(true,false,true),'permissions');
 assert.equal(setupStage(true,false,false),'identity');
});
test('returning to permissions or losing Runtime readiness cannot expose identity prematurely',()=>{
 assert.equal(setupStage(false,false,false),'runtime');
 assert.equal(setupStage(true,false,true),'permissions');
 assert.equal(setupStage(true,true,false),'features');
});
