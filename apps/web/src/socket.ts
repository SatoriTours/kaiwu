import {SnapshotAssembler} from './snapshot.ts';
import {APIError} from './api.ts';
import {createOperationID} from './workbench.ts';
export class SocketClient {
 socket:WebSocket;
 pending=new Map<string,{resolve:(v:any)=>void;reject:(e:Error)=>void;timer:ReturnType<typeof setTimeout>}>();
 constructor(url:string,onEvent:(event:any)=>void,onOpen:()=>void,onClose:(event:CloseEvent)=>void){
  const socket=this.socket=new WebSocket(url);
  socket.onopen=onOpen;
  const snapshots=new SnapshotAssembler();
  socket.onmessage=message=>{
   let v:any;try{v=JSON.parse(message.data)}catch{socket.close();return}
   if(v.type!=='reply'){try{const event=snapshots.accept(v);if(event)onEvent(event)}catch{socket.close()}return}
   const request=this.pending.get(v.id);if(!request)return;clearTimeout(request.timer);this.pending.delete(v.id);
   if(v.status>=200&&v.status<300)request.resolve(v.data);
   else request.reject(new APIError(v.status,v.data?.error?.code??'socket_error',v.data?.error?.message??'请求未完成'));
  };
  socket.onclose=event=>{this.fail();onClose(event)};socket.onerror=()=>socket.close();
 }
 get open(){return this.socket.readyState===WebSocket.OPEN}
 request<T>(method:string,body?:unknown):Promise<T>{
  if(!this.open)return Promise.reject(new APIError(0,'offline','连接中断，原输入与操作标识已保留'));
  if(this.pending.size>=32)return Promise.reject(new APIError(409,'busy','实时请求繁忙，请稍后重试'));
  const id=createOperationID();
  return new Promise<T>((resolve,reject)=>{
   const timer=setTimeout(()=>{this.pending.delete(id);reject(new APIError(0,'unknown_result','未收到操作回执，请以原操作标识查询确认'))},30000);
   this.pending.set(id,{resolve,reject,timer});
   try{this.socket.send(JSON.stringify({id,method,body:body??{}}))}catch{clearTimeout(timer);this.pending.delete(id);reject(new APIError(0,'unknown_result','发送结果未知，请查询原操作回执'))}
  });
 }
 fail(){for(const p of this.pending.values()){clearTimeout(p.timer);p.reject(new APIError(0,'unknown_result','连接中断，操作结果待确认；不会自动重发'))}this.pending.clear()}
 close(){this.fail();this.socket.close()}
}
