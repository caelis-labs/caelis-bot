import {test} from 'node:test';
import assert from 'node:assert/strict';
import {questionStatusKey} from '../frontend/src/question-presentation.ts';
test('closed states have distinct labels instead of unknown delivery',()=>{
 for(const [state,key]of [['answered','questionAnswered'],['dismissed','questionDismissed'],['expired','questionExpired'],['superseded','questionSuperseded'],['unknown','questionUnknown'],['dispatching','questionUnknown']])assert.equal(questionStatusKey(state),'chat.'+key);
});
