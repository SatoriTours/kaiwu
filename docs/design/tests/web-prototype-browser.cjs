// Local Chromium/CDP checks. No npm dependency, server, personal browser profile or model call.
const {spawn}=require('node:child_process');
const fs=require('node:fs');
const os=require('node:os');
const path=require('node:path');
const {pathToFileURL}=require('node:url');
const {once}=require('node:events');

const screenshotFlag=process.argv.indexOf('--screenshots');
const screenshotDirectory=path.resolve(screenshotFlag>=0?process.argv[screenshotFlag+1]:'./.superpowers/sdd/2026-10-11-web-kanban-prototype/screenshots');
const profile=fs.mkdtempSync(path.join(os.tmpdir(),'kaiwu-kanban-browser-'));
const browser=spawn(process.env.CHROMIUM_BIN||'chromium',['--headless','--no-sandbox','--disable-gpu','--disable-background-networking','--disable-extensions','--no-first-run','--no-default-browser-check',`--user-data-dir=${profile}`,'--remote-debugging-pipe'],{stdio:['ignore','ignore','pipe','pipe','pipe']});
let nextId=0,buffer='',session,stderr='',exitObserved=false,allowSnapshotReload=false,snapshotLoaded;
const inflight=new Map(),exceptions=[],requests=[];
browser.stderr.on('data',chunk=>stderr+=chunk.toString());
function rejectPending(error) { for(const call of inflight.values()){clearTimeout(call.timer);call.reject(error);}inflight.clear(); }
browser.on('error',rejectPending);
browser.on('exit',(code,signal)=>{exitObserved=true;rejectPending(new Error(`Chromium exited (${code||signal}): ${stderr.slice(-1200)}`));});
for(const pipe of [browser.stdio[3],browser.stdio[4]]) pipe.on('error',rejectPending);
browser.stdio[4].on('data',chunk=>{
  buffer+=chunk.toString();let end;
  while((end=buffer.indexOf('\0'))>=0) {
    const raw=buffer.slice(0,end);buffer=buffer.slice(end+1);if(!raw)continue;
    const message=JSON.parse(raw);
    if(message.id){const call=inflight.get(message.id);if(call){inflight.delete(message.id);clearTimeout(call.timer);message.error?call.reject(new Error(JSON.stringify(message.error))):call.resolve(message.result);}}
    else if(message.method==='Runtime.exceptionThrown')exceptions.push(message.params.exceptionDetails);
    else if(message.method==='Network.requestWillBeSent'&&/^(https?|wss?):/.test(message.params.request.url))requests.push(message.params.request.url);
    else if(message.method==='Page.javascriptDialogOpening'){
      if(allowSnapshotReload&&message.params.type==='beforeunload')command('Page.handleJavaScriptDialog',{accept:true},session).catch(rejectPending);
      else rejectPending(new Error('Unexpected browser dialog: '+message.params.type));
    }
    else if(message.method==='Page.loadEventFired'&&snapshotLoaded)snapshotLoaded();
  }
});
function command(method,params={},sessionId) {
  return new Promise((resolve,reject)=>{
    if(exitObserved){reject(new Error('Chromium is no longer running'));return;}
    const id=++nextId;
    const timer=setTimeout(()=>{inflight.delete(id);reject(new Error(`CDP timeout: ${method}`));},8000);
    inflight.set(id,{resolve,reject,timer});
    browser.stdio[3].write(JSON.stringify({id,method,params,...(sessionId?{sessionId}:{})})+'\0');
  });
}
async function run(expression){const result=await command('Runtime.evaluate',{expression,awaitPromise:true,returnByValue:true},session);if(result.exceptionDetails)throw new Error(result.exceptionDetails.exception?.description||result.exceptionDetails.text);return result.result.value;}
let count=0;
async function check(expression,label){if(!await run(expression))throw new Error('FAILED: '+label);console.log('PASS: '+label);count++;}
const click=selector=>run(`document.querySelector(${JSON.stringify(selector)}).click()`);
const fill=(selector,value)=>run(`{const el=document.querySelector(${JSON.stringify(selector)});el.value=${JSON.stringify(value)};el.dispatchEvent(new Event('input',{bubbles:true}));}`);
const change=(selector,value)=>run(`{const el=document.querySelector(${JSON.stringify(selector)});el.value=${JSON.stringify(value)};el.dispatchEvent(new Event('change',{bubbles:true}));}`);
async function waitFor(expression){return run(`new Promise((resolve,reject)=>{const deadline=performance.now()+7000;function check(){if(${expression})resolve(true);else if(performance.now()>deadline)reject(new Error('DOM condition timed out'));else setTimeout(check,20);}check();})`);}
async function size(width,height){await command('Emulation.setDeviceMetricsOverride',{width,height,deviceScaleFactor:1,mobile:width<768},session);}
async function shot(name){const result=await command('Page.captureScreenshot',{format:'png',captureBeyondViewport:false},session);fs.writeFileSync(path.join(screenshotDirectory,name),Buffer.from(result.data,'base64'));}
const timeout=setTimeout(()=>{rejectPending(new Error('Browser check exceeded 90s'));browser.kill('SIGKILL');},90000);

