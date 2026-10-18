import { test, expect, type Page, type APIRequestContext } from '@playwright/test';
import { randomUUID } from 'node:crypto';
import { resolve } from 'node:path';
import { mkdirSync } from 'node:fs';

const TOKEN='kaiwu-delivery-fixture-token-00000000';
const authHeaders={'X-Fixture-Token':TOKEN};
async function evidence(request:APIRequestContext){const r=await request.get('/__fixture/evidence',{headers:authHeaders});expect(r.ok()).toBeTruthy();return r.json()}
async function snapshot(request:APIRequestContext){const r=await request.get('/api/snapshot');expect(r.ok()).toBeTruthy();return (await r.json()).state}
async function post(request:APIRequestContext,path:string,body:any){const r=await request.post('/api/'+path,{data:body,headers:{'X-Kaiwu-Client':'web','Origin':'http://127.0.0.1:17332'}});expect(r.ok(),await r.text()).toBeTruthy();return r.json()}
async function login(page:Page){await page.goto('/');await page.getByLabel('本机访问令牌').fill(TOKEN);await page.getByRole('button',{name:'进入工作台',exact:true}).click();await expect(page.getByRole('button',{name:'当前项目',exact:true})).toBeVisible()}
async function configure(page:Page,manual=false){
 const info=await evidence(page.request),res=await page.request.get('/api/delivery/policy?projectId='+info.projectId);const {policy}=await res.json();
 Object.assign(policy,{enabled:true,requireAcceptance:manual,repository:{id:42,owner:'owner',name:'repo',url:'https://github.com/owner/repo.git',mainBranch:'main'},checks:[{id:'test',argv:['true'],dir:'.',timeoutSeconds:300}],taskWorkflows:[{id:1,jobs:['test']}],mainWorkflows:[{id:2,jobs:['test']}],maxRepairs:3,gitCredentialRef:'server_github_https',apiCredentialRef:'server_github_api',authorizationDigest:'fixture-explicit-scope',actor:'local-login',authorizedAt:new Date().toISOString(),revokedAt:'0001-01-01T00:00:00Z'});
 await post(page.request,'delivery/policy',{operationId:randomUUID(),expectedVersion:policy.version,policy});return info.projectId;
}
async function addTask(page:Page,request:string){
 await page.locator('[data-action=new-task]').click();await page.locator('input[name=title]').fill(request);await page.locator('textarea[name=request]').fill(request);await page.getByRole('button',{name:'保存',exact:true}).click();
 await expect(page.locator('#chat-title')).toContainText(request);
 const st=await snapshot(page.request);return Object.values(st.tasks).find((t:any)=>t.title===request) as any;
}
async function startTask(page:Page,task:any){
 await page.locator('.task-card[data-taskid="'+task.id+'"]').getByRole('button',{name:'开始开发',exact:true}).click();
}
async function phase(page:Page,task:any,wanted:string){
 await expect.poll(async()=> (await snapshot(page.request)).deliveries?.[task.id]?.phase,{timeout:30000}).toBe(wanted);
 return (await snapshot(page.request)).deliveries[task.id];
}
async function cleanup(page:Page){
 const st=await snapshot(page.request);
 for(const d of Object.values(st.deliveries??{}) as any[]){if(['succeeded','cancelled'].includes(d.phase))continue;await post(page.request,'delivery/control',{operationId:randomUUID(),deliveryId:d.id,action:'cancel',expectedVersion:d.version})}
 await page.request.post('/__fixture/ci',{headers:authHeaders,data:{holdMain:false}});
}

