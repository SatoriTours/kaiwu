<script setup lang="ts">
import {ref,computed,watch,onBeforeUnmount,nextTick} from 'vue';
import {copyText} from './clipboard.ts';
import CustomSelect from './CustomSelect.vue';
import {api,httpAPI} from './api.ts';
import type {Project,Task,Execution,Attachment} from './types.ts';
const props=defineProps<{view:'files'|'terminal';project:Project;tasks:Task[];executions:Record<string,Execution>;conversationId:string;online:boolean}>();
const emit=defineEmits<{attach:[{conversationId:string;attachment:Attachment}]}>();
const taskId=ref(''),folder=ref('.'),filter=ref(''),entries=ref<{name:string;directory:boolean;size:number;modifiedAt:string}[]>([]),error=ref(''),busy=ref(false),fileInput=ref<HTMLInputElement>(),selected=ref(''),preview=ref(''),previewImage=ref(''),root=ref('');
const viewer=ref<HTMLDialogElement>(),copied=ref(false),imageKind=ref(''),imageDimensions=ref('');
const selectedEntry=computed(()=>entries.value.find(e=>e.name===selected.value));
const terminals=ref<{id:string;running:boolean;output:string}[]>([]),terminalId=ref(''),output=ref(''),running=ref(false),connected=ref(false),command=ref(''),log=ref<HTMLElement>();
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
async function load(){
 const token=++epoch;disconnect();error.value='';selected.value='';cleanPreview();
 try{
  if(props.view==='files'){
   const result=await httpAPI<{entries:typeof entries.value;root:string}>('tools/files?'+query({path:folder.value}));
   if(token===epoch){entries.value=result.entries;root.value=result.root}
  }else{
   const result=await api<typeof terminals.value>('tools/terminal/list',scope.value);
   if(token!==epoch)return;
   terminals.value=result;
   const active=result.find(t=>t.id===terminalId.value)??result.find(t=>t.running)??result[0];
   terminalId.value=active?.id??'';output.value='';running.value=!!active?.running;
   if(active)connect(active.id,token);
  }
 }catch(e){if(token===epoch)error.value=(e as Error).message}
}
function connect(id:string,token:number){
 disconnect();
 const ws=new WebSocket((location.protocol==='https:'?'wss://':'ws://')+location.host+'/api/tools/terminal/socket?'+query({id}));socket=ws;
 ws.onopen=()=>{if(token===epoch)connected.value=true};
 ws.onmessage=async e=>{
  if(token!==epoch||socket!==ws)return;
  try{const data=JSON.parse(e.data);output.value=(data.reset?String(data.output):output.value+String(data.output)).slice(-262144).replace(/\x1b\[[0-9;?]*[ -/]*[@-~]/g,'').replace(/\x1b\][^\x07]*(?:\x07|\x1b\\)/g,'');running.value=!!data.running;await nextTick();if(log.value)log.value.scrollTop=log.value.scrollHeight}catch{error.value='终端响应格式无效'}
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
async function start(){
 const payload={...scope.value},token=epoch;
 await guarded(async()=>{const t=await api<{id:string}>('tools/terminal/start',payload);if(token===epoch){terminalId.value=t.id;await load()}})
}
async function stop(){
 const payload={...scope.value,id:terminalId.value},token=epoch;
 await guarded(async()=>{await api('tools/terminal/stop',payload);if(token===epoch)await load()})
}
function input(value:string){if(!connected.value||!running.value||!socket)return false;if(new TextEncoder().encode(value).length>4096){error.value='终端单次输入不能超过 4096 字节，内容已保留';return false}error.value='';socket.send(JSON.stringify({input:value}));return true}
function submit(){if(!connected.value||!running.value)return;if(input(command.value+'\n'))command.value=''}
async function copyPath(path=selectedPath()){const token=epoch;try{await copyText('./'+path);if(token===epoch)copied.value=true}catch{if(token===epoch)error.value='无法复制，请手动复制相对路径：./'+path}}
function filePath(name:string){return folder.value==='.'?name:folder.value+'/'+name}
function date(value?:string){return value?new Date(value).toLocaleString():'—'}
watch(()=>[props.project.id,taskId.value],()=>{folder.value='.';terminalId.value='';load()});
watch(()=>props.view,()=>load(),{immediate:true});
watch(()=>props.online,value=>{if(value&&!disposed)load();else disconnect()});
onBeforeUnmount(()=>{disposed=true;++epoch;disconnect();cleanPreview()});
</script>
<template><main class="project-tools" :aria-label="view==='files'?'项目文件':'项目终端'">
 <div class="tools-bar" :class="{terminal:view==='terminal'}">
  <div class="tool-scope" :class="{'project-scope':!taskId}" :title="root||project.path"><span aria-hidden="true">▱</span><CustomSelect v-model="taskId" :options="scopeOptions" aria-label="工作区" /></div>
  <template v-if="view==='files'">
   <button :disabled="folder==='.'" aria-label="上一级目录" @click="parent">‹</button><span class="file-breadcrumb" :title="folder">{{folder==='.'?project.name:folder}}</span>
   <input v-model="filter" class="file-filter" aria-label="筛选文件" placeholder="筛选文件" />
   <button aria-label="刷新文件" title="刷新" @click="load">↻</button>
   <button :disabled="busy||!online" aria-label="上传文件到项目" title="上传文件（不覆盖同名文件）" @click="fileInput?.click()">↑<span class="tool-action-label"> 上传</span></button>
   <button :disabled="busy||!online" aria-label="新建文件夹" @click="mkdir">＋<span class="tool-action-label"> 文件夹</span></button>
   <input ref="fileInput" type="file" hidden @change="upload(($event.target as HTMLInputElement).files)" />
  </template>
  <template v-else>
   <span class="terminal-tab">bash<span v-if="terminalId"> · {{terminalId.slice(0,6)}}</span></span><span class="tools-spacer"></span>
   <button :disabled="busy||!online" @click="load">{{connected?'刷新':'接回'}}</button>
   <button v-if="running" :disabled="busy||!online" @click="stop">结束终端</button>
   <button v-else :disabled="busy||!online" @click="start">＋ 新建终端</button>
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
  <p class="terminal-notice">服务器账号权限 · 不是沙箱 · 关闭页面不会结束终端。人工终端与同工作区 AI／合并互斥。仅提供命令行交互，不支持全屏编辑器。</p>
  <pre ref="log" class="terminal-log" aria-label="终端输出">{{output||'点击“新建终端”显式启动服务器 Shell。'}}</pre>
  <form class="terminal-input" @submit.prevent="submit"><span>$</span><input v-model="command" aria-label="终端命令" autocomplete="off" spellcheck="false" :disabled="!connected||!running" @keydown.ctrl.c.prevent="input('\x03')" /><button :disabled="!connected||!running">执行 ↵</button><button type="button" :disabled="!connected||!running" @click="input('\x03')">Ctrl+C</button></form>
  <div class="terminal-status">{{running?(connected?'● 已连接':'○ 已断开，可接回原终端'):'○ 未运行'}}<span>输出保留最近 256 KiB · 服务重启后需新建</span></div>
 </div>
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
.tools-error{color:var(--theme-danger,#9b4b44);font-size:12px}.terminal{background:#213129;color:#d6e1d3;border-color:#3d4e41}.terminal select,.terminal button{color:inherit}.terminal option{background:#213129}
.terminal-content{display:flex;flex-direction:column;flex:1;background:#1c2922;color:#cfdbca;border-radius:0 0 10px 10px;overflow:hidden;min-height:380px}
.terminal-notice{font-size:10px;color:#96aa97;padding:9px 16px;margin:0;border-bottom:1px solid #344438}.terminal-log{white-space:pre-wrap;overflow-wrap:anywhere;word-break:break-all;flex:1;overflow:auto;margin:0;padding:18px;font:12px/1.8 ui-monospace,monospace;min-height:150px}
.terminal-input{display:flex;align-items:center;gap:8px;padding:10px 14px;border-top:1px solid #344438}.terminal-input input{border:0;background:transparent;color:inherit;padding:4px;min-width:0;flex:1}.terminal-input button{background:transparent;color:inherit;border-color:#3d4e41;font-size:10px}.terminal-status{display:flex;justify-content:space-between;gap:8px;font-size:9px;color:#96aa97;padding:6px 14px}
@media(max-width:650px){.project-tools{padding:13px}.tools-bar{padding:6px;gap:3px}.tool-scope select{max-width:103px;font-size:10px}.file-filter{width:75px;min-width:50px}.file-breadcrumb{max-width:90px;font-size:10px}.tool-action-label{display:none}.files-content{position:relative}.file-detail{position:absolute;inset:0;max-width:none;background:var(--theme-surface,#fff);border:0;z-index:2}.terminal-status span{display:none}.terminal-notice{font-size:9px}}
</style>

<style scoped>
.file-entry-row{display:flex;border-bottom:1px solid var(--border);position:relative}.file-entry-row.selected{background:var(--theme-inset,#eef3ed)}.file-entry{flex:1;min-width:0;background:transparent!important;border-bottom:0;display:grid;grid-template-columns:minmax(0,1fr) 65px 145px}.file-table-head{display:grid;grid-template-columns:minmax(0,1fr) 65px 145px 30px}.file-modified{font-size:10px;color:var(--muted)}
.file-row-menu{flex:0 0 30px;align-self:center;font-size:12px}.file-row-menu summary{cursor:pointer;list-style:none}.file-row-menu>div{position:absolute;right:5px;top:34px;z-index:4;display:grid;min-width:120px;padding:6px;background:var(--theme-surface,#fff);border:1px solid var(--border);border-radius:8px;box-shadow:0 4px 12px #0001}.file-row-menu a,.file-row-menu button{text-align:left;padding:7px;font-size:11px;border:0;color:var(--ink);text-decoration:none;background:transparent}
.file-preview-button{display:block;width:100%;padding:0;background:transparent;border:0;cursor:zoom-in}.file-metadata{display:grid;grid-template-columns:60px minmax(0,1fr);gap:9px;font-size:10px;margin:20px 0;color:var(--muted)}.file-metadata dd{margin:0;color:var(--ink)}
.file-image-viewer{max-width:95vw;max-height:95dvh;padding:16px;border:1px solid var(--border);border-radius:12px;background:var(--theme-surface,#fff);color:var(--ink)}.file-image-viewer::backdrop{background:#101a1680}.file-image-viewer header{display:flex;align-items:center;gap:20px;margin-bottom:12px}.file-image-viewer header strong{flex:1;font-size:12px}.file-image-viewer>img{display:block;max-width:85vw;max-height:75dvh;object-fit:contain}
@media(max-width:900px){.file-modified{display:none}.file-entry{grid-template-columns:minmax(0,1fr) 50px}.file-table-head{grid-template-columns:minmax(0,1fr) 50px 30px}}
</style>

<style scoped>.file-preview-button>span{display:block;color:var(--blue);font-size:11px;margin:-14px 0 16px}.file-to-chat{background:var(--blue);color:#fff;border-color:var(--blue)}</style>
