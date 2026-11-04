import {test,expect,type Page} from '@playwright/test';
import {mkdirSync} from 'node:fs';
import {join,dirname} from 'node:path';

async function setup(page:Page) {
 await page.goto('/');
 const r=await page.request.post('/api/login',{headers:{Origin:'http://127.0.0.1:17335','X-Kaiwu-Client':'web'},data:{token:'kaiwu-managed-fixture-token-000000'}});
 expect(r.ok()).toBe(true);await page.reload();
 await expect(page.getByRole('button',{name:'当前项目',exact:true})).toBeVisible();
}
async function snapshot(page:Page) {return (await(await page.request.get('/api/snapshot')).json()).state}
async function choose(page:Page,name:string) {
 await page.getByRole('button',{name:'当前项目',exact:true}).click();
 await page.getByRole('menuitemradio',{name,exact:true}).click();
}
async function openCreate(page:Page) {
 await page.getByRole('button',{name:'当前项目',exact:true}).click();
 await page.getByRole('menuitem',{name:'添加项目',exact:true}).click();
 return page.getByRole('dialog',{name:'创建项目',exact:true});
}
async function shot(page:Page,name:string) {
 const dir=process.env.KAIWU_MANAGED_ARTIFACTS;if(dir){mkdirSync(dir,{recursive:true});await page.screenshot({path:join(dir,name+'.png'),fullPage:true})}
}
for(const width of [1440,390]){
 test('name-only creation, same-name disambiguation, refresh and rename '+width,async({page})=>{
  await page.setViewportSize({width,height:900});await setup(page);
  const name='中文项目'+width;
  let form=await openCreate(page);
  await expect(form).toBeVisible();
  await expect(form.locator('input[name=path],input[name=remoteURL]')).toHaveCount(0);
  await form.getByRole('textbox',{name:'项目名称',exact:true}).fill(name);
  await shot(page,'create-'+width);
  await form.getByRole('button',{name:'保存',exact:true}).click();
  await expect(form).toHaveCount(0);
  await expect(page.getByRole('button',{name:'当前项目'})).toContainText(name);
  const first=Object.values((await snapshot(page)).projects).find((p:any)=>p.name===name) as any;
  expect(first.storageMode).toBe('managed');expect(first.path).toBe(first.storageRoot+'/repo');
  form=await openCreate(page);await form.getByRole('textbox',{name:'项目名称',exact:true}).fill(name);
  await form.getByRole('button',{name:'保存',exact:true}).click();await expect(form).toHaveCount(0);
  const projects=Object.values((await snapshot(page)).projects).filter((p:any)=>p.name===name) as any[];
  expect(projects).toHaveLength(2);expect(projects[0].path).not.toBe(projects[1].path);
  await page.reload();await expect(page.getByRole('button',{name:'当前项目'})).toContainText(name);
  const currentId=await page.getByRole('button',{name:'当前项目'}).getAttribute('data-project-id');
  const before=(await snapshot(page)).projects[currentId!];
  await page.getByRole('button',{name:'当前项目'}).click();await page.getByRole('menuitem',{name:'重命名项目',exact:true}).click();
  const rename=page.getByRole('dialog',{name:'重命名项目',exact:true});
  await rename.getByRole('textbox',{name:'项目名称',exact:true}).fill(name+'新名');
  await shot(page,'rename-'+width);
  await rename.getByRole('button',{name:'保存',exact:true}).click();
  await expect(page.getByRole('button',{name:'当前项目'})).toContainText(name+'新名');
  const after=(await snapshot(page)).projects[currentId!];expect(after).toEqual({...before,name:name+'新名'});
 });
}
test('failed creation retry retains its operation id and creates only one project',async({page})=>{
 const ids:string[]=[];
 await page.routeWebSocket('**/api/socket',ws=>{
  const server=ws.connectToServer();
  ws.onMessage(message=>{const v=JSON.parse(String(message));if(v.method==='commands'&&v.body.type==='create_project')ids.push(v.body.operationId);server.send(message)});
  server.onMessage(message=>ws.send(message));
 });
 await page.setViewportSize({width:390,height:844});
 await setup(page);const before=Object.keys((await snapshot(page)).projects).length;
 expect((await page.request.post('/fixture/fail-next-git')).ok()).toBe(true);
 const form=await openCreate(page);
 await form.getByRole('textbox',{name:'项目名称',exact:true}).fill('创建失败后重试');
 await form.getByRole('button',{name:'保存',exact:true}).click();
 await expect(form.getByRole('alert')).toContainText('原身份和文件已保留');
 await expect(form.getByRole('button',{name:'保存',exact:true})).toBeEnabled();
 await page.reload();
 const recovered=await openCreate(page);
 await expect(recovered.getByRole('button',{name:'重试创建 创建失败后重试',exact:true})).toBeVisible();
 await shot(page,'recovery-pending-390');
 await recovered.getByRole('button',{name:'重试创建 创建失败后重试',exact:true}).click();
 await expect(recovered).toHaveCount(0);
 expect(ids).toHaveLength(2);expect(ids[0]).toBe(ids[1]);
 expect(Object.keys((await snapshot(page)).projects)).toHaveLength(before+1);
});

