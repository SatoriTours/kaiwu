<script setup lang="ts">
import {ref,computed,watch,onBeforeUnmount,nextTick} from 'vue';
import {copyText} from './clipboard.ts';
import CustomSelect from './CustomSelect.vue';
import TerminalSurface from './TerminalSurface.vue';
import TerminalToolbar from './TerminalToolbar.vue';
import {terminalStorage,readTerminalView,writeTerminalView,chooseTerminal,readPendingTerminal,writePendingTerminal,type TerminalItem} from './terminal-state.ts';
import {api,httpAPI} from './api.ts';
import type {Project,Task,Execution,Attachment} from './types.ts';
const props=defineProps<{view:'files'|'terminal';project:Project;tasks:Task[];executions:Record<string,Execution>;conversationId:string;online:boolean}>();
const emit=defineEmits<{attach:[{conversationId:string;attachment:Attachment}]}>();
const savedTerminal=readTerminalView(terminalStorage,props.project.id);
const taskId=ref(savedTerminal.taskId),folder=ref('.'),filter=ref(''),entries=ref<{name:string;directory:boolean;size:number;modifiedAt:string}[]>([]),error=ref(''),busy=ref(false),fileInput=ref<HTMLInputElement>(),selected=ref(''),preview=ref(''),previewImage=ref(''),root=ref('');
const viewer=ref<HTMLDialogElement>(),copied=ref(false),imageKind=ref(''),imageDimensions=ref('');
const selectedEntry=computed(()=>entries.value.find(e=>e.name===selected.value));
const terminals=ref<TerminalItem[]>([]),terminalId=ref(savedTerminal.terminalId),running=ref(false),connected=ref(false),truncated=ref(false),terminal=ref<InstanceType<typeof TerminalSurface>>();
const terminalGeneration=ref(0),screenReader=ref(false),explicitlyClosed=ref(savedTerminal.closed),endDialog=ref<HTMLDialogElement>(),endTarget=ref('');
function remember(){const value={taskId:taskId.value,terminalId:terminalId.value,closed:explicitlyClosed.value};writeTerminalView(terminalStorage,props.project.id,value);writeTerminalView(terminalStorage,props.project.id+'/'+taskId.value,value)}
let epoch=0,socket:WebSocket|undefined,disposed=false;
const availableTasks=computed(()=>props.tasks.filter(t=>t.executionId&&props.executions[t.executionId]?.workspace));
const scope=computed(()=>({projectId:props.project.id,taskId:taskId.value}));
const scopeOptions=computed(()=>[{value:'',label:'项目主目录'},...availableTasks.value.map(t=>({value:t.id,label:t.title}))]);
const visibleEntries=computed(()=>entries.value.filter(e=>e.name.toLowerCase().includes(filter.value.toLowerCase())));
function query(extra:Record<string,string>={}){
 return new URLSearchParams({...scope.value,...extra}).toString();
}
function disconnect(){socket?.close();socket=undefined;connected.value=false}
function cleanPreview(){viewer.value?.close();copied.value=false;preview.value='';previewImage.value='';imageKind.value='';imageDimensions.value=''}
async function load(allowCreate=true){
 if(!props.online)return;
 if(taskId.value&&!availableTasks.value.some(t=>t.id===taskId.value)){taskId.value='';return}
 const token=++epoch;terminalGeneration.value++;disconnect();error.value='';selected.value='';cleanPreview();
 try{
  if(props.view==='files'){
   const result=await httpAPI<{entries:typeof entries.value;root:string}>('tools/files?'+query({path:folder.value}));
   if(token===epoch){entries.value=result.entries;root.value=result.root}
  }else{
   const result=await api<typeof terminals.value>('tools/terminal/list',scope.value);
   if(token!==epoch)return;
   const pending=readPendingTerminal(terminalStorage,props.project.id,taskId.value);
   if(pending){
    const resolution=await api<{known:boolean;terminal?:TerminalItem}>('tools/terminal/resolve',{...scope.value,operationId:pending});
    if(token!==epoch)return;
    if(resolution.known){
     writePendingTerminal(terminalStorage,props.project.id,taskId.value,'');
     // Inventory may predate the launch that Lookup waited for. Reconcile its
     // authoritative retained receipt, without issuing another start request.
     const receipt=resolution.terminal;
     if(receipt&&receipt.number>0&&!result.some(t=>t.id===receipt.id))result.push(receipt);
    }
    else error.value='上次新建结果仍未确认；再次点击新建将显式核查原操作，不会重复创建。';
   }
   // A natural exit remains a readable selected result; only an explicit close
   // removes that tab. Never silently switch to a sibling just because bash exited.
   terminals.value=result.filter(t=>t.running||t.id===terminalId.value&&!explicitlyClosed.value);
   if(!result.length&&allowCreate&&!terminalId.value&&!explicitlyClosed.value&&!pending){await start(true);return}
   const active=chooseTerminal(terminals.value,terminalId.value);
   if(!active&&terminalId.value)explicitlyClosed.value=true;
   terminalId.value=active?.id??'';running.value=!!active?.running;truncated.value=false;remember();
   await nextTick();if(token!==epoch)return;terminal.value?.reset();
   if(active)connect(active.id,token);
  }
 }catch(e){if(token===epoch)error.value=(e as Error).message}
}
function connect(id:string,token:number){
 disconnect();
 const ws=new WebSocket((location.protocol==='https:'?'wss://':'ws://')+location.host+'/api/tools/terminal/socket?'+query({id,v:'2'}));socket=ws;
 ws.onmessage=e=>{
  if(token!==epoch||socket!==ws)return;
  try{
   const data=JSON.parse(e.data);
   if(typeof data.data!=='string'||!Number.isSafeInteger(data.sequence)||typeof data.reset!=='boolean'||typeof data.running!=='boolean'||!Number.isInteger(data.cols)||!Number.isInteger(data.rows))throw new Error('invalid frame');
   running.value=data.running;const item=terminals.value.find(t=>t.id===id);if(item)item.running=data.running;if(data.reset)truncated.value=!!data.truncated;
   const bytes=Uint8Array.from(atob(data.data),c=>c.charCodeAt(0));
   terminal.value?.write(bytes,data.reset,data.cols,data.rows,()=>{
    if(token!==epoch||socket!==ws||ws.readyState!==WebSocket.OPEN)return;
    // Only advertise input readiness after the initial screen has been restored.
    connected.value=true;
    ws.send(JSON.stringify({ack:data.sequence}));
    if(data.reset)terminal.value?.resize();
   });
  }catch{error.value='终端响应格式无效，请接回';disconnect()}
 };
 ws.onclose=()=>{if(token===epoch&&socket===ws)connected.value=false};
 ws.onerror=()=>{if(token===epoch)error.value='终端连接中断；Shell 仍由服务器保留，可点击接回'}
}
async function guarded(work:()=>Promise<void>){
 if(busy.value)return;busy.value=true;error.value='';
 try{await work()}catch(e){error.value=(e as Error).message}finally{busy.value=false}
}
function enter(name:string){folder.value=folder.value==='.'?name:folder.value+'/'+name;load()}
function parent(){const parts=folder.value.split('/');parts.pop();folder.value=parts.join('/')||'.';load()}
async function select(name:string){
 cleanPreview();selected.value=name;
 const token=epoch,path=folder.value==='.'?name:folder.value+'/'+name;
 try{
  const response=await fetch('/api/tools/file?'+query({path}),{credentials:'same-origin'});
  if(!response.ok){const data=await response.json();throw new Error(data.error?.message??'读取失败')}
  const blob=await response.blob();if(token!==epoch||selected.value!==name)return;
  const bytes=new Uint8Array(await blob.slice(0,12).arrayBuffer());if(token!==epoch||selected.value!==name)return;
  // Raster only: never execute HTML/SVG or interpolate source into the DOM.
  if((bytes[0]===137&&bytes[1]===80&&bytes[2]===78&&bytes[3]===71)||(bytes[0]===255&&bytes[1]===216)||(bytes[0]===71&&bytes[1]===73&&bytes[2]===70)||(bytes[0]===82&&bytes[1]===73&&bytes[8]===87&&bytes[9]===69)){
   const mime=bytes[0]===137?'image/png':bytes[0]===255?'image/jpeg':bytes[0]===71?'image/gif':'image/webp';
   // The workbench CSP permits data: raster images, not blob: URLs. Keep the
   // existing policy rather than relaxing it for file previews.
   const data=await new Promise<string>((resolve,reject)=>{const reader=new FileReader();reader.onload=()=>resolve(String(reader.result));reader.onerror=()=>reject(new Error('图片读取失败'));reader.readAsDataURL(new Blob([blob],{type:mime}))});
   if(token!==epoch||selected.value!==name)return;
   imageKind.value=mime.slice(6).toUpperCase();previewImage.value=data;
  }else preview.value='此文件可下载或添加到聊天。文件管理不提供源码编辑器。';
 }catch(e){if(token===epoch)error.value=(e as Error).message}
}
function imageLoaded(event:Event){const img=event.target as HTMLImageElement;if(img.currentSrc===previewImage.value)imageDimensions.value=img.naturalWidth+' × '+img.naturalHeight}
function imageFailed(event:Event){if((event.target as HTMLImageElement).currentSrc!==previewImage.value)return;cleanPreview();preview.value='图片无法解码，可下载原文件或更换图片。'}
function selectedPath(){return folder.value==='.'?selected.value:folder.value+'/'+selected.value}
async function attach(path=selectedPath()){
 const destination=props.conversationId,token=epoch,payload={...scope.value,path,conversationId:destination};
 await guarded(async()=>{const attachment=await api<Attachment>('tools/attach',payload);if(token===epoch)emit('attach',{conversationId:destination,attachment})})
}
async function mkdir(){
 const name=window.prompt('新建文件夹名称');if(!name)return;
 if(/[\/\\]/.test(name)||name==='.'||name==='..'){error.value='请输入单个文件夹名称';return}
 const payload={...scope.value,path:folder.value==='.'?name:folder.value+'/'+name},token=epoch;
 await guarded(async()=>{await api('tools/mkdir',payload);if(token===epoch)await load()})
}
async function upload(files:FileList|null){
 const file=files?.[0];if(!file)return;if(file.size>8*1024*1024){error.value='单个文件不能超过 8 MiB';return}
 const token=epoch,url='/api/tools/upload?'+query({path:folder.value});
 await guarded(async()=>{const body=new FormData();body.append('file',file);const response=await fetch(url,{method:'POST',credentials:'same-origin',headers:{'X-Kaiwu-Client':'web'},body});if(!response.ok){const data=await response.json();throw new Error(data.error?.message??'上传失败')}if(token===epoch)await load()});
 if(fileInput.value)fileInput.value.value='';
}
async function start(ensure=false){
 const target={...scope.value},token=epoch;
 const operationId=readPendingTerminal(terminalStorage,target.projectId,target.taskId)||crypto.randomUUID();
 await guarded(async()=>{
  writePendingTerminal(terminalStorage,target.projectId,target.taskId,operationId);
  try{
   const t=await api<TerminalItem>('tools/terminal/start',{...target,operationId,ensure});
   writePendingTerminal(terminalStorage,target.projectId,target.taskId,'');
   if(token===epoch){terminalId.value=t.id;explicitlyClosed.value=false;remember();await load(false)}
   else if(props.view==='terminal'&&target.projectId===props.project.id&&target.taskId===taskId.value){await load(false)}
  }catch(e){
   const status=(e as {status?:number}).status;
   if(status&&status>=400&&status<500)writePendingTerminal(terminalStorage,target.projectId,target.taskId,'');
   throw e;
  }
 })
}
async function selectTerminal(id:string){
 if(busy.value||!terminals.value.some(t=>t.id===id))return;
 terminalId.value=id;explicitlyClosed.value=false;remember();await load(false)
}
function requestStop(id:string){endTarget.value=id;endDialog.value?.showModal()}
async function stop(){
 const payload={...scope.value,id:endTarget.value},token=epoch;
 endDialog.value?.close();
 await guarded(async()=>{await api('tools/terminal/stop',payload);if(token===epoch){
  if(terminalId.value===payload.id)terminalId.value='';
  explicitlyClosed.value=!terminals.value.some(t=>t.id!==payload.id&&t.running);
  remember();await load(false)
 }})
}
function input(value:string){
 if(!connected.value||!running.value||socket?.readyState!==WebSocket.OPEN)return;
 if(new TextEncoder().encode(value).length>4096){error.value='本次输入超过 4096 字节，未发送，请分段粘贴';return}
 if(socket.bufferedAmount>65536){error.value='终端发送拥堵，本次输入未发送';return}
 error.value='';socket.send(JSON.stringify({input:value}));
}
function resize(size:{cols:number;rows:number}){if(connected.value&&running.value&&socket?.readyState===WebSocket.OPEN)socket.send(JSON.stringify({resize:size}))}
async function copyPath(path=selectedPath()){const token=epoch;try{await copyText('./'+path);if(token===epoch)copied.value=true}catch{if(token===epoch)error.value='无法复制，请手动复制相对路径：./'+path}}
function filePath(name:string){return folder.value==='.'?name:folder.value+'/'+name}
function date(value?:string){return value?new Date(value).toLocaleString():'—'}
watch(()=>[props.project.id,taskId.value],()=>{folder.value='.';const saved=readTerminalView(terminalStorage,props.project.id+'/'+taskId.value);terminalId.value=saved.terminalId;explicitlyClosed.value=saved.closed;load()});
watch(()=>props.view,()=>load(),{immediate:true});
watch(()=>props.online,value=>{if(value&&!disposed)load();else disconnect()});
onBeforeUnmount(()=>{disposed=true;++epoch;disconnect();cleanPreview()});
</script>
<template><main class="project-tools" :class="{'terminal-main':view==='terminal'}" :aria-label="view==='files'?'项目文件':'项目终端'">
 <Teleport v-if="view==='terminal'" to="#terminal-toolbar-host"><TerminalToolbar :terminals="terminals" :active-id="terminalId" :task-id="taskId" :options="scopeOptions" :busy="busy" :online="online" :connected="connected" :running="running" :screen-reader="screenReader" :truncated="truncated" @select="selectTerminal" @scope="taskId=$event" @start="start(false)" @reconnect="load(false)" @stop="requestStop" @reader="screenReader=!screenReader" /></Teleport>
 <div v-if="view==='files'" class="tools-bar">
  <div class="tool-scope" :class="{'project-scope':!taskId}" :title="root||project.path"><span aria-hidden="true">▱</span><CustomSelect v-model="taskId" :options="scopeOptions" aria-label="工作区" /></div>
  <template v-if="view==='files'">
   <button :disabled="folder==='.'" aria-label="上一级目录" @click="parent">‹</button><span class="file-breadcrumb" :title="folder">{{folder==='.'?project.name:folder}}</span>
   <input v-model="filter" class="file-filter" aria-label="筛选文件" placeholder="筛选文件" />
   <button aria-label="刷新文件" title="刷新" @click="load()">↻</button>
   <button :disabled="busy||!online" aria-label="上传文件到项目" title="上传文件（不覆盖同名文件）" @click="fileInput?.click()">↑<span class="tool-action-label"> 上传</span></button>
   <button :disabled="busy||!online" aria-label="新建文件夹" @click="mkdir">＋<span class="tool-action-label"> 文件夹</span></button>
   <input ref="fileInput" type="file" hidden @change="upload(($event.target as HTMLInputElement).files)" />
  </template>

 </div>
 <p v-if="error" class="tools-error" role="alert">{{error}}</p>
 <div v-if="view==='files'" class="files-content">
  <div class="files-list"><div class="file-table-head"><span>名称</span><span>大小</span><span class="file-modified">修改时间</span><span></span></div>
   <div v-for="entry in visibleEntries" :key="entry.name" class="file-entry-row" :class="{selected:entry.name===selected}">
    <button class="file-entry" @click="entry.directory?enter(entry.name):select(entry.name)"><span><span aria-hidden="true">{{entry.directory?'▱':'▤'}}</span> {{entry.name}}</span><small>{{entry.directory?'—':Math.max(1,Math.ceil(entry.size/1024))+' KB'}}</small><small class="file-modified">{{date(entry.modifiedAt)}}</small></button>
    <details class="file-row-menu"><summary :aria-label="'文件操作 '+entry.name" title="文件操作">···</summary><div><button @click="copyPath(filePath(entry.name))">复制路径</button><template v-if="!entry.directory"><a :href="'/api/tools/file?'+query({path:filePath(entry.name)})">下载文件</a><button :disabled="busy||!online" @click="attach(filePath(entry.name))">添加到聊天</button></template></div></details>
   </div>
   <p v-if="!visibleEntries.length" class="tools-empty">没有文件</p>
  </div>
  <aside v-if="selected" class="file-detail"><button class="file-detail-close" aria-label="关闭文件预览" @click="selected='';cleanPreview()">×</button><strong>{{selected}}</strong><button v-if="previewImage" class="file-preview-button" aria-label="放大文件图片" @click="viewer?.showModal()"><img :src="previewImage" :alt="selected" @load="imageLoaded" @error="imageFailed" /><span>放大图片 ↗</span></button><p v-else>{{preview||'正在读取…'}}</p><dl class="file-metadata"><dt>类型</dt><dd>{{previewImage?imageKind+' 图片'+(imageDimensions?' · '+imageDimensions:''):'文件'}}</dd><dt>大小</dt><dd>{{selectedEntry?.size??0}} 字节</dd><dt>位置</dt><dd>./{{selectedPath()}}</dd><dt>修改时间</dt><dd>{{date(selectedEntry?.modifiedAt)}}</dd></dl><div class="file-detail-actions"><a :href="'/api/tools/file?'+query({path:selectedPath()})">下载文件 ↓</a><button class="file-to-chat" :disabled="busy||!online" @click="attach()">添加到当前聊天</button><button @click="copyPath()">复制路径</button></div><p v-if="copied" class="copy-status" role="status">已复制相对路径</p><p>作为待发送附件添加，不会自动发送或启动 AI。</p></aside>
 </div>
 <div v-else class="terminal-content">
  <div class="terminal-viewport">
   <TerminalSurface :key="terminalGeneration" ref="terminal" :enabled="connected&&running" :screen-reader="screenReader" @input="input" @resize="resize" />
   <div v-if="!terminalId" class="terminal-empty"><p>{{busy?'正在打开终端…':'原终端已结束或不可接回，不会重发旧命令。'}}</p><button v-if="!busy" :disabled="!online" aria-label="重新打开终端" @click="start(false)">重新打开终端</button></div>
  </div>

 </div>
