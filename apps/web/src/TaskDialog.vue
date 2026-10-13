<script setup lang="ts">
import { ref, onMounted } from "vue";
import type { Topic, Project } from "./types.ts";
const props = defineProps<{
  kind: string;
  project?: Project;
  topicID: string;
  topics: Topic[];
  error: string;
  saving: boolean;
}>();
const emit = defineEmits<{ close: []; submit: [Record<string, string>] }>();
const dialog = ref<HTMLDialogElement>();
onMounted(() => dialog.value?.showModal());
const titles: Record<string, string> = {
  git: "项目 Git 设置",
  topic: "添加功能专题",
  task: "创建任务卡片",
  project: "添加项目",
  delete: "删除专题",
};
function submit(event: Event) {
  const data = Object.fromEntries(
    new FormData(event.target as HTMLFormElement),
  ) as Record<string, string>;
  emit("submit", data);
}
</script>
<template>
  <dialog
    ref="dialog"
    aria-labelledby="dialog-title"
    @cancel.prevent="emit('close')"
  >
    <h2 id="dialog-title">{{ titles[props.kind] }}</h2>
    <form @submit.prevent="submit">
      <template v-if="kind === 'project'"
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
          ><select name="topicId" :value="topicID">
            <option v-for="topic in topics" :key="topic.id" :value="topic.id">
              {{ topic.name }}
            </option>
          </select></label
        ><label
          ><span>任务标题（可选）</span
          ><input name="title" maxlength="80" /></label
        ><label
          ><span>需求</span
          ><textarea name="request" required maxlength="5000" autofocus />
        </label>
        <p>创建卡片不启动 CLI；发送消息时按需启动独立只读会话。</p></template
      >
      <p v-if="kind === 'delete'">
        删除「{{
          topics.find((t) => t.id === topicID)?.name
        }}」后，所有任务将移到“其他”。对话与执行状态保留。
      </p>
      <p class="error" role="alert">{{ error }}</p>
      <div class="dialog-actions">
        <button type="button" :disabled="saving" @click="emit('close')">
          取消</button
        ><button class="primary" :disabled="saving">
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
