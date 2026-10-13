(function (root, factory) {
  const api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else root.KaiwuPrototypeState = api;
})(typeof globalThis !== 'undefined' ? globalThis : this, function () {
  'use strict';
  const statuses = ['idle', 'running', 'needs_confirmation', 'completed', 'failed'];
  const taskConversationId = id => `task:kaiwu:${id}`;
  const conversation = (text, id) => ({
    messages: [{ id: `welcome:${id}`, role: 'assistant', text }], draft: '', draftRevision: 0
  });

  function createDemoState() {
    const tasks = [
      { id:'t1', topicId:'topic1', title:'浏览器断线后，任务继续执行', request:'将连接与任务生命周期分开，重连后恢复进度。', status:'running', owner:'pi · 执行 Agent', updated:'刚刚', result:null },
      { id:'t2', topicId:'topic1', title:'重连时补齐遗漏的任务事件', request:'检查游标补发，重复事件不重复展示。', status:'idle', owner:'pi · 执行 Agent', updated:'5 分钟前', result:null },
      { id:'t3', topicId:'topic1', title:'验证恢复过程中的失败提示', request:'验证失败时保留已有成果与明确的原因。', status:'failed', owner:'pi · 执行 Agent', updated:'8 分钟前', result:'模拟失败：事件游标已失效，需要重新读取状态；已有工作区改动保留。' },
      { id:'t4', topicId:'topic2', title:'确认新建任务的默认配置', request:'选择整套配置，启动时保存快照。', status:'needs_confirmation', owner:'pi · 主 Agent', updated:'2 分钟前', result:null },
      { id:'t5', topicId:'topic2', title:'完善项目选择的空状态', request:'区分首次使用、无权限与目录不存在。', status:'completed', owner:'pi · 执行 Agent', updated:'12 分钟前', result:'模拟结果：已整理三类空状态与对应操作。验证为固定界面样例，未运行真实模型或业务测试；尚未验收、提交、合并或推送。' },
      { id:'t6', topicId:'topic2', title:'手机上查看任务并补充需求', request:'保持看板与聊天的上下文一致。', status:'idle', owner:'尚未分派', updated:'18 分钟前', result:null },
      { id:'t7', topicId:'other', title:'整理下一阶段开发想法', request:'先讨论范围，再决定创建哪些任务。', status:'idle', owner:'尚未分派', updated:'20 分钟前', result:null }
    ];
    const conversations = { 'project:kaiwu': conversation('我们在这里讨论整个项目。你可以提出新的想法、创建任务卡片，或安排接下来的工作。选中左侧卡片后，这里会连接到该任务自己的 pi 会话。', 'project') };
    for (const task of tasks) conversations[taskConversationId(task.id)] = conversation(`这是「${task.title}」的独立任务会话。\n${task.request}\n\n当前为模拟内容，未启动真实 pi。`, task.id);
    return { projectId:'kaiwu', topics:[{id:'topic1',name:'连接与恢复'},{id:'topic2',name:'工作台体验'},{id:'other',name:'其他'}], tasks, selectedTaskId:null, conversations, nextTopic:3, nextTask:8, nextMessage:1 };
  }

  function currentConversationId(state) {
    return state.selectedTaskId === null ? `project:${state.projectId}` : `task:${state.projectId}:${state.selectedTaskId}`;
  }

  function dispatch(state, action) {
    const fail = error => ({ state, error });
    const success = (next, extra = {}) => ({ state:next, error:null, ...extra });
    if (!action || typeof action !== 'object') return fail('invalid_input');
    const task = state.tasks.find(item => item.id === action.taskId);
    const existing = typeof action.conversationId === 'string' && Object.hasOwn(state.conversations, action.conversationId)
      ? state.conversations[action.conversationId] : null;
    switch (action.type) {
      case 'add_topic': {
        if (typeof action.name !== 'string') return fail('invalid_name');
        const name = action.name.trim();
        if (!name || Array.from(name).length > 40) return fail('invalid_name');
        if (state.topics.some(item => item.name === name)) return fail('duplicate_name');
        const id = `topic${state.nextTopic}`;
        const topics = state.topics.filter(item => item.id !== 'other');
        topics.push({id,name}, state.topics.find(item => item.id === 'other'));
        return success({...state,topics,nextTopic:state.nextTopic+1},{createdId:id});
      }
      case 'delete_topic': {
        if (action.topicId === 'other') return fail('protected_topic');
        if (!state.topics.some(item => item.id === action.topicId)) return fail('not_found');
        return success({...state, topics:state.topics.filter(item=>item.id!==action.topicId), tasks:state.tasks.map(item=>item.topicId===action.topicId?{...item,topicId:'other'}:item)});
      }
      case 'add_task': {
        if (typeof action.request !== 'string' || !action.request.trim() || action.request.length > 5000 ||
            (action.title !== undefined && (typeof action.title !== 'string' || action.title.length > 80))) return fail('invalid_input');
        const request = action.request.trim();
        const id = `t${state.nextTask}`;
        const requestedTopicExists = state.topics.some(item=>item.id===action.topicId);
        const topicId = requestedTopicExists ? action.topicId : 'other';
        const title = (action.title || '').trim() || Array.from(request).slice(0,20).join('');
        const created = {id,topicId,title,request,status:'idle',owner:'尚未分派',updated:'刚刚',result:null};
        return success({...state, tasks:[...state.tasks,created], selectedTaskId:id, nextTask:state.nextTask+1,
          conversations:{...state.conversations,[taskConversationId(id)]:conversation(`这是「${title}」的独立任务会话。\n${request}\n\n任务尚未开始；当前仅演示沟通，不启动真实 pi。`,id)}},
          {createdId:id,...(!requestedTopicExists && action.topicId?{notice:'原专题已不存在，任务已放入其他。'}:{})});
      }
      case 'select_task':
        if (action.taskId !== null && !task) return fail('not_found');
        return success({...state,selectedTaskId:action.taskId});
      case 'set_task_status': {
        if (!task) return fail('not_found');
        if (!statuses.includes(action.status)) return fail('invalid_status');
        const result = action.status === 'completed'
          ? '模拟结果：本任务执行完成，可查看改动与验证摘要。未执行真实模型、构建或 Git 操作；执行完成不等于用户验收。'
          : action.status === 'failed' ? '模拟失败：执行过程遇到错误，已有成果保留。请在此任务会话中讨论下一步。' : task.result;
        return success({...state,tasks:state.tasks.map(item=>item.id===task.id?{...item,status:action.status,result,updated:'刚刚'}:item)});
      }
      case 'set_draft':
        if (!existing) return fail('not_found');
        if (typeof action.text !== 'string') return fail('invalid_input');
        return success({...state,conversations:{...state.conversations,[action.conversationId]:{...existing,draft:action.text,draftRevision:existing.draftRevision+1}}});
      case 'send_message': {
        if (!existing) return fail('not_found');
        if (typeof action.text !== 'string' || !action.text.trim() || action.text.length>5000 || !Number.isSafeInteger(action.draftRevision)) return fail('invalid_input');
        const id = `m${state.nextMessage}`;
        const matched = existing.draftRevision === action.draftRevision;
        const updated = {...existing, messages:[...existing.messages,{id,role:'user',text:action.text.trim()}],
          ...(matched?{draft:'',draftRevision:existing.draftRevision+1}:{})};
        return success({...state,nextMessage:state.nextMessage+1,conversations:{...state.conversations,[action.conversationId]:updated}},{createdId:id});
      }
      case 'receive_reply': {
        if (!existing) return fail('not_found');
        if (typeof action.messageId!=='string'||!action.messageId || typeof action.text!=='string') return fail('invalid_input');
        if (existing.messages.some(item=>item.id===action.messageId)) return success(state);
        return success({...state,conversations:{...state.conversations,[action.conversationId]:{...existing,messages:[...existing.messages,{id:action.messageId,role:'assistant',text:action.text}]}}});
      }
      default: return fail('invalid_input');
    }
  }
  return { createDemoState, currentConversationId, dispatch };
});