<dialog ref="endDialog" class="terminal-end-dialog" aria-label="结束终端"><h2>结束终端？</h2><p>此终端及其中的命令将结束，其他终端不受影响。</p><div><button @click="endDialog?.close()">取消</button><button class="terminal-confirm-stop" :disabled="busy||!online" @click="stop">结束终端</button></div></dialog>
<dialog ref="viewer" class="file-image-viewer" aria-label="项目图片预览" @click="($event.target===viewer)&&viewer?.close()"><header><strong>{{selected}}</strong><button aria-label="关闭项目图片预览" @click="viewer?.close()">×</button></header><img v-if="previewImage" :src="previewImage" :alt="selected" /></dialog></main></template>
<style scoped>
.project-tools{flex:1;min-width:0;overflow:auto;padding:16px 24px;display:flex;flex-direction:column}
.tools-bar{display:flex;align-items:center;gap:7px;min-height:46px;padding:7px 12px;border:1px solid var(--border);border-radius:10px 10px 0 0;background:var(--theme-surface,#fff)}
.tool-scope{display:flex;align-items:center;gap:3px;min-width:0}.tool-scope select{border:0;background:transparent;padding:4px;font-size:11px;width:auto;max-width:135px}
.tool-scope :deep(.custom-select-trigger){border:0;background:transparent;padding:3px;font-size:11px;max-width:145px}.project-scope :deep(.custom-select-label){display:none}.project-scope :deep(.custom-select-trigger){width:20px;min-width:20px}.tool-scope :deep(.custom-select-dropdown){min-width:180px}
.file-breadcrumb{font-size:12px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;min-width:25px;max-width:260px}
.file-filter{margin-left:auto;width:140px;padding:4px 7px;font-size:11px}.tools-bar button{border:0;background:transparent;font-size:11px;padding:3px 6px;white-space:nowrap}
.tools-spacer{flex:1}.files-content{display:flex;flex:1;min-height:300px;border:1px solid var(--border);border-top:0;border-radius:0 0 10px 10px;overflow:auto}
.files-list{flex:1;min-width:0}.file-table-head,.file-entry{width:100%;display:flex;justify-content:space-between;gap:10px;padding:12px 18px;border-radius:0;border:0;border-bottom:1px solid var(--border);text-align:left}
.file-table-head{font-size:10px;color:var(--muted)}.file-entry>span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.file-entry small{font-size:10px;white-space:nowrap;color:var(--muted)}
.file-entry.selected{background:var(--theme-inset,#eef3ed)}.tools-empty{padding:30px;color:var(--muted);text-align:center}
.file-detail{position:relative;flex:0 0 290px;max-width:45%;padding:24px 16px;border-left:1px solid var(--border);overflow-wrap:anywhere}.file-detail img{display:block;width:100%;max-height:330px;object-fit:contain;margin:24px 0}.file-detail-close{float:right;border:0}.file-detail strong{font-size:12px}.file-detail p{color:var(--muted);font-size:11px;margin:24px 0}.file-detail-actions{display:flex;gap:8px;flex-wrap:wrap;font-size:11px}.file-detail-actions a{color:var(--blue);padding:5px 0}
.tools-error{color:var(--theme-danger,#9b4b44);font-size:12px}
.project-tools.terminal-main{padding:0;margin:0;min-height:0;overflow:hidden;background:var(--terminal-bg,#fafbfa)}
.terminal-content{display:flex;flex-direction:column;flex:1;min-height:0;overflow:hidden;color:var(--ink);background:var(--terminal-bg,#fafbfa)}
.terminal-viewport{position:relative;flex:1;min-height:0;min-width:0;overflow:hidden}
.terminal-empty{position:absolute;inset:0;display:flex;flex-direction:column;align-items:center;justify-content:center;gap:12px;font-size:12px;color:var(--muted);background:var(--terminal-bg,#fafbfa);margin:0}
.terminal-end-dialog{width:min(360px,calc(100vw - 32px));border:1px solid var(--border);border-radius:10px;background:var(--theme-surface,#fff);color:var(--ink);padding:22px}
.terminal-end-dialog::backdrop{background:var(--theme-backdrop,#16231955)}.terminal-end-dialog h2{font-size:15px}.terminal-end-dialog p{font-size:12px;color:var(--muted);margin:12px 0 20px}.terminal-end-dialog>div{display:flex;gap:8px;justify-content:flex-end}.terminal-confirm-stop{color:var(--theme-danger,#a4544d)}
@media(max-width:650px){.project-tools{padding:13px}.tools-bar{padding:6px;gap:3px}.tool-scope select{max-width:103px;font-size:10px}.file-filter{width:75px;min-width:50px}.file-breadcrumb{max-width:90px;font-size:10px}.tool-action-label{display:none}.files-content{position:relative}.file-detail{position:absolute;inset:0;max-width:none;background:var(--theme-surface,#fff);border:0;z-index:2}.terminal-status .terminal-help{display:none}.terminal-notice{font-size:9px}}
</style>

<style scoped>
.file-entry-row{display:flex;border-bottom:1px solid var(--border);position:relative}.file-entry-row.selected{background:var(--theme-inset,#eef3ed)}.file-entry{flex:1;min-width:0;background:transparent!important;border-bottom:0;display:grid;grid-template-columns:minmax(0,1fr) 65px 145px}.file-table-head{display:grid;grid-template-columns:minmax(0,1fr) 65px 145px 30px}.file-modified{font-size:10px;color:var(--muted)}
.file-row-menu{flex:0 0 30px;align-self:center;font-size:12px}.file-row-menu summary{cursor:pointer;list-style:none}.file-row-menu>div{position:absolute;right:5px;top:34px;z-index:4;display:grid;min-width:120px;padding:6px;background:var(--theme-surface,#fff);border:1px solid var(--border);border-radius:8px;box-shadow:0 4px 12px #0001}.file-row-menu a,.file-row-menu button{text-align:left;padding:7px;font-size:11px;border:0;color:var(--ink);text-decoration:none;background:transparent}
.file-preview-button{display:block;width:100%;padding:0;background:transparent;border:0;cursor:zoom-in}.file-metadata{display:grid;grid-template-columns:60px minmax(0,1fr);gap:9px;font-size:10px;margin:20px 0;color:var(--muted)}.file-metadata dd{margin:0;color:var(--ink)}
.file-image-viewer{max-width:95vw;max-height:95dvh;padding:16px;border:1px solid var(--border);border-radius:12px;background:var(--theme-surface,#fff);color:var(--ink)}.file-image-viewer::backdrop{background:#101a1680}.file-image-viewer header{display:flex;align-items:center;gap:20px;margin-bottom:12px}.file-image-viewer header strong{flex:1;font-size:12px}.file-image-viewer>img{display:block;max-width:85vw;max-height:75dvh;object-fit:contain}
@media(max-width:900px){.file-modified{display:none}.file-entry{grid-template-columns:minmax(0,1fr) 50px}.file-table-head{grid-template-columns:minmax(0,1fr) 50px 30px}}
</style>

<style scoped>.file-preview-button>span{display:block;color:var(--blue);font-size:11px;margin:-14px 0 16px}.file-to-chat{background:var(--blue);color:#fff;border-color:var(--blue)}</style>
