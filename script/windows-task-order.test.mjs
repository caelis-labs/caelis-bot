import assert from 'node:assert/strict';
import test from 'node:test';
import {preserveTaskOrder} from '../frontend/src/windows-task-order.ts';

test('hover keeps card order while accepting status, removals and new cards', () => {
  const previous = [{id:'a',status:'running'}, {id:'b',status:'running'}, {id:'removed',status:'completed'}];
  const incoming = [{id:'b',status:'approval'}, {id:'new',status:'running'}, {id:'a',status:'completed'}];
  assert.deepEqual(preserveTaskOrder(previous,incoming,true), [incoming[2],incoming[0],incoming[1]]);
  assert.deepEqual(preserveTaskOrder(previous,incoming,false), incoming);
  assert.equal(previous[0].status,'running');
});
