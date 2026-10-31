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
 await page.getByRole('button',{name:'＋ 新建终端'}).click();
 await expect(page.locator('.terminal-status')).toContainText('已连接');
}
async function command(page:Page,value:string){
 await page.getByLabel('终端输入',{exact:true}).focus();
 await page.getByLabel('终端输入',{exact:true}).evaluate((el,text)=>{const data=new DataTransfer();data.setData('text/plain',text);el.dispatchEvent(new ClipboardEvent('paste',{clipboardData:data,bubbles:true,cancelable:true}))},value);await page.keyboard.press('Enter');
}
async function shot(page:Page,name:string){
 const dir=process.env.KAIWU_EVIDENCE_DIR;if(!dir)return;
 mkdirSync(dir,{recursive:true});await page.screenshot({path:resolve(dir,name+'.png')});
}

test('default terminal accepts input-only text and emoji, rather than dropping it in reader mode',async({page})=>{
 const output:Buffer[]=[];
 page.on('websocket',ws=>{if(ws.url().includes('/api/tools/terminal/socket'))ws.on('framereceived',event=>{
  const frame=JSON.parse(String(event.payload));if(frame.data)output.push(Buffer.from(frame.data,'base64'));
 })});
 await start(page);
 await page.getByLabel('终端输入',{exact:true}).focus();
 await page.keyboard.insertText("printf 'INPUT_ONLY_%s 中文 🌱\\n' OK");await page.keyboard.press('Enter');
 await expect.poll(()=>Buffer.concat(output).toString('utf8')).toContain('INPUT_ONLY_OK 中文 🌱\r\n');
 await page.getByRole('button',{name:'结束终端'}).click();
});

test('xterm real shell: inline input, history, completion, clear, interrupt, resize and reconnect',async({page})=>{
 test.setTimeout(60000);
 const errors:string[]=[];page.on('pageerror',e=>errors.push(e.message));
 await page.setViewportSize({width:1440,height:900});await start(page);
 await page.getByRole('button',{name:'终端读屏模式'}).click();
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
 await expect.poll(async()=>((await screen.textContent())??'').match(/SIZE:(\d+) (\d+)/)?.[2]).toMatch(/^([2-9]|[1-3]\d|4[0-5])$/);
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
 await page.getByRole('button',{name:'终端读屏模式'}).click();
 await expect(page.locator('.terminal-tab')).toHaveText(id!);
 await expect(screen).toContainText('STREAM_DONE');
 await command(page,"printf 'RECONNECTED_OK\\n'");await expect(screen).toContainText('RECONNECTED_OK');
 await page.getByRole('button',{name:'结束终端'}).click();
 await expect(page.getByRole('button',{name:'＋ 新建终端'})).toBeVisible();
 expect(errors).toEqual([]);
});
