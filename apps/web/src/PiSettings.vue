<script setup lang="ts">
import {computed,nextTick,onBeforeUnmount,onMounted,reactive,ref,watch} from 'vue';
import {api} from './api.ts';
import CustomSelect from './CustomSelect.vue';
import ThemePicker from './ThemePicker.vue';
import {connectionDraft,mergeDiscoveredModels,cleanModelID,credentialChanged,protocolOptions,type PiConnection,type ConnectionModel,type OAuthGrant} from './connections.ts';
import type {Profile} from './types.ts';
import './pi-settings.css';
const props=defineProps<{profile?:Profile}>();
const emit=defineEmits<{close:[];model:[]}>();
const dialog=ref<HTMLDialogElement>();
const view=ref<'list'|'form'>('list'),tab=ref('models'),method=ref('key');
const connections=ref<PiConnection[]>([]);
const oauthProviders=ref([{value:'openai',label:'OpenAI · ChatGPT 订阅'},{value:'anthropic',label:'Anthropic · Claude 订阅'}]);
const loading=ref(false),saving=ref(false),refreshing=ref(false),authorizing=ref(false);
const error=ref(''),notice=ref(''),modelError=ref(''),modelStatus=ref('');
const draft=reactive(connectionDraft());
let baseline=connectionDraft(),editKind='api_key',alive=true,version=0,modelVersion=0;
const catalog=ref<ConnectionModel[]>([]),search=ref(''),manual=ref(''),manualOpen=ref(false),reveal=ref(false);
const menu=ref(''),remove=ref<PiConnection>(),discard=ref<'close'|'back'>();
const provider=ref('openai'),grant=ref<OAuthGrant>(),callback=ref('');
const busy=computed(()=>saving.value||authorizing.value);
const filtered=computed(()=>catalog.value.filter(m=>(m.id+' '+m.name).toLowerCase().includes(search.value.toLowerCase())));
const dirty=computed(()=>JSON.stringify(draft)!==JSON.stringify(baseline)||!!grant.value);
const needsKey=computed(()=>draft.hasKey&&credentialChanged(baseline,draft)&&!draft.key);
const defaultProvider=computed(()=>connections.value.find(c=>c.provider===props.profile?.provider)?.name||props.profile?.provider);
const title=computed(()=>view.value==='list'?'设置':draft.id?'编辑 pi 连接':'添加 pi 连接');
function message(e:unknown){return e instanceof Error?e.message:'操作未完成，请稍后重试'}
async function load(){
 loading.value=true;error.value='';
 try{const result=await api<{connections:PiConnection[];oauthProviders:typeof oauthProviders.value}>('pi/connections');if(alive){connections.value=result.connections;oauthProviders.value=result.oauthProviders;}}
 catch(e){if(alive)error.value=message(e)}
 finally{if(alive)loading.value=false}
}
function cancelGrant(){
 const id=grant.value?.id;grant.value=undefined;callback.value='';
 if(id)void api('pi/oauth/cancel',{id}).catch(()=>{});
}
function resetSecrets(){draft.key='';reveal.value=false;cancelGrant()}
function leave(action:'close'|'back'){
 version++;modelVersion++;resetSecrets();discard.value=undefined;
 if(action==='close'){emit('close');return}
 view.value='list';error.value='';modelError.value='';void load();
}
function askLeave(action:'close'|'back'){
 if(busy.value)return;
 if(view.value==='form'&&dirty.value){discard.value=action;return}
 leave(action);
}
function escapeDialog(){
 if(remove.value){if(!saving.value)remove.value=undefined;return}
 if(menu.value){menu.value='';return}
 if(discard.value){discard.value=undefined;return}
 askLeave('close');
}
function trapRemoval(event:KeyboardEvent){
 if(event.key!=='Tab')return;
 const items=dialog.value?.querySelectorAll<HTMLButtonElement>('.pi-remove-overlay button:not(:disabled)');if(!items?.length)return;
 const first=items[0],last=items[items.length-1];
 if(event.shiftKey&&document.activeElement===first){event.preventDefault();last.focus()}
 else if(!event.shiftKey&&document.activeElement===last){event.preventDefault();first.focus()}
}
watch(remove,value=>{if(value)void nextTick(()=>dialog.value?.querySelector<HTMLButtonElement>('.pi-remove-overlay button')?.focus())});
watch(()=>draft.name,()=>{if(grant.value){version++;cancelGrant()}});
function edit(c?:PiConnection){
 version++;modelVersion++;cancelGrant();Object.assign(draft,connectionDraft(c));baseline=connectionDraft(c);editKind=c?.kind??'api_key';
 method.value=editKind==='oauth'?'login':'key';provider.value=c?.kind==='oauth'?c.provider:'openai';
 catalog.value=draft.models.map(id=>({id,name:id,manual:true}));search.value='';manual.value='';manualOpen.value=false;reveal.value=false;
 error.value='';notice.value='';modelError.value='';modelStatus.value='';menu.value='';view.value='form';refreshing.value=false;
 void nextTick(()=>dialog.value?.querySelector<HTMLInputElement>('[aria-label="连接名称"]')?.focus());
}
watch(()=>[draft.baseUrl,draft.api,draft.key],()=>{
 modelVersion++;refreshing.value=false;modelError.value='';
 catalog.value=catalog.value.filter(m=>draft.models.includes(m.id)).map(m=>({...m,manual:true}));
 modelStatus.value='连接信息已变化，请刷新列表或核对已选模型';
});
watch([method,provider],()=>{
 version++;modelVersion++;refreshing.value=false;cancelGrant();error.value='';modelError.value='';
 if(method.value==='login'){draft.key='';reveal.value=false}
});
async function discover(){
 if(refreshing.value||busy.value)return;
 const token=++modelVersion,editToken=version;refreshing.value=true;modelError.value='';
 try{
 const result=await api<{models:ConnectionModel[]}>('pi/connections/models',{...draft,hasKey:undefined});
 if(!alive||token!==modelVersion||editToken!==version)return;
 catalog.value=mergeDiscoveredModels(catalog.value,result.models,draft.models);
 modelStatus.value=result.models.length?'已更新 · 未列出的已选模型仍会保留':'未获取到模型，可手动添加';
 }catch(e){if(alive&&token===modelVersion&&editToken===version)modelError.value=message(e)}
 finally{if(alive&&token===modelVersion)refreshing.value=false}
}
function toggle(id:string,checked:boolean){draft.models=checked?[...new Set([...draft.models,id])]:draft.models.filter(v=>v!==id)}
function addManual(){
 const id=cleanModelID(manual.value);
 if(!id){modelError.value='请输入有效的模型 ID，最多 300 个字符';return}
 if(!catalog.value.some(m=>m.id===id))catalog.value.push({id,name:id,manual:true});
 toggle(id,true);manual.value='';search.value='';modelError.value='';
}
async function save(){
 if(busy.value||refreshing.value)return;
 error.value='';saving.value=true;
 try{
 await api('pi/connections/save',{...draft,hasKey:undefined});
 if(!alive)return;
 notice.value='连接已保存，默认模型未改变';leave('back');
 }catch(e){if(alive)error.value=message(e)}
 finally{if(alive)saving.value=false}
}
async function startOAuth(){
 if(busy.value)return;authorizing.value=true;error.value='';cancelGrant();
 const token=version;
 try{
 const result=await api<OAuthGrant>('pi/oauth/start',{provider:provider.value,name:draft.name,id:draft.id,revision:draft.revision});
 if(!alive||token!==version){void api('pi/oauth/cancel',{id:result.id}).catch(()=>{});return}
 grant.value=result;
 }catch(e){if(alive&&token===version)error.value=message(e)}
 finally{if(alive&&token===version)authorizing.value=false}
}
async function finishOAuth(){
 if(!grant.value||busy.value)return;
 authorizing.value=true;error.value='';
 try{
 await api('pi/oauth/finish',{id:grant.value.id,callback:callback.value});
 if(!alive)return;grant.value=undefined;callback.value='';notice.value='账号已绑定，请在默认模型中选择模型';leave('back');
 }catch(e){if(alive)error.value=message(e)}
 finally{if(alive)authorizing.value=false}
}
async function removeConnection(){
 if(!remove.value||saving.value)return;
 saving.value=true;error.value='';
 try{await api('pi/connections/remove',{id:remove.value.id,revision:remove.value.revision});if(alive){remove.value=undefined;notice.value='连接已移除，已有会话未被中断';await load()}}
 catch(e){if(alive)error.value=message(e)}
 finally{if(alive)saving.value=false}
}
function endpoint(value:string){try{return new URL(value).host}catch{return value}}
function chooseModel(){resetSecrets();emit('model')}
onMounted(()=>{dialog.value?.showModal();void load()});
onBeforeUnmount(()=>{alive=false;version++;modelVersion++;resetSecrets()});
</script>

