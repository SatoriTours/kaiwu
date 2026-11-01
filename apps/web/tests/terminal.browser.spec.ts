import {test,expect,type Page} from '@playwright/test';
import {readFileSync,mkdirSync} from 'node:fs';
import {resolve} from 'node:path';
import {randomUUID} from 'node:crypto';

async function start(page:Page){
 await page.goto('/');
 const headers={Origin:new URL(page.url()).origin,'X-Kaiwu-Client':'web'};
 expect((await page.request.post('/api/login',{headers,data:{token:'kaiwu-browser-test-token-00000000'}})).ok()).toBeTruthy();
 const path=readFileSync(resolve('../../.superpowers/sdd/2026-10-11-web-service/browser-project.txt'),'utf8')+'/terminal-'+randomUUID();
 const state=(await(await page.request.get('/api/snapshot')).json()).state;
 const response=await page.request.post('/api/commands',{headers,data:{operationId:randomUUID(),revision:state.revision,type:'add_project',name:'交互终端验收',path}});
 expect(response.ok()).toBeTruthy();
 await page.goto('/projects/'+(await response.json()).createdId);
 await page.getByRole('button',{name:'终端',exact:true}).click();
 await expect(page.locator('.terminal-status')).toContainText('已连接');
}
async function command(page:Page,value:string){
 await page.getByLabel('终端输入',{exact:true}).focus();
 await page.getByLabel('终端输入',{exact:true}).evaluate((el,text)=>{const data=new DataTransfer();data.setData('text/plain',text);el.dispatchEvent(new ClipboardEvent('paste',{clipboardData:data,bubbles:true,cancelable:true}))},value);await page.keyboard.press('Enter');
}
async function reader(page:Page){await page.getByRole('button',{name:'终端更多操作'}).click();await page.getByRole('button',{name:'终端读屏模式'}).click()}
async function end(page:Page){await page.getByRole('button',{name:'终端更多操作'}).click();await page.getByRole('button',{name:'结束终端',exact:true}).click();await page.getByRole('dialog',{name:'结束终端'}).getByRole('button',{name:'结束终端',exact:true}).click()}
async function shot(page:Page,name:string){
 const dir=process.env.KAIWU_EVIDENCE_DIR;if(!dir)return;
 mkdirSync(dir,{recursive:true});await page.locator('.terminal-surface').evaluate(el=>new Promise<void>(resolve=>requestAnimationFrame(()=>requestAnimationFrame(()=>resolve()))));await page.screenshot({path:resolve(dir,name+'.png')});
}

