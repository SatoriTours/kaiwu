const { test } = require('node:test');
const assert = require('node:assert/strict');
const { createDemoState, currentConversationId, dispatch } = require('../web-prototype-state.js');

function act(state, type, payload = {}) {
  const result = dispatch(state, { type, ...payload });
  assert.equal(result.error, null);
  return result;
}

test('starts_in_project_scope: project and task histories are separate', () => {
  const state = createDemoState();
  assert.equal(currentConversationId(state), 'project:kaiwu');
  assert.equal(state.selectedTaskId, null);
  const task = state.tasks[0];
  const selected = act(state, 'select_task', { taskId: task.id }).state;
  assert.equal(currentConversationId(selected), `task:kaiwu:${task.id}`);
  assert.notDeepEqual(selected.conversations[currentConversationId(selected)].messages,
    state.conversations['project:kaiwu'].messages);
});

test('add_topic_appends_before_other without mutating original state', () => {
  const state = createDemoState();
  const before = structuredClone(state);
  const result = act(state, 'add_topic', { name: '  新功能  ' });
  assert.equal(result.state.topics.at(-1).id, 'other');
  assert.equal(result.state.topics.at(-2).name, '新功能');
  assert.equal(result.state.topics.at(-2).id, result.createdId);
  assert.deepEqual(state, before);
});

test('topic_name_validation rejects empty, long and duplicate names, accepts Unicode and plain HTML text', () => {
  const state = createDemoState();
  for (const name of ['', '  ', '悟'.repeat(41), null, 42]) {
    const result = dispatch(state, { type: 'add_topic', name });
    assert.equal(result.error, 'invalid_name');
    assert.equal(result.state, state);
  }
  assert.equal(dispatch(state, { type: 'add_topic', name: state.topics[0].name }).error, 'duplicate_name');
  assert.equal(act(state, 'add_topic', { name: '🌱'.repeat(40) }).state.topics.at(-2).name, '🌱'.repeat(40));
  assert.equal(act(state, 'add_topic', { name: '<img src=x>' }).state.topics.at(-2).name, '<img src=x>');
});

test('delete_topic_preserves_task_and_conversation, including current selection and pending draft', () => {
  let state = createDemoState();
  const task = state.tasks[0];
  state = act(state, 'select_task', { taskId: task.id }).state;
  state = act(state, 'set_draft', { conversationId: currentConversationId(state), text: '继续修改' }).state;
  const before = structuredClone(state);
  const next = act(state, 'delete_topic', { topicId: task.topicId }).state;
  assert.equal(next.topics.some(topic => topic.id === task.topicId), false);
  assert.equal(next.selectedTaskId, task.id);
  assert.deepEqual(next.conversations, before.conversations);
  assert.deepEqual(next.tasks, before.tasks.map(item => item.topicId === task.topicId ? { ...item, topicId: 'other' } : item));
  assert.deepEqual(state, before);
});

test('protect_other and reject unknown topic without changing state', () => {
  const state = createDemoState();
  for (const [topicId, error] of [['other', 'protected_topic'], ['missing', 'not_found']]) {
    const result = dispatch(state, { type: 'delete_topic', topicId });
    assert.equal(result.error, error);
    assert.equal(result.state, state);
  }
});

test('delete_empty_topic keeps all tasks and histories', () => {
  const added = act(createDemoState(), 'add_topic', { name: '空专题' });
  const next = act(added.state, 'delete_topic', { topicId: added.createdId }).state;
  assert.deepEqual(next.tasks, added.state.tasks);
  assert.deepEqual(next.conversations, added.state.conversations);
});

test('stale_topic_new_task_falls_back and preserves project draft', () => {
  let state = createDemoState();
  state = act(state, 'set_draft', { conversationId: 'project:kaiwu', text: '项目草稿' }).state;
  const result = act(state, 'add_task', { topicId: 'deleted', title: '', request: '🌱'.repeat(24) });
  const task = result.state.tasks.find(item => item.id === result.createdId);
  assert.equal(task.topicId, 'other');
  assert.equal(task.title, '🌱'.repeat(20));
  assert.equal(task.status, 'idle');
  assert.equal(result.state.selectedTaskId, task.id);
  assert.equal(result.state.conversations['project:kaiwu'].draft, '项目草稿');
  assert.ok(result.notice);
});

test('new task validation leaves state unchanged on invalid input', () => {
  const state = createDemoState();
  for (const payload of [{request:'  '}, {request:'x'.repeat(5001)}, {request:'ok', title:'x'.repeat(81)}]) {
    const result = dispatch(state, { type: 'add_task', ...payload });
    assert.equal(result.error, 'invalid_input');
    assert.equal(result.state, state);
  }
});

