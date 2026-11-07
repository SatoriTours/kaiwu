import {test,expect,type Page,type BrowserContext} from '@playwright/test';
import {randomUUID} from 'node:crypto';
import {readFileSync,mkdirSync} from 'node:fs';
import {resolve} from 'node:path';
let authCookies:Awaited<ReturnType<BrowserContext['cookies']>>|undefined;
async function setup(page:Page){
 await page.addInitScript(()=>{(window as any).__fixtureSocketReady=false;const Original=window.WebSocket;window.WebSocket=class extends Original{constructor(url:string|URL,protocols?:string|string[]){super(url,protocols);this.addEventListener('message',()=>{(window as any).__fixtureSocketReady=true})}}});
 if(authCookies)await page.context().addCookies(authCookies);
 await page.goto('/');
 const headers={Origin:new URL(page.url()).origin,'X-Kaiwu-Client':'web'};
 if(!authCookies){expect((await page.request.post('/api/login',{headers,data:{token:'kaiwu-browser-test-token-00000000'}})).ok()).toBeTruthy();authCookies=await page.context().cookies();}
 const state=(await(await page.request.get('/api/snapshot')).json()).state;
 const path=readFileSync(resolve('../../.superpowers/sdd/2026-10-11-web-service/browser-project.txt'),'utf8');
 const response=await page.request.post('/api/commands',{headers,data:{type:'add_project',name:'创建任务验收',path:path+'-create-'+randomUUID(),revision:state.revision,structureRevision:state.structureRevision,operationId:randomUUID()}});
 expect(response.ok(),await response.text()).toBeTruthy();
 const id=(await response.json()).createdId;
 await page.goto('/projects/'+id);
 await page.waitForFunction(()=>(window as any).__fixtureSocketReady===true);
 await page.locator('[data-action=new-task]:visible').first().click();
 return id;
}
async function tasks(page:Page,id:string){
 const s=(await(await page.request.get('/api/snapshot')).json()).state;
 return {s,tasks:Object.values(s.tasks).filter((t:any)=>t.projectId===id) as any[]};
}
test('single request, equal actions and aligned dropdown in desktop/mobile themes',async({page})=>{
 await setup(page);
 const d=page.getByRole('dialog',{name:'创建任务卡片',exact:true});
 await expect(d.locator('input[name=title]')).toHaveCount(0);
 await expect(d.locator('textarea')).toHaveCount(1);
 await expect(d).not.toContainText('创建卡片不启动');
 for(const width of [1440,390,360]){
  await page.setViewportSize({width,height:900});
  for(const theme of ['light','dark']){
   await page.evaluate(t=>document.documentElement.dataset.theme=t,theme);
   const a=(await d.getByRole('button',{name:'创建',exact:true}).boundingBox())!;
   const b=(await d.getByRole('button',{name:'创建并开始',exact:true}).boundingBox())!;
   expect(a.width).toBe(b.width);expect(a.height).toBe(b.height);
   const select=d.getByRole('combobox',{name:'功能专题'});
   const s=(await select.boundingBox())!, r=(await d.locator('textarea').boundingBox())!;
   expect(s.x).toBe(r.x);expect(s.width).toBe(r.width);
   const text=d.locator('.custom-select-label');
   expect(await text.evaluate(el=>getComputedStyle(el).marginBottom)).toBe('0px');
   await select.click();await expect(d.getByRole('option',{name:'其他',exact:true})).toBeVisible();
   await page.keyboard.press('Escape');await expect(d).toBeVisible();
   expect(await d.evaluate(el=>el.scrollWidth<=el.clientWidth)).toBeTruthy();
   if(process.env.KAIWU_TASK_CREATE_ARTIFACTS){
    mkdirSync(process.env.KAIWU_TASK_CREATE_ARTIFACTS,{recursive:true});
    await page.screenshot({path:resolve(process.env.KAIWU_TASK_CREATE_ARTIFACTS,'create-'+width+'-'+theme+'.png')});
   }
  }
 }
});
test('create only preserves full request and does not launch a session',async({page})=>{
 const id=await setup(page),request='请帮我添加数据库的能力，我要能输入我的每段话';
 const d=page.getByRole('dialog',{name:'创建任务卡片',exact:true});
 await d.locator('textarea').fill(request);
 await d.getByRole('button',{name:'创建',exact:true}).click();
 await expect(d).not.toBeVisible();
 const result=await tasks(page,id);expect(result.tasks).toHaveLength(1);
 const t=result.tasks[0];expect(t.title).toBe('添加数据库的能力');expect(t.request).toBe(request);expect(t.status).toBe('idle');
 expect(t.executionId||'').toBe('');expect(result.s.conversations[t.conversationId].messages).toHaveLength(0);
});
test('create and start launches exactly the created task',async({page})=>{
 const id=await setup(page);
 const d=page.getByRole('dialog',{name:'创建任务卡片',exact:true});
 await d.locator('textarea').fill('创建隔离开发示例');
 await d.getByRole('button',{name:'创建并开始',exact:true}).click();
 await expect(d).not.toBeVisible();
 await expect.poll(async()=>{const r=await tasks(page,id);return !!r.tasks[0]?.executionId}).toBe(true);
 const r=await tasks(page,id);expect(r.tasks).toHaveLength(1);expect(r.s.conversations[r.tasks[0].conversationId].messages.length).toBeGreaterThan(0);
});
test('failed startup retains one created card and reports the failure',async({page})=>{
 const id=await setup(page);
 // Inject a deterministic pre-send start failure, leaving real creation intact.
 await page.evaluate(()=>{(window as any).__KAIWU_API_INTERCEPTOR__=(path:string,body:any)=>{if(path==='commands'&&body?.type==='start_task')throw new Error('测试启动失败：工作区不可用')}});
 const d=page.getByRole('dialog',{name:'创建任务卡片',exact:true});
 await d.locator('textarea').fill('启动失败保留任务');
 await d.getByRole('button',{name:'创建并开始',exact:true}).click();
 await expect(d).not.toBeVisible();
 await expect(page.locator('.global-error')).toBeVisible();
 const r=await tasks(page,id);expect(r.tasks).toHaveLength(1);
});
test('empty/whitespace input is rejected and implicit submission only creates',async({page})=>{
 const id=await setup(page),d=page.getByRole('dialog',{name:'创建任务卡片',exact:true});
 await d.getByRole('button',{name:'创建并开始',exact:true}).click();
 expect((await tasks(page,id)).tasks).toHaveLength(0);
 await d.locator('textarea').fill('   ');
 await d.getByRole('button',{name:'创建并开始',exact:true}).click();
 await expect(d.locator('[role=alert]')).toContainText('需求');
 expect((await tasks(page,id)).tasks).toHaveLength(0);
 await d.locator('textarea').fill('仅创建任务');
 await d.locator('form').evaluate((el:HTMLFormElement)=>el.requestSubmit());
 await expect(d).not.toBeVisible();
 const r=await tasks(page,id);expect(r.tasks).toHaveLength(1);expect(r.tasks[0].executionId||'').toBe('');
});
test('two submissions while creation is pending create and start only once',async({page})=>{
 const id=await setup(page),d=page.getByRole('dialog',{name:'创建任务卡片',exact:true});
 await page.evaluate(()=>{(window as any).__KAIWU_API_INTERCEPTOR__=async(path:string,body:any)=>{
  if(path==='commands'&&body?.type==='add_task')await new Promise(resolve=>setTimeout(resolve,250));
 }});
 await d.locator('textarea').fill('防止重复创建');
 await d.locator('form').evaluate((el:HTMLFormElement)=>{const b=el.querySelector('button[value=start]') as HTMLButtonElement;el.requestSubmit(b);el.requestSubmit(b)});
 await expect(d).not.toBeVisible();
 await expect.poll(async()=>!!(await tasks(page,id)).tasks[0]?.executionId).toBe(true);
 expect((await tasks(page,id)).tasks).toHaveLength(1);
});
test('full request gets a model summary without starting development',async({page})=>{
 const id=await setup(page),d=page.getByRole('dialog',{name:'创建任务卡片',exact:true});
 await d.locator('textarea').fill('界面保持原样。核心需求是把每段聊天保存进数据库，重启后恢复。');
 await d.getByRole('button',{name:'创建',exact:true}).click();
 await expect.poll(async()=> (await tasks(page,id)).tasks[0]?.title).toBe('聊天记录持久化与恢复');
 const r=await tasks(page,id);expect(r.tasks[0].titleState).toBe('ready');expect(r.tasks[0].status).toBe('idle');
 expect(r.s.conversations[r.tasks[0].conversationId].messages).toHaveLength(0);
});
test('startup is visible then failure has a safe retry for the same card',async({page})=>{
 const id=await setup(page),d=page.getByRole('dialog',{name:'创建任务卡片',exact:true});
 await page.evaluate(()=>{(window as any).__KAIWU_API_INTERCEPTOR__=async(path:string,body:any)=>{
  if(path==='commands'&&body?.type==='start_task'){await new Promise(resolve=>setTimeout(resolve,600));throw new Error('测试配置不可用')}
 }});
 await d.locator('textarea').fill('启动反馈验证');await d.getByRole('button',{name:'创建并开始',exact:true}).click();
 await expect(page.locator('.creation-notice')).toContainText('正在启动');
 await expect(page.locator('.creation-notice')).toContainText('卡片已创建，启动失败');
 if(process.env.KAIWU_TASK_CREATE_ARTIFACTS){
  mkdirSync(process.env.KAIWU_TASK_CREATE_ARTIFACTS,{recursive:true});
  for(const width of [1440,390])for(const theme of ['light','dark']){
   await page.setViewportSize({width,height:900});await page.evaluate(t=>document.documentElement.dataset.theme=t,theme);
   await page.screenshot({path:resolve(process.env.KAIWU_TASK_CREATE_ARTIFACTS,'startup-failure-'+width+'-'+theme+'.png')});
  }
 }

 const before=(await tasks(page,id)).tasks[0].id;
 await page.evaluate(()=>delete (window as any).__KAIWU_API_INTERCEPTOR__);
 await page.getByRole('button',{name:'重试启动',exact:true}).click();
 await expect.poll(async()=>!!(await tasks(page,id)).tasks[0]?.executionId).toBe(true);
 const after=(await tasks(page,id)).tasks;expect(after).toHaveLength(1);expect(after[0].id).toBe(before);
});
for(const cancel of [true,false])test('missing model '+(cancel?'cancel keeps card idle':'selection starts the original new card'),async({page})=>{
 let mask=true;
 const clear=(value:any)=>{if(!value||typeof value!=='object')return;for(const [key,v] of Object.entries(value)){if(key==='profile'&&v&&typeof v==='object'){(v as any).provider='';(v as any).model=''}else clear(v)}};
 await page.route('**/api/snapshot',async route=>{const response=await route.fetch();const body=await response.json();if(mask)clear(body);await route.fulfill({response,json:body})});
 await page.routeWebSocket('**/api/socket',client=>{
  const server=client.connectToServer();
  server.onMessage(raw=>{const f=JSON.parse(String(raw));if(mask&&(f.type==='snapshot'||f.type==='patch')){clear(f);for(const op of f.ops??[]){if(op.path?.includes('profile')&&['provider','model'].includes(op.path.at(-1)))op.value=''}}client.send(JSON.stringify(f))});
  client.onMessage(raw=>{const f=JSON.parse(String(raw));if(f.method==='commands'&&f.body?.type==='set_conversation_profile')mask=false;if(!mask&&f.method==='sync')f.body.after=0;server.send(JSON.stringify(f))});
 });
 const id=await setup(page),d=page.getByRole('dialog',{name:'创建任务卡片',exact:true});
 await d.locator('textarea').fill('缺模型流程验证');await d.getByRole('button',{name:'创建并开始',exact:true}).click();
 const m=page.getByRole('dialog',{name:'选择会话模型',exact:true});await expect(m).toBeVisible();
 if(cancel){await m.getByRole('button',{name:'取消',exact:true}).click();expect((await tasks(page,id)).tasks[0].executionId||'').toBe('');await expect(page.locator('.creation-notice')).toContainText('选择模型')}
 else{
  await m.getByRole('combobox',{name:'提供商',exact:true}).click();await m.getByRole('option',{name:'fixture',exact:true}).click();
  await m.getByRole('combobox',{name:'模型',exact:true}).click();await m.getByRole('option',{name:'隔离测试模型 · isolated-test',exact:true}).click();
  await m.getByRole('button',{name:'保存模型',exact:true}).click();
  await expect.poll(async()=>!!(await tasks(page,id)).tasks[0]?.executionId).toBe(true);
 }
 expect((await tasks(page,id)).tasks).toHaveLength(1);
});
test('lost creation receipt is reconciled without allowing duplicate creation',async({page})=>{
 let dropped=false,requestId='';
 await page.routeWebSocket('**/api/socket',client=>{const server=client.connectToServer();
  client.onMessage(raw=>{const f=JSON.parse(String(raw));if(!dropped&&f.method==='commands'&&f.body?.type==='add_task')requestId=f.id;server.send(String(raw))});
  server.onMessage(raw=>{const f=JSON.parse(String(raw));if(!dropped&&f.type==='reply'&&f.id===requestId){dropped=true;client.close({code:1012,reason:'receipt lost fixture'});server.close();return}client.send(String(raw))});
 });
 const id=await setup(page),d=page.getByRole('dialog',{name:'创建任务卡片',exact:true});
 await d.locator('textarea').fill('回执丢失验证');await d.getByRole('button',{name:'创建',exact:true}).click();
 await expect(d).not.toBeVisible({timeout:15000});expect(dropped).toBe(true);
 const r=await tasks(page,id);expect(r.tasks).toHaveLength(1);expect(r.tasks[0].executionId||'').toBe('');
 await expect(page.locator('#chat-title')).toContainText('回执丢失验证');
});
test('new card starts without starting an existing old task',async({page})=>{
 const id=await setup(page),d=page.getByRole('dialog',{name:'创建任务卡片',exact:true});
 await d.locator('textarea').fill('旧任务保持未开始');await d.getByRole('button',{name:'创建',exact:true}).click();await expect(d).not.toBeVisible();
 const old=(await tasks(page,id)).tasks[0];
 await page.getByRole('button',{name:'收起聊天',exact:true}).click();await page.locator('[data-action=new-task]:visible').first().click();
 await d.locator('textarea').fill('只启动新任务');await d.getByRole('button',{name:'创建并开始',exact:true}).click();
 await expect.poll(async()=> (await tasks(page,id)).tasks.filter(t=>t.executionId).length).toBe(1);
 const r=await tasks(page,id);expect(r.tasks).toHaveLength(2);expect(r.s.tasks[old.id].status).toBe('idle');expect(r.s.tasks[old.id].executionId||'').toBe('');
});
test('lost startup receipt never dispatches a second development request',async({page})=>{
 let dropped=false,requestId='',starts=0;
 await page.routeWebSocket('**/api/socket',client=>{const server=client.connectToServer();
  client.onMessage(raw=>{const f=JSON.parse(String(raw));if(f.method==='commands'&&f.body?.type==='start_task'){requestId=f.id;starts++}server.send(String(raw))});
  server.onMessage(raw=>{const f=JSON.parse(String(raw));if(!dropped&&f.type==='reply'&&f.id===requestId){dropped=true;client.close({code:1012,reason:'startup receipt lost fixture'});server.close();return}client.send(String(raw))});
 });
 const id=await setup(page),d=page.getByRole('dialog',{name:'创建任务卡片',exact:true});
 await d.locator('textarea').fill('启动回执丢失验证');await d.getByRole('button',{name:'创建并开始',exact:true}).click();
 await expect.poll(async()=>!!(await tasks(page,id)).tasks[0]?.executionId).toBe(true);
 await expect(page.locator('.creation-notice')).not.toBeVisible({timeout:15000});
 expect(dropped).toBe(true);expect(starts).toBe(1);expect((await tasks(page,id)).tasks).toHaveLength(1);
});
test('authentication expiry during creation never starts or creates a task',async({page})=>{
 const id=await setup(page),d=page.getByRole('dialog',{name:'创建任务卡片',exact:true});
 const headers={Origin:new URL(page.url()).origin,'X-Kaiwu-Client':'web'};
 await page.request.post('/api/logout',{headers});authCookies=undefined;
 await d.locator('textarea').fill('身份失效不得创建');await d.getByRole('button',{name:'创建并开始',exact:true}).click();
 await expect(page.locator('.login-page')).toBeVisible();
 const response=await page.request.post('/api/login',{headers,data:{token:'kaiwu-browser-test-token-00000000'}});expect(response.ok()).toBeTruthy();
 expect((await tasks(page,id)).tasks).toHaveLength(0);
});