test('entry auto opens, restores two independent terminals and uses an edge-to-edge themed header',async({page,context})=>{
 test.setTimeout(60000);
 await page.goto('/');
 const headers={Origin:new URL(page.url()).origin,'X-Kaiwu-Client':'web'};
 await page.request.post('/api/login',{headers,data:{token:'kaiwu-browser-test-token-00000000'}});
 const state=(await(await page.request.get('/api/snapshot')).json()).state;
 const root=readFileSync(resolve('../../.superpowers/sdd/2026-10-11-web-service/browser-project.txt'),'utf8')+'/multi-'+randomUUID();
 const response=await page.request.post('/api/commands',{headers,data:{operationId:randomUUID(),revision:state.revision,type:'add_project',name:'多终端',path:root}});
 const url='/projects/'+(await response.json()).createdId;
 await page.goto(url);await page.getByRole('button',{name:'终端',exact:true}).click();
 await expect(page.getByRole('tab',{selected:true})).toBeVisible();
 await expect(page.locator('.terminal-status')).toContainText('已连接');
 const first=await page.getByRole('tab',{selected:true}).getAttribute('data-terminal-id');
 await command(page,"cd /tmp; export KAIWU_RESTORE=ORIGINAL; printf 'FIRST_READY\\n'");
 await page.getByRole('button',{name:'新建终端',exact:true}).click();
 await expect(page.getByRole('tab')).toHaveCount(2);
 await expect(page.locator('.terminal-status')).toContainText('已连接');
 const second=await page.getByRole('tab',{selected:true}).getAttribute('data-terminal-id');
 expect(second).not.toBe(first);
 await command(page,"export KAIWU_RESTORE=SECOND; printf 'SECOND_READY\\n'");
 await page.close();
 const restored=await context.newPage();await restored.goto(url);
 await expect(restored.getByRole('tab')).toHaveCount(2);
 await expect(restored.getByRole('tab',{selected:true})).toHaveAttribute('data-terminal-id',second!);
 await expect(restored.locator('.terminal-status')).toContainText('已连接');
 await reader(restored);
 await command(restored,"printf 'RESTORE_%s\\n' \"$KAIWU_RESTORE\"");
 await expect(restored.locator('.xterm-accessibility-tree')).toContainText('RESTORE_SECOND');
 await restored.locator('[role=tab][data-terminal-id="'+first+'"]').click();
 await command(restored,"printf 'RESTORE_%s\\n' \"$KAIWU_RESTORE\"; pwd");
 await expect(restored.locator('.xterm-accessibility-tree')).toContainText('RESTORE_ORIGINAL');
 await expect(restored.locator('.xterm-accessibility-tree')).toContainText('/tmp');
 for(const theme of ['dark','light']){
  await restored.getByRole('combobox',{name:'外观主题',exact:true}).click();await restored.getByRole('option',{name:theme==='dark'?'深色':'浅色',exact:true}).click();
  await command(restored,"printf 'THEME_"+theme.toUpperCase()+"_READY\\n'");
  await expect(restored.locator('.xterm-accessibility-tree')).toContainText('THEME_'+theme.toUpperCase()+'_READY');
  for(const width of [1440,1024,390,360]){
   await restored.setViewportSize({width,height:844});
   await expect.poll(()=>restored.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBeTruthy();
   const geometry=await restored.locator('.terminal-viewport').evaluate(el=>{const r=el.getBoundingClientRect(),top=document.querySelector('.top')!.getBoundingClientRect();return {x:r.x,top:r.top-top.bottom,right:innerWidth-r.right}});
   expect(geometry.x).toBe(0);expect(geometry.top).toBe(0);expect(geometry.right).toBe(0);
  }
  await restored.setViewportSize({width:1440,height:844});await shot(restored,'terminal-A-'+theme+'-desktop');
  await restored.setViewportSize({width:390,height:844});await shot(restored,'terminal-A-'+theme+'-mobile');
 }
 await restored.setViewportSize({width:1440,height:844});
 const inactive=restored.locator('.terminal-tab-wrap').filter({has:restored.locator('[data-terminal-id="'+second+'"]')});
 await inactive.hover();await inactive.locator('.terminal-tab-close').click();
 await expect(restored.getByRole('tab',{selected:true})).toHaveAttribute('data-terminal-id',first!);
 await restored.getByRole('dialog',{name:'结束终端'}).getByRole('button',{name:'取消',exact:true}).click();
 await expect(restored.getByRole('tab')).toHaveCount(2);
 await inactive.hover();await inactive.locator('.terminal-tab-close').click();
 await restored.getByRole('dialog',{name:'结束终端'}).getByRole('button',{name:'结束终端',exact:true}).click();
 await expect(restored.getByRole('tab')).toHaveCount(1);
 await expect(restored.getByRole('tab',{selected:true})).toHaveAttribute('data-terminal-id',first!);
 for(let n=0;n<1;n++){
  await restored.getByRole('button',{name:'终端更多操作'}).click();
  await restored.getByRole('button',{name:'结束终端',exact:true}).click();
  await restored.getByRole('dialog',{name:'结束终端'}).getByRole('button',{name:'结束终端',exact:true}).click();
 }
 await expect(restored.getByRole('tab')).toHaveCount(0);
 await restored.reload();await expect(restored.getByRole('tab')).toHaveCount(0);
 await expect(restored.getByRole('button',{name:'重新打开终端'})).toBeVisible();
});

test('natural exit restores the selected result without marking it manually closed',async({page})=>{
 await start(page);await reader(page);
 const id=await page.getByRole('tab',{selected:true}).getAttribute('data-terminal-id');
 await command(page,"printf 'NATURAL_FINAL_RESULT\\n'; exit");
 await expect(page.locator('.terminal-status')).toContainText('未运行');
 await expect(page.locator('.xterm-accessibility-tree')).toContainText('NATURAL_FINAL_RESULT');
 await page.reload();
 await expect(page.getByRole('tab',{selected:true})).toHaveAttribute('data-terminal-id',id!);
 await reader(page);
 await expect(page.locator('.xterm-accessibility-tree')).toContainText('NATURAL_FINAL_RESULT');
 const state=await page.evaluate(()=>JSON.parse(localStorage.getItem('kaiwu.terminal.view.'+location.pathname.split('/')[2])!));
 expect(state.closed).toBe(false);expect(state.terminalId).toBe(id);
 await end(page);
});
test('arrow navigation keeps focus on tabs after the restored first frame',async({page})=>{
 await start(page);await page.getByRole('button',{name:'新建终端',exact:true}).click();
 await expect(page.getByRole('tab')).toHaveCount(2);await expect(page.locator('.terminal-status')).toContainText('已连接');
 const tabs=page.getByRole('tab');await tabs.nth(1).focus();await page.keyboard.press('ArrowLeft');
 await expect(tabs.nth(0)).toHaveAttribute('aria-selected','true');
 await expect(page.locator('.terminal-status')).toContainText('已连接');
 await expect(tabs.nth(0)).toBeFocused();
 await page.keyboard.press('End');await expect(tabs.nth(1)).toHaveAttribute('aria-selected','true');
 await expect(page.locator('.terminal-status')).toContainText('已连接');await expect(tabs.nth(1)).toBeFocused();
 await end(page);await end(page);
});
test('pending ensure aliases are read-only resolved before an explicit new terminal',async({page})=>{
 await start(page);
 const id=new URL(page.url()).pathname.split('/')[2]!,operationId=randomUUID();
 const headers={Origin:new URL(page.url()).origin,'X-Kaiwu-Client':'web'};
 const alias=await page.request.post('/api/tools/terminal/start',{headers,data:{projectId:id,operationId,ensure:true}});
 expect(alias.ok()).toBeTruthy();
 await page.evaluate(({id,operationId})=>localStorage.setItem('kaiwu.terminal.pending.'+id+'/',operationId),{id,operationId});
 await page.reload();await expect(page.locator('.terminal-status')).toContainText('已连接');
 await expect.poll(()=>page.evaluate(id=>localStorage.getItem('kaiwu.terminal.pending.'+id+'/'),id)).toBe('');
 await page.getByRole('button',{name:'新建终端',exact:true}).click();
 await expect(page.getByRole('tab')).toHaveCount(2);
 await end(page);await end(page);
});
test('a resolved launch receipt repairs an older empty list without replaying creation',async({page})=>{
 await start(page);
 const id=new URL(page.url()).pathname.split('/')[2]!,operationId=randomUUID();
 const original=await page.getByRole('tab',{selected:true}).getAttribute('data-terminal-id');
 const headers={Origin:new URL(page.url()).origin,'X-Kaiwu-Client':'web'};
 expect((await page.request.post('/api/tools/terminal/start',{headers,data:{projectId:id,operationId,ensure:true}})).ok()).toBeTruthy();
 await page.evaluate(({id,operationId})=>localStorage.setItem('kaiwu.terminal.pending.'+id+'/',operationId),{id,operationId});
 // Equivalent stale inventory: list predates a completed launch, while the
 // operation query reads the authoritative post-launch receipt. No fake shell.
 let delivered=false;
 await page.routeWebSocket('**/api/socket',client=>{
  const server=client.connectToServer(),lists=new Set<string>();
  client.onMessage(raw=>{const frame=JSON.parse(String(raw));if(frame.method==='tools/terminal/list')lists.add(frame.id);server.send(raw)});
  server.onMessage(raw=>{const frame=JSON.parse(String(raw));if(!delivered&&frame.type==='reply'&&lists.has(frame.id)){delivered=true;frame.data=[];client.send(JSON.stringify(frame))}else client.send(raw)});
 });
 await page.reload();await expect(page.locator('.terminal-status')).toContainText('已连接');
 await expect(page.getByRole('tab',{selected:true})).toHaveAttribute('data-terminal-id',original!);
 expect(delivered).toBe(true);
 const result=await page.request.post('/api/tools/terminal/list',{headers,data:{projectId:id}});
 expect((await result.json()).filter((t:{running:boolean})=>t.running)).toHaveLength(1);
 await end(page);
});
test('14px terminal uses actual compact 17px cells',async({page})=>{
 await start(page);await reader(page);
 const height=await page.locator('.xterm-accessibility-tree').evaluate(el=>el.firstElementChild!.getBoundingClientRect().height);
 console.log('actual terminal cell height:',height);
 expect(height).toBeGreaterThanOrEqual(16);expect(height).toBeLessThanOrEqual(17.5);
 await end(page);
});

test('default terminal accepts input-only text and emoji, rather than dropping it in reader mode',async({page})=>{
 const output:Buffer[]=[];
 page.on('websocket',ws=>{if(ws.url().includes('/api/tools/terminal/socket'))ws.on('framereceived',event=>{
  const frame=JSON.parse(String(event.payload));if(frame.data)output.push(Buffer.from(frame.data,'base64'));
 })});
 await start(page);
 await page.getByLabel('终端输入',{exact:true}).focus();
 await page.keyboard.insertText("printf 'INPUT_ONLY_%s 中文 🌱\\n' OK");await page.keyboard.press('Enter');
 await expect.poll(()=>Buffer.concat(output).toString('utf8')).toContain('INPUT_ONLY_OK 中文 🌱\r\n');
 await end(page);
});

test('xterm real shell: inline input, history, completion, clear, interrupt, resize and reconnect',async({page})=>{
 test.setTimeout(60000);
 const errors:string[]=[];page.on('pageerror',e=>errors.push(e.message));
 await page.setViewportSize({width:1440,height:900});await start(page);
 await reader(page);
 const screen=page.locator('.xterm-accessibility-tree');
 await expect(page.getByRole('button',{name:'执行 ↵'})).toHaveCount(0);
 await command(page,"printf '\\033[32m中文终端就绪\\033[0m\\n'");
 await expect(screen).toContainText('中文终端就绪');
 // Up recalls the shell's previous command; verify the actual output repeats.
 await page.keyboard.press('ArrowUp');await page.keyboard.press('Enter');
 await expect.poll(async()=>((await screen.textContent())??'').split('中文终端就绪').length).toBeGreaterThanOrEqual(5);
 await command(page,'touch completion-example.txt');
 await page.getByLabel('终端输入',{exact:true}).pressSequentially('cat completion-ex');await page.keyboard.press('Tab');
 await expect(screen).toContainText('cat completion-example.txt');
 await page.keyboard.press('Control+c');
 await command(page,'clear');
 await expect(screen).not.toContainText('中文终端就绪');
 await command(page,"printf '\\033[36m开悟 · 直接输入命令\\033[0m\\n'; printf 'TERM=%s\\n' \"$TERM\"; stty size");
 await expect(screen).toContainText('TERM=xterm-256color');
 await shot(page,'terminal-desktop');
 // Terminal resizing reaches the real PTY, not just the canvas dimensions.
 await page.setViewportSize({width:390,height:844});
 await expect.poll(()=>page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBeTruthy();
 await expect.poll(async()=>(await page.locator('.xterm-screen').boundingBox())!.width).toBeLessThan(390);
 await command(page,"printf 'SIZE:'; stty size");
 await expect.poll(async()=>((await screen.textContent())??'').match(/SIZE:(\d+) (\d+)/)?.[2]).toMatch(/^([2-9]|[1-3]\d|4[0-8])$/);
 await shot(page,'terminal-mobile');
 await command(page,'sleep 30');await page.keyboard.press('Control+c');
 await command(page,"printf 'INTERRUPTED_OK\\n'");await expect(screen).toContainText('INTERRUPTED_OK');
 // Reject oversized paste atomically; never execute a partial paste.
 await page.getByLabel('终端输入',{exact:true}).evaluate(el=>{
  const data=new DataTransfer();data.setData('text/plain','x'.repeat(4097));
  el.dispatchEvent(new ClipboardEvent('paste',{clipboardData:data,bubbles:true,cancelable:true}));
 });
 await expect(page.getByRole('alert')).toContainText('4096');
 await command(page,"printf 'PASTE_REJECTED_OK\\n'");await expect(screen).toContainText('PASTE_REJECTED_OK');
 await command(page,"printf '\\344'; sleep 0.15; printf '\\270\\255\\n'");
 await expect(screen).not.toContainText('�');
 await command(page,"seq 1 15000; printf 'STREAM_DONE\\n'");
 await expect(screen).toContainText('STREAM_DONE');
 const id=await page.locator('.terminal-tab').textContent();
 await page.getByRole('button',{name:'看板',exact:true}).first().click();
 await page.getByRole('button',{name:'终端',exact:true}).click();
 await reader(page);
 await expect(page.locator('.terminal-tab')).toHaveText(id!);
 await expect(screen).toContainText('STREAM_DONE');
 await command(page,"printf 'RECONNECTED_OK\\n'");await expect(screen).toContainText('RECONNECTED_OK');
 await end(page);
 await expect(page.getByRole('button',{name:'新建终端'})).toBeVisible();
 expect(errors).toEqual([]);
});

test('terminal creation works on LAN HTTP without secure-context randomUUID',async({page})=>{
 // LAN HTTP exposes getRandomValues, but not randomUUID. Node fixture IDs remain real.
 await page.addInitScript(()=>Object.defineProperty(Crypto.prototype,'randomUUID',{configurable:true,value:undefined}));
 await start(page);
 expect(await page.evaluate(()=>typeof crypto.randomUUID)).toBe('undefined');
 const first=await page.getByRole('tab',{selected:true}).getAttribute('data-terminal-id');
 await page.getByRole('button',{name:'新建终端',exact:true}).click();
 await expect(page.getByRole('tab')).toHaveCount(2);
 await expect(page.locator('.terminal-status')).toContainText('已连接');
 const second=await page.getByRole('tab',{selected:true}).getAttribute('data-terminal-id');
 expect(second).not.toBe(first);
 const projectId=new URL(page.url()).pathname.split('/')[2]!;
 const response=await page.request.post('/api/tools/terminal/list',{headers:{Origin:new URL(page.url()).origin,'X-Kaiwu-Client':'web'},data:{projectId}});
 expect(response.ok()).toBeTruthy();
 const running=(await response.json()).filter((t:{running:boolean})=>t.running);
 expect(running).toHaveLength(2);
 const operations=running.map((t:{operationId:string})=>t.operationId);
 expect(new Set(operations).size).toBe(2);
 for(const id of operations){expect(typeof id).toBe('string');expect(id.length).toBeGreaterThan(0);expect(id.length).toBeLessThanOrEqual(100)}
 await end(page);await end(page);
});
