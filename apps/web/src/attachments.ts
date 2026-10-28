import type {Attachment} from './types.ts';
import type {LocalState} from './workbench.ts';
export function addDraftAttachments(local:LocalState,id:string,items:Attachment[]){
 const draft=local.drafts[id]??={text:'',revision:0};
 const next=[...(draft.attachments??[]),...items];
 if(next.length>8)throw new Error('每条消息最多 8 个附件');
 if(next.reduce((sum,a)=>sum+a.size,0)>24*1024*1024)throw new Error('附件合计不能超过 24 MiB');
 draft.attachments=next;draft.revision++;
}
export function removeDraftAttachment(local:LocalState,id:string,itemID:string){
 const draft=local.drafts[id];if(!draft)return;
 draft.attachments=(draft.attachments??[]).filter(a=>a.id!==itemID);draft.revision++;
}
export function attachmentURL(conversationId:string,id:string,inline=false){
 return '/api/attachments/'+encodeURIComponent(id)+'?conversationId='+encodeURIComponent(conversationId)+(inline?'&inline=1':'');
}
export async function uploadAttachment(conversationId:string,file:File):Promise<Attachment>{
 if(file.size>8*1024*1024)throw new Error('单个附件不能超过 8 MiB');
 const form=new FormData();form.append('file',file);
 const response=await fetch('/api/attachments?conversationId='+encodeURIComponent(conversationId),{method:'POST',credentials:'same-origin',headers:{'X-Kaiwu-Client':'web'},body:form});
 const result=await response.json();if(!response.ok)throw new Error(result.error?.message??'附件上传失败');return result;
}
