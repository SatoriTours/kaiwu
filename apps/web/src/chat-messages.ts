import type {Message} from './types.ts';

export interface ChatEntry {id:string;message:Message;updates?:Message[]}

// Keep stored messages intact; only consecutive process updates share a row.
export function groupChatMessages(messages:Message[]):ChatEntry[] {
 const entries:ChatEntry[]=[];
 for(const message of messages){
  const previous=entries.at(-1);
  if(message.role==='assistant'&&message.kind==='progress'){
   if(previous?.updates){previous.updates.push(message);previous.message=message}
   else entries.push({id:message.id,message,updates:[message]});
  }else entries.push({id:message.id,message});
 }
 return entries;
}

export function messageText(message:Message):string {
 if(message.displayText!==undefined)return message.displayText;
 const marker=message.text.indexOf('<kaiwu-report>');
 return message.delivery==='accepted'&&marker>=0?message.text.slice(0,marker):message.text;
}

function summarySource(message:Message):string {
 let text=messageText(message).split('<kaiwu-report>')[0]??'';
 if(message.displayText!==undefined){
  const report=message.text.match(/<kaiwu-report>([\s\S]*?)<\/kaiwu-report>/);
  if(report)try{const value=JSON.parse(report[1]!);if(typeof value.summary==='string'&&value.summary.trim())text=value.summary}catch{/* Keep the visible reply as fallback. */}
 }
 return text;
}

export function replySummary(message:Message):string {
 const text=summarySource(message);
 const prose=text.replace(/```[^\n]*\n[\s\S]*?(?:```|$)/g,'').trim();
 if(!prose)return text.trim()?'已返回代码或执行输出，展开查看。':message.delivery==='accepted'?'正在处理，等待进展说明…':'本条更新没有文字说明，展开查看记录。';
 // Preserve Markdown; old plain Chinese run-on text gets sentence breaks.
 const formatted=prose.includes('\n')?prose:prose.replace(/([。！？])\s*(?=\S)/g,'$1\n\n');
 const preview=formatted.split(/\n\s*\n/).slice(0,3).join('\n\n');
 const chars=Array.from(preview);
 return chars.length>600?chars.slice(0,600).join('')+'…':preview;
}

export function replyLinks(message:Message):string[] {
 const source=messageText(message)+'\n'+summarySource(message);
 const links=new Set<string>();
 for(const match of source.matchAll(/https?:\/\/[^\s<>"'`，。；！？、）】》]+/gi)){
  const value=match[0].replace(/[)\]}>.,;，。；！？、）】》]+$/g,'');
  try{const url=new URL(value);if(['http:','https:'].includes(url.protocol)&&!url.username&&!url.password)links.add(value)}catch{/* Ignore incomplete streamed URLs. */}
  if(links.size===6)break;
 }
 return [...links];
}