(async()=>{try{
  fs.mkdirSync(screenshotDirectory,{recursive:true});
  const version=await command('Browser.getVersion');console.log('Browser:',version.product);
  const {targetId}=await command('Target.createTarget',{url:'about:blank'});
  ({sessionId:session}=await command('Target.attachToTarget',{targetId,flatten:true}));
  await command('Page.enable',{},session);await command('Runtime.enable',{},session);await command('Network.enable',{},session);
  await size(1440,1100);
  const url=pathToFileURL(path.resolve(__dirname,'../web-prototype.html')).href;
  await command('Page.navigate',{url},session);
  await waitFor("document.readyState==='complete'");
  await check("Boolean(document.getElementById('board')) && document.getElementById('chat-title')?.textContent.includes('项目对话')",'Kanban and project conversation replace old three-column layout');
  await check("document.querySelectorAll('.column').length===3 && document.querySelectorAll('.task-card').length>=5",'functional topics contain task cards');
  await check("new Set([...document.querySelectorAll('.task-status')].map(el=>el.dataset.status)).size===5",'five readable task badges appear on cards');
  await check("document.querySelector('[data-taskid=t1]').getBoundingClientRect().right <= document.getElementById('chat').getBoundingClientRect().left",'board sits left and chat stays right');
  await fill('#draft','项目草稿');
  await click('[data-taskid=t1]');await fill('#draft','A 草稿');
  await click('[data-taskid=t2]');await fill('#draft','B 草稿');
  await click('[data-taskid=t1]');
  await check("document.getElementById('draft').value==='A 草稿' && document.getElementById('chat-session').textContent.includes('t1')",'task selection restores its independent draft and simulated CLI binding');
  await click('[data-taskid=t1]');
  await check("document.getElementById('draft').value==='A 草稿'",'repeated selection preserves draft');
  await click('[data-action=project]');
  await check("document.getElementById('draft').value==='项目草稿'",'return to project restores project draft');
  await click('[data-taskid=t1]');await fill('#draft','给 A 的消息');await click('#send');
  await click('[data-taskid=t2]');await fill('#draft','B 新草稿');
  await waitFor("document.querySelector('[data-taskid=t1] .unread')");
  await check("document.getElementById('draft').value==='B 新草稿' && !document.getElementById('messages').textContent.includes('给 A 的消息')",'late acceptance and reply for A cannot alter B history or draft');
  await click('[data-taskid=t1]');
  await check("document.getElementById('messages').textContent.includes('给 A 的消息')",'A retains its own accepted message and reply');
  await fill('#draft','已发送原文');await click('#send');await fill('#draft','同一会话的新草稿');
  await waitFor("!document.getElementById('send').disabled");
  await check("document.getElementById('draft').value==='同一会话的新草稿'",'old acceptance cannot clear a newer draft in same session');
  await click('[data-action=delete-topic][data-topicid=topic1]');await click('[data-action=close]');
  await check("Boolean(document.querySelector('.column[data-topicid=topic1]'))",'cancel topic deletion leaves board unchanged');
  await fill('#draft','删除专题前发送');await click('#send');
  await click('[data-action=delete-topic][data-topicid=topic1]');await click('#confirm-delete');
  await check("!document.querySelector('.column[data-topicid=topic1]') && document.querySelector('.column[data-topicid=other] [data-taskid=t1]') && document.getElementById('chat-session').textContent.includes('t1')",'deletion moves tasks into Other and preserves selected conversation');
  await waitFor("document.getElementById('messages').textContent.includes('已收到：删除专题前发送')");
  await check("document.getElementById('chat-session').textContent.includes('t1')",'late reply survives topic deletion in original session');
  await check("!document.querySelector('[data-action=delete-topic][data-topicid=other]')",'Other is protected from deletion');
  await click('[data-action=new-topic]');await fill('#topic-name','连接与恢复');await run("document.getElementById('topic-form').requestSubmit()");
  await check("!document.getElementById('modal').open",'new topic can reuse a deleted name');
  await click('[data-action=new-topic]');await fill('#topic-name','连接与恢复');await run("document.getElementById('topic-form').requestSubmit()");
  await check("document.getElementById('modal').open && document.getElementById('form-error').textContent.length>0",'duplicate topic error retains form');
  await fill('#topic-name','<img src=x onerror=alert(1)>');await run("document.getElementById('topic-form').requestSubmit()");
  await check("!document.querySelector('#board img') && document.getElementById('board').textContent.includes('<img src=x')",'untrusted topic is rendered as text');
  await click('[data-action=new-task]');await fill('#task-title','测试新任务');await fill('#task-request','保留所有草稿');await run("document.getElementById('task-form').requestSubmit()");
  await check("document.querySelector('.task-card.selected .task-status').dataset.status==='idle' && document.querySelector('.column[data-topicid=other] .task-card.selected')",'new task defaults to Other and remains unstarted');
  const taskId=await run("document.querySelector('.task-card.selected').dataset.taskid");
  for(const status of ['running','needs_confirmation','completed','failed','idle']) {
    await change('#demo-status',status);
    await check(`document.querySelector('[data-taskid=${taskId}] .task-status').dataset.status===${JSON.stringify(status)} && document.querySelector('.column[data-topicid=other] [data-taskid=${taskId}]')`,'badge '+status+' does not move task between topics');
  }
  await fill('#draft','输入法测试');
  const before=await run("document.querySelectorAll('.message.user').length");
  await run("document.getElementById('draft').dispatchEvent(new KeyboardEvent('keydown',{key:'Enter',ctrlKey:true,isComposing:true,bubbles:true}))");
  await check(`document.querySelectorAll('.message.user').length===${before} && !document.getElementById('send').disabled`,'IME composition cannot send message');
  await run("document.getElementById('draft').dispatchEvent(new KeyboardEvent('keydown',{key:'Enter',ctrlKey:true,bubbles:true}));document.getElementById('draft').dispatchEvent(new KeyboardEvent('keydown',{key:'Enter',ctrlKey:true,bubbles:true}));");
  await waitFor(`document.querySelectorAll('.message.user').length===${before+1}`);
  await check(`document.querySelectorAll('.message.user').length===${before+1} && document.querySelector('.task-card.selected .task-status').dataset.status==='idle'`,'shortcut submits once and discussion does not start task');
  await fill('#draft','离线保留');await click('#connection');
  await check("document.getElementById('send').disabled && document.querySelector('[data-action=new-task]').disabled && document.getElementById('draft').value==='离线保留'",'offline disables writes but preserves current draft');
  await click('#connection');
  for(const [width,height] of [[1440,1100],[1024,900],[768,1024],[360,900]]) {
    await size(width,height);
    await check("document.documentElement.scrollWidth<=document.documentElement.clientWidth",`${width}px viewport has no page horizontal overflow`);
  }
  await click('#show-board');await change('#mobile-topic','other');
  await click(`[data-taskid=${taskId}]`);
  await check("getComputedStyle(document.getElementById('chat')).display!=='none' && document.getElementById('draft').value==='离线保留'",'mobile card opens task conversation with existing draft');
  await click('[data-action=project]');
  await check("document.getElementById('draft').value==='项目草稿'",'mobile explicit return restores project scope');
  await click('#show-board');await click('[data-action=new-topic]');
  await check("document.getElementById('modal').getBoundingClientRect().width<=360 && document.getElementById('modal').contains(document.activeElement)",'mobile dialog fits screen and receives focus');
  await command('Input.dispatchKeyEvent',{type:'keyDown',key:'Escape',code:'Escape',windowsVirtualKeyCode:27},session);
  await command('Input.dispatchKeyEvent',{type:'keyUp',key:'Escape',code:'Escape',windowsVirtualKeyCode:27},session);
  await waitFor("!document.getElementById('modal').open && document.activeElement.dataset.action==='new-topic'");
  await check("!document.getElementById('modal').open && document.activeElement.dataset.action==='new-topic'",'Escape cancels dialog and restores trigger focus');
  await click('[data-action=new-topic]');await fill('#topic-name','可删除的空专题');await run("document.getElementById('topic-form').requestSubmit()");
  const emptyTopic=await run("document.querySelector('#mobile-topic').value");
  await click(`[data-action=delete-topic][data-topicid=${emptyTopic}]`);await click('#confirm-delete');
  await check(`!document.querySelector('.column[data-topicid=${emptyTopic}]')`,'empty topic can be deleted');
  for(let index=0;index<6;index++){await click('[data-action=new-topic]');await fill('#topic-name','更多功能 '+index);await run("document.getElementById('topic-form').requestSubmit()");}
  await click('[data-action=new-task]');await fill('#task-title','长标题🌱'.repeat(8));await fill('#task-request','<img src=x onerror=alert(1)> 仅作为文字');await run("document.getElementById('task-form').requestSubmit()");
  await check("!document.querySelector('#messages img') && document.getElementById('messages').textContent.includes('<img src=x')",'untrusted task requirement is plain text in chat');
  await change('#demo-status','completed');await click('#show-board');await change('#mobile-topic','other');
  await check("(()=>{const card=document.querySelector('.task-card.selected'),badge=card.querySelector('.task-status');return badge.getBoundingClientRect().right<=card.getBoundingClientRect().right&&document.documentElement.scrollWidth<=360;})()",'long Unicode title and completed badge stay inside mobile card');
  await size(1440,1100);
  await check("document.getElementById('board').scrollWidth>document.getElementById('board').clientWidth && document.documentElement.scrollWidth<=1440",'many topics scroll inside board without stretching page');
  await run("document.querySelector('[data-taskid=t2]').focus()");
  await command('Input.dispatchKeyEvent',{type:'keyDown',key:'Enter',code:'Enter',windowsVirtualKeyCode:13,text:'\r',unmodifiedText:'\r'},session);
  await command('Input.dispatchKeyEvent',{type:'keyUp',key:'Enter',code:'Enter',windowsVirtualKeyCode:13},session);
  await check("document.getElementById('chat-session').textContent.includes('t2')",'keyboard Enter selects a task card');
  await fill('#draft','等待接受期间断线');await click('#send');await click('#connection');
  await waitFor("document.getElementById('messages').textContent.includes('已收到：等待接受期间断线')");
  await check("document.getElementById('chat-session').textContent.includes('t2') && document.getElementById('send').disabled",'captured simulated reply stays in its scope across disconnect');
  await click('#connection');
  await fill('#draft','长消息验证独立阅读位置：'+('正文内容。'.repeat(350)));await click('#send');
  await waitFor("document.getElementById('messages').textContent.includes('已收到：长消息验证独立阅读位置')");
  await run("document.getElementById('messages').scrollTop=100");
  await click('[data-action=project]');await click('[data-taskid=t2]');
  await check("Math.abs(document.getElementById('messages').scrollTop-100)<2",'task conversation restores independent reading position');
  await size(360,900);
  await click('#show-chat');
  await run("document.getElementById('messages').scrollTop=300");
  await click('#show-board');await change('#mobile-topic','other');await click('[data-taskid=t1]');
  await fill('#draft','另一个任务的长回复。'.repeat(180));await click('#send');
  await waitFor("document.getElementById('messages').textContent.includes('已收到：另一个任务的长回复')");
  await run("document.getElementById('messages').scrollTop=500");
  await click('#show-board');await change('#mobile-topic','other');await click('[data-taskid=t2]');
  await check("Math.abs(document.getElementById('messages').scrollTop-300)<2",'mobile board-to-task switch restores A reading position');
  await click('#show-board');await click('[data-taskid=t1]');
  await check("Math.abs(document.getElementById('messages').scrollTop-500)<2",'mobile board-to-task switch restores B reading position');
  await run("document.getElementById('messages').scrollTop=700");await click('#show-chat');
  await check("Math.abs(document.getElementById('messages').scrollTop-700)<2",'repeated active chat tab preserves current reading position');

  // Native key activation enables the browser's unsaved-draft warning. Accept it only
  // for this deliberate reset, after all interaction checks have finished.
  allowSnapshotReload=true;
  let reloadTimeout;
  const reloaded=new Promise((resolve,reject)=>{
    reloadTimeout=setTimeout(()=>reject(new Error('Screenshot reload timed out')),8000);
    snapshotLoaded=()=>{clearTimeout(reloadTimeout);snapshotLoaded=undefined;resolve();};
  });
  try{await command('Page.reload',{},session);await reloaded;}finally{clearTimeout(reloadTimeout);}
  await waitFor("document.getElementById('chat-title')?.textContent.includes('项目对话')");
  allowSnapshotReload=false;
  await size(1440,1100);await shot('web-desktop.png');
  await size(360,900);await shot('web-mobile.png');
  if(exceptions.length)throw new Error('Runtime exceptions: '+JSON.stringify(exceptions));
  if(requests.length)throw new Error('Unexpected network: '+requests.join(', '));
  console.log(`PASS: ${count} DOM/interaction checks; no runtime exceptions or HTTP/WebSocket requests.`);
  console.log('Screenshots:',screenshotDirectory);
}catch(error){console.error(error.stack);if(session&&!exitObserved)console.error('DOM diagnostic:',await run("({dialogOpen:document.getElementById('modal')?.open,focusTag:document.activeElement?.tagName,focusAction:document.activeElement?.dataset.action,focusId:document.activeElement?.id,focusTask:document.activeElement?.dataset.taskid,chatScope:document.getElementById('chat-session')?.textContent,keys:window.__keyDiagnostics})").catch(()=>null));process.exitCode=1;
}finally{
  clearTimeout(timeout);
  if(!exitObserved)await command('Browser.close').catch(()=>{});
  if(!exitObserved)await Promise.race([once(browser,'exit').catch(()=>{}),new Promise(resolve=>setTimeout(resolve,1500))]);
  if(!exitObserved){browser.kill('SIGKILL');await once(browser,'exit').catch(()=>{});}
  rejectPending(new Error('Browser closed'));
  fs.rmSync(profile,{recursive:true,force:true});
}})();
