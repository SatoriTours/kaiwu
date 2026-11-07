<script setup lang="ts">
import { ref, computed, onMounted } from "vue";
import type { Topic, Project } from "./types.ts";
import CustomSelect from "./CustomSelect.vue";
import {api} from "./api.ts";
const props = defineProps<{
  kind: string;
  managedProjects?: boolean;
  project?: Project;
  topicID: string;
  topics: Topic[];
  error: string;
  saving: boolean;
}>();
type PendingCreation = {operationId:string;projectId:string;name:string;phase:string};
const emit = defineEmits<{ close: []; submit: [Record<string, string>]; retry:[PendingCreation] }>();
const pendingCreations=ref<PendingCreation[]>([]),recoveryError=ref('');
onMounted(async()=>{
 if(props.kind!=='project'||!props.managedProjects)return;
 try{pendingCreations.value=(await api<{creations:PendingCreation[]}>('projects/creations')).creations}
 catch(e){recoveryError.value=e instanceof Error?e.message:'无法读取未完成创建记录'}
});
const dialog = ref<HTMLDialogElement>();
const selectedTopicId = ref(props.topicID);
const submittingStart = ref(false);
const topicOptions = computed(() =>
  props.topics.map((t) => ({ value: t.id, label: t.name }))
);
onMounted(() => dialog.value?.showModal());
const titles: Record<string, string> = {
  git: "项目 Git 设置",
  topic: "添加功能专题",
  task: "创建任务卡片",
  project: "添加项目",
  rename: "重命名项目",
  delete: "删除专题",
};
function submit(event: Event) {
  if (props.saving) return;
  const data = Object.fromEntries(
    new FormData(event.target as HTMLFormElement),
  ) as Record<string, string>;
  if (props.kind === "task") {
    submittingStart.value = (event as SubmitEvent).submitter?.getAttribute("value") === "start";
    data.startAfterCreate = submittingStart.value ? "true" : "false";
  }
  emit("submit", data);
}
</script>
<template>
  <dialog
    ref="dialog"
    :class="{ 'task-create-dialog': kind === 'task' }"
    aria-labelledby="dialog-title"
    @cancel.prevent="!saving && emit('close')"
  >
    <button v-if="kind === 'task'" type="button" class="task-create-close" aria-label="关闭创建任务" :disabled="saving" @click="emit('close')">×</button>
    <h2 id="dialog-title">{{ kind === 'project' && managedProjects ? '创建项目' : titles[props.kind] }}</h2>
    <form @submit.prevent="submit">
      <template v-if="kind === 'project' && managedProjects">
        <p>填写名称即可创建项目。代码、会话与成果由开悟管理；同名项目会自动区分，名称以后可以修改。</p>
        <label><span>项目名称</span><input name="name" required maxlength="80" autofocus /></label>
        <p>目录归属不代表安全沙箱；不自动克隆、提交或推送。</p>
        <p v-if="recoveryError" role="alert">{{ recoveryError }}</p>
        <section v-if="pendingCreations.length" aria-label="未完成的项目创建">
          <p>以下创建尚未完成，保留了原身份和材料；仅在点击后重试，不会自动执行。</p>
          <button v-for="item in pendingCreations" :key="item.operationId" type="button" :disabled="saving" :aria-label="'重试创建 '+item.name" @click="emit('retry',item)">重试创建 {{ item.name }} · {{ item.projectId.slice(0,8) }}</button>
        </section>
      </template>
      <template v-else-if="kind === 'project'"
        ><p>
          填写服务器上的项目目录；目录不存在时会创建，尚未建立 Git
          仓库时会自动初始化。
        </p>
        <label
          ><span>项目名称</span
          ><input name="name" required maxlength="80" autofocus /></label
        ><label
          ><span>项目绝对路径</span
          ><input name="path" required placeholder="/path/to/project" /></label
        ><label
          ><span>Git 远程地址（可选）</span
          ><input
            name="remoteURL"
            maxlength="2048"
            placeholder="https://github.com/owner/repo.git"
        /></label>
        <p>
          可留空，已有仓库留空时沿用原配置。填写后仅设置
          origin，不自动克隆、提交或推送。
        </p></template
      >
      <template v-if="kind === 'rename'">
        <p>只修改显示名称，不移动目录，也不改变任务、对话或运行状态。</p>
        <label><span>项目名称</span><input name="name" :value="project?.name" required maxlength="80" autofocus /></label>
      </template>
      <template v-if="kind === 'git'"
        ><p>默认主分支：{{ project?.defaultBranch || "自动识别" }}</p>
        <details open>
          <summary>高级设置（可选）</summary>
          <label
            ><span>主分支覆盖</span
            ><input
              name="mainBranchOverride"
              :value="project?.mainBranchOverride"
              placeholder="留空使用自动识别的主分支"
              maxlength="200" /></label
          ><label
            ><span>提交者姓名</span
            ><input
              name="commitName"
              :value="project?.commitName"
              placeholder="沿用项目配置或 Kaiwu"
              maxlength="100" /></label
          ><label
            ><span>提交者邮箱</span
            ><input
              name="commitEmail"
              :value="project?.commitEmail"
              placeholder="沿用项目配置或 kaiwu@localhost"
              maxlength="200"
          /></label></details
      ></template>
      <label v-if="kind === 'topic'"
        ><span>专题名称</span
        ><input name="name" required maxlength="40" autofocus
      /></label>
      <template v-if="kind === 'task'"
        ><label
          ><span>功能专题</span
          ><CustomSelect
            name="topicId"
            aria-label="功能专题"
            v-model="selectedTopicId"
            :options="topicOptions"
            :disabled="saving"
            required
        /></label
        ><label
          ><span>你想做什么？</span
          ><textarea name="request" required maxlength="5000" placeholder="描述你想完成的功能或解决的问题…" :disabled="saving" autofocus />
        </label>
      </template>
      <p v-if="kind === 'delete'">
        删除「{{
          topics.find((t) => t.id === topicID)?.name
        }}」后，所有任务将移到“其他”。对话与执行状态保留。
      </p>
      <p v-if="error || kind !== 'task'" class="error" role="alert">{{ error }}</p>
      <div class="dialog-actions" :class="{ 'task-create-actions': kind === 'task' }">
        <button class="cancel" type="button" :disabled="saving" @click="emit('close')">
          取消</button
        >
        <template v-if="kind === 'task'">
          <button type="submit" value="create" :disabled="saving">{{ saving && !submittingStart ? "创建中…" : "创建" }}</button>
          <button type="submit" value="start" class="primary" :disabled="saving"><span aria-hidden="true">▷</span> {{ saving && submittingStart ? "创建中…" : "创建并开始" }}</button>
        </template>
        <button v-else class="primary" :disabled="saving">
          {{
            saving
              ? "保存中…"
              : kind === "delete"
                ? "删除专题，保留任务"
                : "保存"
          }}
        </button>
      </div>
    </form>
  </dialog>
</template>
