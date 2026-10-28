import { expect, type Page, type Locator } from '@playwright/test';

// Legacy business scenarios used both panes at once. Navigate through the real
// drawer controls now; never force-click inert content or change server state.
export async function closeChat(page: Page) {
  const close = page.getByRole('button', { name: '收起聊天', exact: true });
  if (await page.locator('.chat-drawer[open]').count()) await close.click();
  await expect(page.locator('.chat-drawer')).not.toBeVisible();
}
export async function openChat(page: Page) {
  // A closing dialog remains painted briefly during its exit transition, but is no longer open.
  if (await page.locator('.chat-drawer[open]').count()) return;
  const resultChat = page.getByRole('button', { name: '返回任务对话', exact: true });
  const mobile = page.getByRole('button', { name: '◌ 聊天', exact: true });
  if (await resultChat.isVisible()) await resultChat.click();
  else if (await mobile.isVisible()) await mobile.click();
  else {
    const taskId = new URL(page.url()).pathname.match(/\/tasks\/([^/]+)$/)?.[1];
    if (taskId) await page.locator('.task-card[data-taskid="' + taskId + '"] .task-select').click();
    else await page.getByRole('button', { name: '项目对话', exact: true }).click();
  }
  await expect(page.locator('.chat-drawer')).toHaveJSProperty('open', true);
  await expect(page.locator('.chat-drawer')).toBeVisible();
}
export async function clickInView(page: Page, target: Locator) {
  if(await target.evaluate(el=>el.ownerDocument.defaultView!==el.ownerDocument.defaultView?.top)){await target.click();return;}
  const location = await target.evaluate(el => ({ chat: !!el.closest('.chat-drawer'), modal: !!el.closest('dialog') }));
  if (location.chat) await openChat(page);
  else if (!location.modal) await closeChat(page);
  await target.click();
}
export async function fillInChat(page: Page, target: Locator, text: string) {
  await openChat(page);
  await target.fill(text);
}

// The explicit completion entry is a card action, never a hidden chat control.
export async function markTaskComplete(page:Page, card?:Locator){
 await closeChat(page);
 const back=page.getByRole('button',{name:'← 返回看板',exact:true});if(await back.isVisible())await back.click();
 const taskId=new URL(page.url()).pathname.match(/\/tasks\/([^/]+)$/)?.[1];
 card??=taskId?page.locator('.task-card[data-taskid="'+taskId+'"]'):page.locator('.task-card').first();
 await card.getByRole('button',{name:'任务操作',exact:true}).click();
 await page.getByRole('menuitem',{name:'标记任务完成',exact:true}).click();
 await page.getByRole('dialog',{name:'标记任务完成',exact:true}).getByRole('button',{name:'标记完成',exact:true}).click();
 await expect(card.locator('.task-status')).toHaveText('已完成');await openChat(page);
}
