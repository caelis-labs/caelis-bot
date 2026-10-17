import {test} from 'node:test';
import assert from 'node:assert/strict';
import {setupStage} from '../frontend/src/setup-stage.ts';
import {SetupActivation} from '../frontend/src/setup-activation.ts';
test('first run confirms a model before optional choices and editing the introduction',()=>{
 assert.equal(setupStage(false,true),'runtime');
 assert.equal(setupStage(true,true),'features');
 assert.equal(setupStage(true,false),'identity');
});
test('returning to connection selection keeps the model gate',()=>{
 assert.equal(setupStage(false,false),'runtime');
 assert.equal(setupStage(true,true),'features');
});

test('later pages can be edited while connection is pending, but first submission waits',async()=>{
 let release;let submitted=false;
 const connection=new Promise(resolve=>{release=resolve});
 const activation=new SetupActivation();
 const ready=activation.start(()=>connection);
 assert.equal(setupStage(true,true),'features');
 assert.equal(setupStage(true,false),'identity');
 const submit=activation.ready().then(ready=>{if(ready)submitted=true});
 await Promise.resolve();assert.equal(submitted,false);
 release();assert.equal(await ready,true);await submit;assert.equal(submitted,true);
});

test('failed preparation never dispatches or retries effects and allows a new selection',async()=>{
 const activation=new SetupActivation();let calls=0;
 assert.equal(await activation.start(async()=>{calls++;throw new Error('lost connection')}),false);
 assert.equal(await activation.ready(),false);assert.equal(await activation.ready(),false);assert.equal(calls,1);
 assert.equal(await activation.start(async()=>{}),true);
 assert.equal(await activation.ready(),true);
});

test('Back and a newer selection cannot be authorized by the earlier connection',async()=>{
 const activation=new SetupActivation();let oldReady,newReady;
 const old=activation.start(()=>new Promise(resolve=>{oldReady=resolve}));
 await Promise.resolve();const oldSubmit=activation.ready();
 activation.reset();assert.equal(await activation.ready(),false);
 const current=activation.start(()=>new Promise(resolve=>{newReady=resolve}));
 await Promise.resolve();oldReady();assert.equal(await old,false);assert.equal(await oldSubmit,false);
 let submitted=false;const submit=activation.ready().then(ready=>{submitted=ready});
 await Promise.resolve();assert.equal(submitted,false);
 newReady();assert.equal(await current,true);await submit;assert.equal(submitted,true);
 activation.reset();assert.equal(await activation.ready(),false);
});

test('an already connected installation may proceed until the user changes its selection',async()=>{
 const activation=new SetupActivation(true);assert.equal(await activation.ready(),true);
 activation.reset();assert.equal(await activation.ready(),false);
});
