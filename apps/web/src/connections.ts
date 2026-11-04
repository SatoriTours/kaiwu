export interface PiConnection {
 id: string; revision: number; name: string; provider: string; api: string;
 baseUrl: string; models: string[]; kind: string; hasKey: boolean;
}
export interface ConnectionDraft {
 id: string; revision: number; name: string; api: string; baseUrl: string;
 key: string; models: string[]; hasKey: boolean;
}
export interface ConnectionModel { id: string; name: string; manual?: boolean }
export interface OAuthGrant { id: string; url: string; provider: string; expires: number }
export const protocolOptions = [
 {value:'openai-completions',label:'OpenAI 兼容 · Chat Completions'},
 {value:'openai-responses',label:'OpenAI 兼容 · Responses'},
 {value:'anthropic-messages',label:'Anthropic 兼容'},
 {value:'google-generative-ai',label:'Google Gemini 兼容'},
];
export function connectionDraft(c?:PiConnection):ConnectionDraft {
 return {id:c?.id??'',revision:c?.revision??0,name:c?.name??'',api:c?.api||'openai-completions',
 baseUrl:c?.baseUrl??'',key:'',models:[...(c?.models??[])],hasKey:!!c?.hasKey};
}
export function cleanModelID(value:string):string|undefined {
 const id=value.trim();
 return id && id.length<=300 && !/[\u0000-\u001f\u007f]/.test(id) && !id.startsWith('!') ? id : undefined;
}
export function mergeDiscoveredModels(previous:ConnectionModel[],fresh:ConnectionModel[],selected:string[]):ConnectionModel[] {
 const map=new Map<string,ConnectionModel>();
 for(const m of fresh){const id=cleanModelID(m.id);if(id&&!map.has(id))map.set(id,{id,name:m.name||id});}
 for(const id of selected)if(!map.has(id))map.set(id,{id,name:previous.find(m=>m.id===id)?.name??id,manual:true});
 return [...map.values()];
}
export function credentialChanged(before:ConnectionDraft,after:ConnectionDraft):boolean {
 return before.api!==after.api || before.baseUrl.trim().replace(/\/+$/,'')!==after.baseUrl.trim().replace(/\/+$/,'');
}