test('file config inherits, saves with version and freezes future tasks only',async({page})=>{
 const errors:string[]=[];page.on('pageerror',error=>errors.push(error.message));
 await setup(page);
 const st=await snapshot(page);
 const created=await page.request.post('/api/commands',{headers:{Origin:'http://127.0.0.1:17335','X-Kaiwu-Client':'web'},data:{operationId:'config-browser-project',type:'create_project',name:'配置专项项目',revision:st.revision}});
 expect(created.ok(),await created.text()).toBe(true);
 const p=(await snapshot(page)).projects[(await created.json()).createdId];await page.reload();
 await choose(page,'配置专项项目');
 await page.getByRole('button',{name:'当前项目',exact:true}).click();
 await page.getByRole('menuitem',{name:'AI 配置',exact:true}).click();
 const d=page.getByRole('dialog',{name:'AI 配置',exact:true});
 await expect(d).toBeVisible();
 await d.getByRole('button',{name:'当前项目',exact:true}).click();
 await d.getByRole('checkbox',{name:'覆盖规则',exact:true}).check();
 await d.getByRole('textbox',{name:'规则',exact:true}).fill('仅对新建任务生效的项目规则');
 await d.getByRole('button',{name:'保存配置',exact:true}).click();
 expect(errors).toEqual([]);
 await expect(d.getByRole('status')).toContainText('已保存');
 await expect(d.getByText('规则来源：项目覆盖', {exact:true})).toBeVisible();
 await page.setViewportSize({width:390,height:844});await shot(page,'config-390');
 await page.setViewportSize({width:1440,height:900});await shot(page,'config-1440');
 await d.getByRole('button',{name:'关闭',exact:true}).click();
 const create=async(op:string)=>{
  const state=await snapshot(page);
  const response=await page.request.post('/api/commands',{headers:{Origin:'http://127.0.0.1:17335','X-Kaiwu-Client':'web'},data:{operationId:op,type:'add_task',projectId:p.id,request:'冻结规则',revision:state.revision}});
  expect(response.ok(),await response.text()).toBe(true);return (await response.json()).createdId;
 };
 const id=await create('frozen-rule-first'),before=(await snapshot(page)).tasks[id].configSnapshot;
 const config=(await (await page.request.get('/api/ai/config?projectId='+p.id)).json());
 const updated=await page.request.put('/api/ai/config',{headers:{Origin:'http://127.0.0.1:17335','X-Kaiwu-Client':'web'},data:{projectId:p.id,version:config.version,config:{schemaVersion:1,rules:[]}}});expect(updated.ok()).toBe(true);
 const second=await create('frozen-rule-second'),after=await snapshot(page);
 expect(before.rules).toEqual(['仅对新建任务生效的项目规则']);
 expect(after.tasks[id].configSnapshot).toEqual(before);
 expect(after.tasks[second].configSnapshot.rules).toEqual([]);
});

test('invalid name keeps input; late rename receipt never switches projects',async({page})=>{
 await setup(page);
 const initial=await snapshot(page),sample=Object.values(initial.projects)[0] as any;
 const legacy=await page.request.post('/api/commands',{headers:{Origin:'http://127.0.0.1:17335','X-Kaiwu-Client':'web'},data:{type:'add_project',operationId:'legacy-browser-project',name:'旧项目',path:join(dirname(dirname(sample.storageRoot)),'legacy'),revision:initial.revision}});
 expect(legacy.ok()).toBe(true);await page.reload();await choose(page,'旧项目');
 let form=await openCreate(page);
 await form.getByRole('textbox',{name:'项目名称',exact:true}).fill('../escape');
 await form.getByRole('button',{name:'保存',exact:true}).click();
 await expect(form.getByRole('alert')).not.toBeEmpty();await expect(form.getByRole('textbox',{name:'项目名称',exact:true})).toHaveValue('../escape');
 await form.getByRole('button',{name:'取消',exact:true}).click();
 const old=Object.values((await snapshot(page)).projects).find((p:any)=>p.name==='旧项目') as any;
 const other=Object.values((await snapshot(page)).projects).find((p:any)=>p.id!==old.id) as any;
 let release:(()=>void)|undefined,commandId='';
 await page.routeWebSocket('**/api/socket',ws=>{
  const server=ws.connectToServer();
  ws.onMessage(message=>{const v=JSON.parse(String(message));if(v.method==='commands'&&v.body.type==='rename_project')commandId=v.id;server.send(message)});
  server.onMessage(message=>{const v=JSON.parse(String(message));if(commandId&&v.id===commandId){release=()=>ws.send(message)}else ws.send(message)});
 });
 await page.reload();await expect(page.getByRole('button',{name:'当前项目'})).toContainText('旧项目');
 await page.getByRole('button',{name:'当前项目'}).click();await page.getByRole('menuitem',{name:'重命名项目',exact:true}).click();
 form=page.getByRole('dialog',{name:'重命名项目',exact:true});
 await form.getByRole('textbox',{name:'项目名称',exact:true}).fill('旧项目改名');
 await form.getByRole('button',{name:'保存',exact:true}).click();await expect.poll(()=>!!release).toBe(true);
 await page.keyboard.press('Escape');await choose(page,other.name);release!();
 await expect(page.getByRole('button',{name:'当前项目'})).toHaveAttribute('data-project-id',other.id);
 await expect.poll(async()=>(await snapshot(page)).projects[old.id].name).toBe('旧项目改名');
 const final=(await snapshot(page)).projects[old.id];expect(final).toEqual({...old,name:'旧项目改名'});
});
