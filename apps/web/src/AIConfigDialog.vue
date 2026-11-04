<script setup lang="ts">
import {onMounted,ref,toRaw} from 'vue';
import {api} from './api.ts';
type Config={schemaVersion:number;adapter?:string;profile?:{provider?:string;model?:string;thinking?:string};rules?:string[];skills?:string[];credentialRef?:string};
type View={config:Config;version:string;resolved:{profile:{provider:string;model:string};sources:Record<string,string>;version:string}};
const props=defineProps<{projectId?:string}>();
const emit=defineEmits<{close:[]}>();
const dialog=ref<HTMLDialogElement>(),scope=ref(''),view=ref<View>(),error=ref(''),notice=ref(''),busy=ref(false);
const overrideRules=ref(false),overrideSkills=ref(false),overrideModel=ref(false),rules=ref(''),skills=ref(''),model=ref('');
let request=0;
async function load(target=scope.value){
 const current=++request;scope.value=target;view.value=undefined;error.value='';notice.value='';
 try{
  const v=await api<View>('ai/config/read',{projectId:target});
  if(current!==request)return;
  view.value=v;overrideRules.value=v.config.rules!==undefined;overrideSkills.value=v.config.skills!==undefined;overrideModel.value=v.config.profile?.model!==undefined;
  rules.value=(v.config.rules??[]).join('\n\n');skills.value=(v.config.skills??[]).join(', ');model.value=v.config.profile?.model??'';
 }catch(e){if(current===request)error.value=e instanceof Error?e.message:'配置读取失败'}
}
async function save(){
 if(!view.value||busy.value)return;
 busy.value=true;error.value='';notice.value='';
 try{
 const config:Config=structuredClone(toRaw(view.value.config));
 if(!scope.value||overrideRules.value)config.rules=rules.value.trim()?[rules.value]:[];else delete config.rules;
 if(!scope.value||overrideSkills.value)config.skills=skills.value.split(/[,，]/).map(s=>s.trim()).filter(Boolean);else delete config.skills;
 if(scope.value){config.profile={...config.profile};if(overrideModel.value)config.profile.model=model.value.trim();else delete config.profile.model}
  view.value=await api<View>('ai/config/write',{projectId:scope.value,version:view.value.version,config});
  notice.value='已保存；仅影响之后新建的项目对话／任务，已有快照及运行不会改变。';
 }catch(e){error.value=e instanceof Error?e.message:'配置保存失败'}finally{busy.value=false}
}
const source=(value?:string)=>({builtin:'内置默认',global:'全局方案',project:'项目覆盖'}[value??'']??'未知');
onMounted(()=>{dialog.value?.showModal();void load()});
</script>
<template>
<dialog ref="dialog" class="ai-config-dialog" aria-labelledby="ai-config-title" @cancel.prevent="!busy&&emit('close')">
 <h2 id="ai-config-title">AI 配置</h2>
 <div class="ai-config-tabs"><button type="button" :aria-pressed="!scope" :disabled="busy" @click="load('')">全局方案</button><button v-if="projectId" type="button" :aria-pressed="!!scope" :disabled="busy" @click="load(projectId)">当前项目</button></div>
 <p>文件为配置来源，凭据仅沿用服务器已授权的 pi 登录引用。目录归属不是安全沙箱。</p>
 <form v-if="view" @submit.prevent="save">
  <p>适配器：pi · 模型：{{view.resolved.profile.provider||'未选'}} / {{view.resolved.profile.model||'未选'}}</p>
  <p>模型来源：{{source(view.resolved.sources['profile.model'])}}。全局默认模型使用现有模型选择器调整。</p>
  <template v-if="scope"><label class="ai-config-check"><input v-model="overrideModel" type="checkbox" :disabled="busy" />覆盖模型</label><label v-if="overrideModel"><span>项目模型 ID（沿用全局提供商）</span><input v-model="model" :disabled="busy" required maxlength="300" /></label></template>
  <label v-if="scope" class="ai-config-check"><input v-model="overrideRules" type="checkbox" :disabled="busy" />覆盖规则</label>
  <label v-if="!scope||overrideRules"><span>规则</span><textarea v-model="rules" :disabled="busy" rows="4" maxlength="32000" /></label>
  <p>规则来源：{{source(view.resolved.sources.rules)}}</p>
  <label v-if="scope" class="ai-config-check"><input v-model="overrideSkills" type="checkbox" :disabled="busy" />覆盖技能</label>
  <label v-if="!scope||overrideSkills"><span>技能标识（逗号分隔，留空禁用）</span><input v-model="skills" :disabled="busy" maxlength="1000" /></label>
  <p>项目只能使用全局已批准技能；全局技能文件须由管理员放入开悟配置目录，不能加载任意路径或扩展。</p>
  <details><summary>来源版本</summary><code>{{view.resolved.version}}</code></details>
  <p v-if="notice" role="status">{{notice}}</p><p v-if="error" role="alert">{{error}}</p>
  <div class="dialog-actions"><button type="button" :disabled="busy" @click="emit('close')">关闭</button><button type="button" :disabled="busy" title="重新读取会丢弃此表单尚未保存的修改" @click="load()">重新读取</button><button class="primary" :disabled="busy">保存配置</button></div>
 </form>
 <template v-else><p v-if="error" role="alert">{{error}}</p><p v-else role="status">正在读取配置…</p><div class="dialog-actions"><button @click="emit('close')">关闭</button><button @click="load()">重新读取</button></div></template>
</dialog>
</template>
<style scoped>
.ai-config-dialog{max-height:90dvh;overflow:auto;width:min(34rem,calc(100vw - 2rem));box-sizing:border-box}
.ai-config-dialog p{font-size:.82rem;line-height:1.6;color:var(--muted);overflow-wrap:anywhere}
.ai-config-tabs{display:flex;gap:.4rem}
.ai-config-tabs [aria-pressed=true]{font-weight:600;border-color:var(--accent)}
.ai-config-check{display:flex;align-items:center;gap:.5rem;font-size:.9rem}
.ai-config-check input{width:auto;margin:0}
.ai-config-dialog code{font-size:.75rem;overflow-wrap:anywhere}
.ai-config-dialog .dialog-actions{flex-wrap:wrap}
</style>