<template>
<dialog ref="dialog" class="pi-settings" aria-labelledby="pi-settings-title" @cancel.prevent="escapeDialog" @click.self="askLeave('close')">
 <header class="pi-settings-head">
  <button v-if="view==='form'" class="pi-back" :disabled="busy" @click="askLeave('back')">‹ 返回</button>
  <h2 id="pi-settings-title">{{title}}</h2>
  <button class="pi-close" aria-label="关闭设置" :disabled="busy" @click="askLeave('close')">×</button>
 </header>
 <nav v-if="view==='list'" class="pi-settings-tabs" aria-label="设置分类">
  <button :aria-pressed="tab==='models'" @click="tab='models'">模型与账号</button>
  <button :aria-pressed="tab==='appearance'" @click="tab='appearance'">外观</button>
 </nav>
 <div v-if="view==='list'" class="pi-settings-body">
  <p v-if="error" class="pi-error" role="alert">{{error}}</p>
  <p v-if="notice" class="pi-notice" role="status">{{notice}}</p>
  <template v-if="tab==='models'">
   <div class="pi-section-title"><h3>默认模型</h3><span>用于新建会话</span></div>
   <section class="pi-default-model">
    <span class="pi-provider-icon">π</span>
    <div><strong>{{profile?.model||'尚未选择默认模型'}}</strong><small>{{defaultProvider||'添加连接后选择模型'}}{{profile?.thinking?' · '+profile.thinking:''}}</small></div>
    <button @click="chooseModel">选择模型 ⌄</button>
   </section>
   <div class="pi-section-title"><h3>pi 连接</h3><button class="primary" aria-label="添加连接" :disabled="loading" @click="edit()">＋ 添加连接</button></div>
   <div v-if="loading" class="pi-empty" role="status">正在读取连接…</div>
   <div v-else-if="!connections.length" class="pi-empty"><span class="pi-empty-icon">↗</span><strong>连接你自己的模型服务</strong><span>绑定账号，或使用服务商提供的 URL 与 API Key</span><button @click="edit()">添加第一个连接</button></div>
   <div v-else class="pi-connection-list">
    <article v-for="c in connections" :key="c.id" class="pi-connection-row">
     <span class="pi-provider-icon">{{c.kind==='oauth'?'◎':'↗'}}</span>
     <div class="pi-connection-label"><strong>{{c.name}}</strong><small>{{c.kind==='oauth'?'账号授权 · '+c.provider:'API Key · '+endpoint(c.baseUrl)}}<template v-if="c.models.length"> · {{c.models.length}} 个模型</template></small></div>
     <span class="pi-configured"><i></i>{{c.kind==='oauth'?'已绑定':'已配置'}}</span>
     <button class="pi-edit" :aria-label="'编辑 '+c.name" @click="edit(c)">{{c.kind==='oauth'?'重新授权':'编辑'}}</button>
     <div class="pi-menu-wrap"><button class="pi-more" :aria-label="'更多 '+c.name" :aria-expanded="menu===c.id" @click="menu=menu===c.id?'':c.id">···</button>
      <div v-if="menu===c.id" class="pi-row-menu"><button @click="edit(c)">编辑连接</button><button class="pi-danger" @click="remove=c;menu=''">移除连接</button></div>
     </div>
    </article>
   </div>
   <div v-if="error&&!loading" class="pi-load-retry"><button @click="load()">重新读取连接</button></div>
   <section class="pi-server-source"><span class="pi-provider-icon">π</span><div><strong>服务器已有配置 <em>只读来源</em></strong><small>沿用服务器 pi 已有账号，不修改原配置</small></div><button @click="chooseModel">查看模型 ↗</button></section>
   <p class="pi-quiet">凭据仅保存在服务器，不进入聊天。连接变更不会中断运行中的任务；已有会话保留原凭据快照。</p>
  </template>
  <section v-else class="pi-appearance"><div><h3>界面主题</h3><p>跟随系统，或选择浅色／深色外观</p></div><ThemePicker /></section>
 </div>
 <div v-else class="pi-settings-body">
  <div v-if="discard" class="pi-warning" role="alert"><span>尚有未保存的修改，是否放弃？</span><button @click="discard=undefined">继续编辑</button><button @click="leave(discard!)">放弃修改</button></div>
  <p v-if="error" class="pi-error" role="alert">{{error}}</p>
  <div class="pi-form-intro"><div class="pi-method-tabs" role="group" aria-label="连接方式"><button :aria-pressed="method==='key'" :disabled="busy||editKind==='oauth'" @click="method='key'">API Key</button><button :aria-pressed="method==='login'" :disabled="busy||!!draft.id&&editKind!=='oauth'" @click="method='login'">账号登录</button></div><span>连接你的模型服务</span></div>
  <div class="pi-form-grid">
   <label><span>连接名称</span><input v-model="draft.name" aria-label="连接名称" maxlength="80" placeholder="例如：我的中转" :disabled="busy" autocomplete="off" /></label>
   <label v-if="method==='key'"><span>提供商／接口协议</span><CustomSelect v-model="draft.api" :options="protocolOptions" aria-label="接口协议" :disabled="busy" /></label>
   <label v-else><span>提供商</span><CustomSelect v-model="provider" :options="oauthProviders" aria-label="登录提供商" :disabled="busy||!!draft.id" /></label>
  </div>
  <template v-if="method==='key'">
   <label class="pi-field"><span>接口 URL</span><input v-model="draft.baseUrl" aria-label="接口 URL" placeholder="https://api.example.com/v1" :disabled="busy" autocomplete="off" spellcheck="false" /><small>填写 API 基础地址，不是聊天网页地址。<b v-if="draft.baseUrl.startsWith('http:')" class="pi-http-warning"> HTTP 会明文传输 Key，请仅用于可信网络。</b></small></label>
   <label class="pi-field"><span>API Key <em v-if="draft.hasKey">已配置</em></span><div class="pi-key-field"><input v-model="draft.key" aria-label="API Key" :type="reveal?'text':'password'" :placeholder="draft.hasKey?'留空保留已配置的 Key':'输入服务商提供的 API Key'" :disabled="busy" autocomplete="new-password" spellcheck="false" /><button :aria-label="reveal?'隐藏 Key':'显示 Key'" :aria-pressed="reveal" @click="reveal=!reveal">◉</button></div><small v-if="needsKey" class="pi-http-warning">地址或协议已改变，请重新输入 Key，避免将原凭据发送到新地址。</small><small v-else>保存后不回显原值；编辑时留空保留原 Key。</small></label>
   <section class="pi-model-section">
    <div class="pi-section-title"><h3>可用模型 <em v-if="draft.models.length">已选 {{draft.models.length}} 个</em></h3><button class="pi-refresh-models" :disabled="refreshing||busy||!draft.baseUrl||(!draft.key&&!draft.hasKey)||needsKey" @click="discover"><span :class="{'pi-spinning':refreshing}" aria-hidden="true">⟳</span>{{refreshing?'正在获取':'刷新获取'}}</button></div>
    <div class="pi-model-picker">
     <div class="pi-model-search"><span aria-hidden="true">⌕</span><input v-model="search" aria-label="搜索模型" placeholder="搜索模型名称或 ID" /><small>{{catalog.length?catalog.length+' 个模型':'尚未获取'}}</small></div>
     <div class="pi-model-options" :aria-busy="refreshing">
      <label v-for="m in filtered" :key="m.id" class="pi-model-option"><input type="checkbox" :aria-label="m.id" :checked="draft.models.includes(m.id)" :disabled="busy" @change="toggle(m.id,($event.target as HTMLInputElement).checked)" /><span>{{m.id}}</span><small>{{m.manual?'手动／保留':m.name===m.id?'':m.name}}</small></label>
      <div v-if="!filtered.length" class="pi-model-empty"><span>{{catalog.length?'没有匹配的模型':'填写 URL 和 Key 后，点击「刷新获取」'}}</span><small>也可直接手动添加模型 ID</small></div>
     </div>
    </div>
    <div class="pi-model-meta"><span role="status">{{modelStatus||'获取模型列表不发送对话或生成请求'}}</span><button :aria-expanded="manualOpen" @click="manualOpen=!manualOpen">手动添加</button></div>
    <div v-if="manualOpen" class="pi-manual-model"><input v-model="manual" aria-label="手动输入模型 ID" placeholder="输入准确的模型 ID" maxlength="300" :disabled="busy" @keydown.enter.prevent="addManual" /><button aria-label="添加模型" :disabled="busy||!manual.trim()" @click="addManual">添加</button></div>
    <p v-if="modelError" class="pi-error" role="alert">{{modelError}}</p>
   </section>
   <p class="pi-quiet">Key 仅保存到服务器。已有会话不追溯更新；新会话使用保存后的配置。</p>
  </template>
  <template v-else>
   <section class="pi-oauth-intro"><span>↗</span><h3>通过提供商授权绑定账号</h3><p>在提供商页面登录，不在开悟输入账号密码。<br />绑定后优先于服务器 pi 中的同名提供商账号。</p>
    <button :disabled="busy||!draft.name.trim()" @click="startOAuth">{{authorizing?'处理中…':grant?'重新生成授权链接':'生成授权链接'}}</button>
    <a v-if="grant" :href="grant.url" target="_blank" rel="noopener noreferrer" class="pi-auth-link">打开授权页面</a>
   </section>
   <div v-if="grant" class="pi-oauth-help"><strong>完成登录后，将授权结果粘贴到下方</strong><p v-if="provider==='openai'">回调地址是 127.0.0.1:1455；若浏览器显示无法访问，复制地址栏中的完整 URL 即可。本服务不额外打开回调端口。</p><p v-else>复制提供商页面显示的授权码（含 # 后的 state）或完整回调 URL。</p><small>授权链接 10 分钟内有效，仅属于本次工作台登录。</small></div>
   <label v-if="grant" class="pi-field"><span>授权码／回调 URL</span><textarea v-model="callback" aria-label="授权码或回调 URL" placeholder="粘贴本次授权结果" rows="3" :disabled="busy" autocomplete="off" spellcheck="false" /></label>
   <p class="pi-quiet">首批提供 OpenAI ChatGPT 与 Anthropic Claude 的授权方式；其他账号仍可沿用服务器 pi 登录。绑定不发送模型请求。</p>
  </template>
 </div>
 <footer class="pi-settings-foot">
  <span>{{view==='list'?'修改默认模型不影响已有会话':'保存后不自动切换默认模型'}}</span>
  <template v-if="view==='list'"><button class="primary" @click="askLeave('close')">完成</button></template>
  <template v-else><button :disabled="busy" @click="askLeave('back')">取消</button><button v-if="method==='key'" class="primary" :disabled="busy||refreshing||!draft.name.trim()||!draft.baseUrl||(!draft.key&&!draft.hasKey)||!draft.models.length||needsKey" @click="save">{{saving?'保存中…':'保存连接'}}</button><button v-else class="primary" :disabled="busy||!grant||!callback.trim()" @click="finishOAuth">{{authorizing?'绑定中…':'完成绑定'}}</button></template>
 </footer>
 <div v-if="remove" class="pi-remove-overlay" @keydown="trapRemoval"><section role="alertdialog" aria-modal="true" aria-labelledby="pi-remove-title"><h3 id="pi-remove-title">移除连接「{{remove.name}}」？</h3><p>不再用于新会话。已有会话保留原凭据快照，正在运行的任务不会中断；默认模型不会自动改选，也不会撤销服务商端的凭据。</p><p v-if="error" class="pi-error" role="alert">{{error}}</p><div><button :disabled="saving" @click="remove=undefined">取消</button><button class="pi-danger" :disabled="saving" @click="removeConnection">确认移除</button></div></section></div>
</dialog>
</template>