test('five_statuses_never_move_topics or accept the result automatically', () => {
  let state = createDemoState();
  const task = state.tasks[0];
  for (const status of ['idle', 'running', 'needs_confirmation', 'completed', 'failed']) {
    state = act(state, 'set_task_status', { taskId: task.id, status }).state;
    const updated = state.tasks.find(item => item.id === task.id);
    assert.equal(updated.topicId, task.topicId);
    assert.equal(updated.status, status);
    assert.equal(updated.accepted, undefined);
    if (status === 'completed') assert.ok(updated.result);
  }
});

test('invalid task or status keeps state and chat target', () => {
  const state = createDemoState();
  for (const action of [
    {type:'select_task',taskId:'missing'},
    {type:'set_task_status',taskId:'missing',status:'running'},
    {type:'set_task_status',taskId:state.tasks[0].id,status:'made-up'},
    {type:'set_draft',conversationId:'missing',text:'no'},
    {type:'send_message',conversationId:'missing',text:'no',draftRevision:0},
    {type:'receive_reply',conversationId:'missing',messageId:'r1',text:'no'}
  ]) {
    const result = dispatch(state, action);
    assert.ok(result.error);
    assert.equal(result.state, state);
  }
});

test('independent_conversation_drafts survive A to B to project and repeated selection', () => {
  let state = createDemoState();
  const [a,b] = state.tasks;
  for (const [conversationId,text] of [['project:kaiwu','项目'],[`task:kaiwu:${a.id}`,'任务 A'],[`task:kaiwu:${b.id}`,'任务 B']]) {
    state = act(state, 'set_draft', {conversationId,text}).state;
  }
  for (const taskId of [a.id,a.id,b.id,null]) state = act(state,'select_task',{taskId}).state;
  assert.equal(state.conversations['project:kaiwu'].draft,'项目');
  assert.equal(state.conversations[`task:kaiwu:${a.id}`].draft,'任务 A');
  assert.equal(state.conversations[`task:kaiwu:${b.id}`].draft,'任务 B');
});

test('late_accept_keeps_new_draft and appends captured text only to original scope', () => {
  let state = createDemoState();
  const conversationId = `task:kaiwu:${state.tasks[0].id}`;
  state = act(state,'set_draft',{conversationId,text:'原始消息'}).state;
  const revision = state.conversations[conversationId].draftRevision;
  state = act(state,'set_draft',{conversationId,text:'新草稿'}).state;
  const sent = act(state,'send_message',{conversationId,text:'原始消息',draftRevision:revision});
  assert.equal(sent.state.conversations[conversationId].draft,'新草稿');
  assert.equal(sent.state.conversations[conversationId].messages.at(-1).text,'原始消息');
  assert.equal(sent.state.conversations[conversationId].messages.at(-1).id,sent.createdId);
  assert.deepEqual(sent.state.conversations['project:kaiwu'],state.conversations['project:kaiwu']);
});

test('accepted current draft clears only that version, ordinary send does not start task', () => {
  let state = createDemoState();
  const task = state.tasks.find(item => item.status === 'idle');
  const conversationId = `task:kaiwu:${task.id}`;
  state = act(state,'set_draft',{conversationId,text:'讨论'}).state;
  state = act(state,'send_message',{conversationId,text:'讨论',draftRevision:state.conversations[conversationId].draftRevision}).state;
  assert.equal(state.conversations[conversationId].draft,'');
  assert.equal(state.tasks.find(item=>item.id===task.id).status,'idle');
});

test('late_reply_stays_in_original_scope after selection and topic deletion', () => {
  let state = createDemoState();
  const [a,b] = state.tasks;
  state = act(state,'select_task',{taskId:b.id}).state;
  state = act(state,'set_draft',{conversationId:currentConversationId(state),text:'B 新草稿'}).state;
  state = act(state,'delete_topic',{topicId:a.topicId}).state;
  const beforeB = structuredClone(state.conversations[currentConversationId(state)]);
  state = act(state,'receive_reply',{conversationId:`task:kaiwu:${a.id}`,messageId:'reply-a',text:'A 的回复'}).state;
  assert.equal(state.selectedTaskId,b.id);
  assert.deepEqual(state.conversations[currentConversationId(state)],beforeB);
  assert.equal(state.conversations[`task:kaiwu:${a.id}`].messages.at(-1).text,'A 的回复');
});

test('duplicate_reply_is_ignored', () => {
  let state = createDemoState();
  const action = {conversationId:'project:kaiwu',messageId:'reply-1',text:'一次回复'};
  state = act(state,'receive_reply',action).state;
  const next = act(state,'receive_reply',action).state;
  assert.deepEqual(next.conversations,state.conversations);
});
