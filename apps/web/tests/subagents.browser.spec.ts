import {test,expect,type Page} from '@playwright/test';
import {mkdirSync} from 'node:fs';
import {join} from 'node:path';
const panel=(page:Page)=>page.getByRole('dialog',{name:'子 Agent 执行记录',exact:true});
async function setup(page:Page){
 await page.goto('/');
 const r=await page.request.post('/api/login',{headers:{Origin:'http://127.0.0.1:17333','X-Kaiwu-Client':'web'},data:{token:'kaiwu-subagent-fixture-token-000000'}});
 expect(r.ok()).toBe(true);
 const st=(await (await page.request.get('/api/snapshot')).json()).state;
 const p=Object.keys(st.projects)[0];
 await page.goto('/projects/'+p);
 const task=page.locator('.task-card').filter({has:page.getByRole('heading',{name:'主要任务',exact:true})});
 await expect(task).toBeVisible();
 return {task,st};
}
async function shot(page:Page,name:string){const dir=process.env.KAIWU_SUBAGENT_ARTIFACTS;if(dir){mkdirSync(dir,{recursive:true});await page.screenshot({path:join(dir,name+'.png'),fullPage:true})}}
test('card badge, safe details, pagination, keyboard, and scope isolation',async({page})=>{
 await page.setViewportSize({width:1440,height:900});
 const {task,st}=await setup(page);
 await task.locator('.subagent-badge').click();
 await expect(panel(page)).toBeVisible();
 await expect(panel(page).getByRole('button',{name:/检查实现/})).toBeVisible();
 await expect(panel(page).getByRole('button',{name:/另一任务专属/})).toHaveCount(0);
 await shot(page,'list-desktop');
 await panel(page).getByRole('button',{name:/检查实现/}).click();
 await expect(panel(page).getByText('完整要求：',{exact:false})).toBeVisible();
 await expect(panel(page).locator('script,img')).toHaveCount(0);
 await expect(panel(page).locator('.subagent-event')).toHaveCount(100);
 await panel(page).getByRole('button',{name:'加载更多',exact:true}).click();
 await expect(panel(page).getByText('记录 104',{exact:true})).toBeVisible();
 await expect(panel(page).locator('.subagent-event')).toHaveCount(108);
 await panel(page).getByText('工具输入',{exact:true}).click();
 await expect(panel(page).getByText('{"path":"README.md"}',{exact:true})).toBeVisible();
 await shot(page,'detail-desktop');
 await page.keyboard.press('Escape');
 await expect(panel(page)).toHaveCount(0);
 await expect(task.locator('.subagent-badge')).toBeFocused();
 await task.locator('.task-select').click();
 await page.locator('.chat-drawer .subagent-badge').click();
 await expect(panel(page)).toBeVisible();
 await page.keyboard.press('Escape');
 await expect(page.getByRole('dialog',{name:'聊天面板',exact:true})).toBeVisible();
 const after=(await(await page.request.get('/api/snapshot')).json()).state;
 expect(Object.keys(after.subagents)).toEqual(Object.keys(st.subagents));
 expect(Object.values(after.conversations).map((c:any)=>[c.id,c.sessionId,c.runState])).toEqual(Object.values(st.conversations).map((c:any)=>[c.id,c.sessionId,c.runState]));
});
test('late detail replies cannot replace another run; failure preserves history and retries',async({page})=>{
 let hold=true, release:(()=>void)|undefined, firstId='';
 let failNext=false;
 await page.routeWebSocket('**/api/socket',ws=>{
  const server=ws.connectToServer();
  ws.onMessage(message=>{
   const data=JSON.parse(String(message));
   if(data.method==='subagents/detail') {
    if(failNext){failNext=false;ws.send(JSON.stringify({id:data.id,type:'reply',status:503,data:{error:{code:'unavailable',message:'合成读取失败'}}}));return}
    if(hold&&!firstId)firstId=data.id;
   }
   server.send(message);
  });
  server.onMessage(message=>{
   const data=JSON.parse(String(message));
   if(hold&&data.id===firstId){release=()=>{hold=false;ws.send(message)};return}
   ws.send(message);
  });
 });
 const {task}=await setup(page);
 await task.locator('.subagent-badge').click();
 await panel(page).getByRole('button',{name:/检查实现/}).click();
 await expect.poll(()=>!!release).toBe(true);
 await panel(page).getByRole('button',{name:'← 返回列表',exact:true}).click();
 await panel(page).getByRole('button',{name:/归属待核查/}).click();
 await expect(panel(page).getByRole('heading',{name:'归属待核查',exact:true})).toBeVisible();
 release!();
 await expect(panel(page).getByText('完整要求：',{exact:false})).toBeVisible();
 await expect(panel(page).getByRole('heading',{name:/检查实现/})).toHaveCount(0);
 failNext=true;
 await panel(page).getByRole('button',{name:'加载更多',exact:true}).click();
 await expect(panel(page).getByRole('alert')).toContainText('合成读取失败');
 await expect(panel(page).locator('.subagent-event')).toHaveCount(100);
 await panel(page).getByRole('button',{name:'重试',exact:true}).click();
 await expect(panel(page).getByText('记录 104',{exact:true})).toBeVisible();
 await expect(panel(page).getByRole('alert')).toHaveCount(0);
});
test('360px panel, offline history, reconnect and real socket version updates',async({page,context})=>{
 await page.setViewportSize({width:360,height:780});
 const {task}=await setup(page);
 await task.locator('.subagent-badge').click();
 await panel(page).getByRole('button',{name:/检查实现/}).click();
 await panel(page).getByRole('button',{name:'加载更多',exact:true}).click();
 await expect(panel(page).getByText('记录 104',{exact:true})).toBeVisible();
 await context.setOffline(true);
 await expect(panel(page).getByText('连接已断开，记录可能不是最新；重连后更新。')).toBeVisible();
 await expect(panel(page).getByText('记录 104',{exact:true})).toBeVisible();
 await shot(page,'detail-mobile-offline');
 await context.setOffline(false);
 await expect(panel(page).getByText('连接已断开，记录可能不是最新；重连后更新。')).toHaveCount(0);
 await page.request.post('/fixture/append');
 await expect(panel(page).getByText(/实时增量/)).toBeVisible();
 await panel(page).getByRole('button',{name:'← 返回列表',exact:true}).click();
 await shot(page,'list-mobile');
 await page.keyboard.press('Tab');
 expect(await panel(page).evaluate(el=>el.contains(document.activeElement))).toBe(true);
 expect((await panel(page).boundingBox())!.width).toBe(360);
});