test.describe('真实 Go 服务的自动交付界面',()=>{
 test.beforeEach(async({page})=>{await login(page);await cleanup(page)});

 test('一次配置有明确授权、只读预检且保存真实后端策略',async({page})=>{
  await page.getByRole('button',{name:'当前项目',exact:true}).click();await page.getByRole('menuitem',{name:/交付设置/}).click();
  const dialog=page.getByRole('dialog',{name:'开发与交付设置'});await expect(dialog).toBeVisible();
  await dialog.getByLabel(/规范仓库地址/).fill('https://github.com/owner/repo.git');
  await dialog.getByLabel('Owner',{exact:true}).fill('owner');await dialog.getByLabel('Repository Name',{exact:true}).fill('repo');await dialog.getByLabel(/Repository ID/).fill('42');
  await dialog.locator('textarea').nth(0).fill(JSON.stringify([{id:'test',argv:['true'],dir:'.',timeoutSeconds:300}]));
  await dialog.locator('textarea').nth(1).fill(JSON.stringify([{id:1,jobs:['test']}]));await dialog.locator('textarea').nth(2).fill(JSON.stringify([{id:2,jobs:['test']}]));
  await dialog.getByLabel(/启用本项目/).check();await expect(dialog).toContainText('任务分支');await expect(dialog).toContainText('部署或发包');
  await dialog.getByLabel(/我确认授权/).check();await dialog.getByRole('button',{name:'保存设置',exact:true}).click();
  await expect(dialog).not.toBeVisible();
  const info=await evidence(page.request),res=await page.request.get('/api/delivery/policy?projectId='+info.projectId);const {policy}=await res.json();expect(policy.enabled).toBe(true);expect(policy.repository.id).toBe(42);expect(policy.authorizedAt).not.toMatch(/^0001/);
  const count=info.pushes.length;
  await page.getByRole('button',{name:'当前项目',exact:true}).click();await page.getByRole('menuitem',{name:/交付设置/}).click();await page.getByRole('button',{name:'执行只读预检'}).click();await expect(page.getByRole('dialog')).toContainText('预检通过');expect((await evidence(page.request)).pushes.length).toBe(count);
 });

 test('人工验收后真实推主分支；刷新和换卡不重发，保留原会话',async({page})=>{
  await configure(page,true);const task=await addTask(page,'人工验收-'+randomUUID().slice(0,6));await startTask(page,task);
  const d=await phase(page,task,'awaiting_acceptance');const before=await evidence(page.request);
  expect(before.refs).toContain(d.candidateSHA+' refs/heads/kaiwu/task-'+task.id);expect(before.refs).not.toContain(d.candidateSHA+' refs/heads/main');
  const panel=page.locator('.delivery-status-panel');await expect(panel).toContainText('待验收');
  await expect(page.locator('#chat').getByRole('button',{name:'合并到主分支',exact:true})).toHaveCount(0);
  await expect(page.locator('#chat').getByRole('button',{name:'确认完成',exact:true})).toHaveCount(0);
  await page.reload();await expect(panel).toContainText('待验收');expect((await evidence(page.request)).pushes.length).toBe(before.pushes.length);
  await panel.getByRole('button',{name:/验收.*交付|效果验收通过/}).click();const done=await phase(page,task,'succeeded');
  const after=await evidence(page.request);expect(after.refs).toContain(done.candidateSHA+' refs/heads/main');expect(after.pushes.length-before.pushes.length).toBe(1);
  await expect(panel).toContainText('技术交付');await expect(panel.locator('a[href^="https://github.com/owner/repo/actions/runs/"]')).not.toHaveCount(0);
 });

 for(const c of [{request:'修复一次 [fail-once]',phase:'succeeded',repairs:1},{request:'三次停止 [fail-always]',phase:'blocked',repairs:3},{request:'主 CI 修复 [main-fail]',phase:'succeeded',repairs:1}]){
  test(c.request+'：真实证据与修复预算，不伪造人工认可',async({page})=>{
   await configure(page,false);const task=await addTask(page,c.request+' '+randomUUID().slice(0,6));await startTask(page,task);const d=await phase(page,task,c.phase);expect(d.repairsUsed).toBe(c.repairs);
   await expect(page.locator('.delivery-status-panel')).toContainText(`${c.repairs}/3`);
   const st=await snapshot(page.request);expect(st.tasks[task.id].status).not.toBe('completed');
   if(c.phase==='succeeded'){expect((await evidence(page.request)).refs).toContain(d.candidateSHA+' refs/heads/main');await expect(page.locator('.delivery-status-panel')).toContainText('未人工验收')}
   else {await expect(page.locator('.task-card[data-taskid="'+task.id+'"]').locator('.task-delivery-badge')).toContainText(/受阻|需要处理/)}
  });
 }

 test('主 CI 等待隔离下一张卡片，服务器继续推进而非选择控制生命周期',async({page})=>{
  await configure(page,false);await page.request.post('/__fixture/ci',{headers:authHeaders,data:{holdMain:true}});
  const a=await addTask(page,'隔离 A '+randomUUID().slice(0,6));await startTask(page,a);await phase(page,a,'main_ci');
  const b=await addTask(page,'隔离 B '+randomUUID().slice(0,6));await startTask(page,b);await phase(page,b,'preparing_integration');
  const before=await evidence(page.request);await page.reload();await expect(page.locator('#chat-title')).toContainText(b.title);expect((await evidence(page.request)).pushes.length).toBe(before.pushes.length);
  await page.request.post('/__fixture/ci',{headers:authHeaders,data:{holdMain:false}});await phase(page,a,'succeeded');const d=await phase(page,b,'succeeded');expect((await evidence(page.request)).refs).toContain(d.candidateSHA+' refs/heads/main');
 });

 test('暂停后原会话修改候选，旧验收令牌失效',async({page})=>{
  await configure(page,true);const task=await addTask(page,'暂停重验 '+randomUUID().slice(0,6));await startTask(page,task);const old=await phase(page,task,'awaiting_acceptance');
  const panel=page.locator('.delivery-status-panel');await panel.getByRole('button',{name:'暂停',exact:true}).click();await phase(page,task,'paused');
  const before=await snapshot(page.request),session=before.conversations[task.conversationId].sessionId;
  await page.locator('#draft').fill('补充效果 '+randomUUID().slice(0,6));await page.locator('#send').click();
  await expect.poll(async()=> (await snapshot(page.request)).conversations[task.conversationId].runState).toBe('idle');
  await panel.getByRole('button',{name:/恢复|继续交付/}).click();const fresh=await phase(page,task,'awaiting_acceptance');expect(fresh.candidateSHA).not.toBe(old.candidateSHA);
  const r=await page.request.post('/api/delivery/accept',{data:{operationId:randomUUID(),deliveryId:fresh.id,token:old.acceptanceToken,expectedVersion:fresh.version},headers:{'X-Kaiwu-Client':'web',Origin:'http://127.0.0.1:17332'}});expect(r.status()).toBe(409);expect((await snapshot(page.request)).conversations[task.conversationId].sessionId).toBe(session);
 });

 test('验收回执丢失后重连只读核查，不重发副作用',async({page})=>{
  const calls:any[]=[];let drop=false;
  await page.routeWebSocket('**/api/socket',client=>{const server=client.connectToServer(),methods=new Map<string,string>();client.onMessage(raw=>{const v=JSON.parse(String(raw));calls.push(v);methods.set(v.id,v.method);server.send(raw)});server.onMessage(raw=>{const v=JSON.parse(String(raw));if(v.type==='reply'&&methods.get(v.id)==='delivery/accept'&&drop){drop=false;void client.close({code:1012,reason:'lost receipt'});return}client.send(raw)})});
  await page.reload();await expect(page.locator('#chat-title')).toBeVisible();await configure(page,true);const task=await addTask(page,'丢回执 '+randomUUID().slice(0,6));await startTask(page,task);await phase(page,task,'awaiting_acceptance');const before=await evidence(page.request);
  drop=true;await page.locator('.delivery-status-panel').getByRole('button',{name:/验收.*交付|效果验收通过/}).click();await phase(page,task,'succeeded');
  const accepted=calls.find(c=>c.method==='delivery/accept');await expect.poll(()=>calls.some(c=>c.method==='operations/'+accepted.body.operationId)).toBe(true);
  await page.reload();await expect(page.locator('.delivery-status-panel')).toContainText('技术交付');
  expect(calls.filter(c=>c.method==='delivery/accept')).toHaveLength(1);expect((await evidence(page.request)).pushes.length-before.pushes.length).toBe(1);
 });

 test('较早交付步骤按真实分页加载且不重复摘要',async({page})=>{
  await configure(page,true);const task=await addTask(page,'完整历史 '+randomUUID().slice(0,6));await startTask(page,task);await phase(page,task,'awaiting_acceptance');
  for(let i=0;i<25;i++){const d=(await snapshot(page.request)).deliveries[task.id];await post(page.request,'delivery/control',{operationId:randomUUID(),deliveryId:d.id,action:'extend_budget',expectedVersion:d.version})}
  const d=(await snapshot(page.request)).deliveries[task.id];expect(d.steps).toHaveLength(20);
  const all=await post(page.request,'delivery/steps',{deliveryId:d.id,after:0,limit:100});expect(all.items.length).toBeGreaterThan(20);
  const history=page.locator('.delivery-steps-history');await history.locator('summary').click();await expect(history.getByRole('button').first()).toBeVisible();await history.getByRole('button').first().click();await expect(history.locator('li')).toHaveCount(all.items.length);
 });

 test('1440/768/360px 紧凑状态与详情收起、键盘菜单、下一轮模型',async({page})=>{
  await configure(page,true);const task=await addTask(page,'响应式验收 '+randomUUID().slice(0,6));await startTask(page,task);await phase(page,task,'awaiting_acceptance');
  for(const width of [1440,768,360]){
   await page.setViewportSize({width,height:900});
   if(width<1024 && !await page.locator('#chat').isVisible())await page.locator('.task-card[data-taskid="'+task.id+'"]').click();
   await expect(page.locator('.delivery-status-panel')).toBeVisible();expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
   expect(await page.locator('.delivery-status-panel details[open]').count()).toBe(0);
   if(width===1440||width===360){const dir=resolve('../../docs/design/web-service');mkdirSync(dir,{recursive:true});await page.screenshot({path:resolve(dir,width===1440?'automated-delivery-desktop.png':'automated-delivery-mobile.png'),fullPage:true})}
  }
  await page.setViewportSize({width:1440,height:900});await page.getByRole('button',{name:'选择会话模型',exact:true}).click();
  const dialog=page.getByRole('dialog');await dialog.getByLabel('提供商',{exact:true}).selectOption('fixture');await dialog.getByLabel('模型',{exact:true}).selectOption('another-test');await dialog.getByRole('button',{name:/下一轮使用/}).click();
  await expect(page.getByRole('button',{name:'选择会话模型',exact:true})).toContainText('待生效');const st=await snapshot(page.request);expect(st.conversations[task.conversationId].pendingProfile.model).toBe('another-test');expect(st.profile.model).toBe('isolated-test');
 });
});
