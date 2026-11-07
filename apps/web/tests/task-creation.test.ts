import {test} from 'node:test';
import assert from 'node:assert/strict';
import {creationFailure} from '../src/task-creation.ts';
test('startup failure preserves task identity and unknown delivery never enables retry',()=>{
 assert.deepEqual(creationFailure('a',Object.assign(new Error('offline'),{status:0})),{taskId:'a',phase:'unknown',message:'卡片已创建，启动结果待确认；不会自动重复启动'});
 assert.equal(creationFailure('b',new Error('配置不可用')).phase,'failed');
 assert.match(creationFailure('b',new Error('配置不可用')).message,/卡片已创建，启动失败/);
});
