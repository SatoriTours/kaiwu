<script setup lang="ts">
import { ref, computed, onMounted, watch } from "vue";
import { api } from "./api.ts";
import type { Profile } from "./types.ts";
import CustomSelect from "./CustomSelect.vue";
interface Model { provider: string; id: string; name: string; reasoning: boolean; contextWindow: number }
const props = defineProps<{ profile?: Profile; scope: string; saving: boolean; error: string }>();
const emit = defineEmits<{ close: []; submit: [Record<string, string>] }>();
const dialog = ref<HTMLDialogElement>();
const models = ref<Model[]>([]), loading = ref(false), loadError = ref("");
const provider = ref(props.profile?.provider ?? ""), model = ref(props.profile?.model ?? ""), thinking = ref(props.profile?.thinking ?? ""), search = ref("");
let request = 0;
const providers = computed(() => [...new Set(models.value.map(m => m.provider))]);
const providerOptions = computed(() => providers.value.map(p => ({ value: p, label: p })));
const filtered = computed(() => models.value.filter(m => m.provider === provider.value && (m.id + " " + m.name).toLowerCase().includes(search.value.toLowerCase())));
const modelOptions = computed(() =>
  filtered.value.map(m => ({
    value: m.id,
    label: m.name && m.name !== m.id ? `${m.name} · ${m.id}` : m.id,
  }))
);
const thinkingOptions = [
  { value: "", label: "沿用 pi 默认" },
  { value: "off", label: "关闭" },
  { value: "minimal", label: "最少" },
  { value: "low", label: "低" },
  { value: "medium", label: "中" },
  { value: "high", label: "高" },
  { value: "xhigh", label: "最高（模型须支持）" },
];
const selected = computed(() => models.value.find(m => m.provider === provider.value && m.id === model.value));
watch(provider, () => { model.value = ""; thinking.value = ""; });
watch(selected, value => { if (!value?.reasoning) thinking.value = ""; });
async function load(force = false) {
  const id = ++request; loading.value = true; loadError.value = "";
  try { const data = await api<{ models: Model[] }>(force ? "models?refresh=1" : "models"); if (id === request) models.value = data.models; }
  catch (e) { if (id === request) loadError.value = e instanceof Error ? e.message : "模型检测失败"; }
  finally { if (id === request) loading.value = false; }
}
onMounted(() => { dialog.value?.showModal(); void load(); });
</script>
<template>
  <dialog ref="dialog" aria-labelledby="model-dialog-title" @cancel.prevent="!saving && emit('close')">
    <h2 id="model-dialog-title">{{ scope === 'default' ? '默认模型' : scope === 'next' ? '切换下一轮模型' : '选择会话模型' }}</h2>
    <p>{{ scope === 'default' ? '用于之后创建的项目对话和任务，已有会话保留各自配置。' : scope === 'next' ? '当前会话已启动，修改将在下一轮对话边界生效并持久化到任务，不中断当前运行。' : '保存到当前会话及任务。已启动会话将作为下一轮默认模型生效，不中断当前运行。' }}</p>
    <form @submit.prevent="selected && !loading && !loadError && emit('submit', { provider, model, thinking })">
      <div class="row"><span class="muted">{{ loading ? '正在检测服务器 pi…' : `检测到 ${models.length} 个可用模型` }}</span><button type="button" :disabled="loading || saving" @click="load(true)">重新检测</button></div>
      <p v-if="loadError" class="error" role="alert">{{ loadError }}</p>
      <p v-else-if="!loading && !models.length">pi 尚无可用模型。请在服务器 pi 中登录提供商，或为开悟服务配置对应 API Key，再重新检测。</p>
      <label><span>提供商</span><CustomSelect aria-label="提供商" v-model="provider" :options="providerOptions" placeholder="选择提供商" required :disabled="loading || saving" /></label>
      <label><span>搜索模型</span><input aria-label="搜索模型" v-model="search" placeholder="输入模型名称或 ID" :disabled="saving" /></label>
      <label><span>模型</span><CustomSelect aria-label="模型" v-model="model" :options="modelOptions" placeholder="选择模型" required :disabled="loading || saving || !provider" /></label>
      <label v-if="selected?.reasoning"><span>思考级别</span><CustomSelect aria-label="思考级别" v-model="thinking" :options="thinkingOptions" placeholder="沿用 pi 默认" :disabled="saving" /></label>
      <p class="muted">选择后沿用服务器 pi 中该提供商的登录或服务 API Key。凭据仅保存在服务器的隔离会话，不传给浏览器；不加载个人扩展。检测不会发送模型请求。</p>
      <p v-if="error" class="error" role="alert">{{ error }}</p>
      <div class="dialog-actions"><button type="button" :disabled="saving" @click="emit('close')">取消</button><button class="primary" :disabled="saving || loading || !!loadError || !selected">{{ saving ? '保存中…' : scope === 'next' ? '下一轮使用此模型' : '保存模型' }}</button></div>
    </form>
  </dialog>
</template>
