<script setup lang="ts">
import {
  reactive,
  watch,
  ref,
  computed,
  onMounted,
  onBeforeUnmount,
  nextTick,
} from "vue";
import KanbanBoard from "./KanbanBoard.vue";
import ProjectMenu from "./ProjectMenu.vue";
import ChatPanel from "./ChatPanel.vue";
import ResultPreview from "./ResultPreview.vue";
import {safePreviewURL} from "./preview.ts";
import TaskDialog from "./TaskDialog.vue";
import MergeDialog from "./MergeDialog.vue";
import ModelDialog from "./ModelDialog.vue";
import DeliverySettings from "./DeliverySettings.vue";
import VersionBadge from "./VersionBadge.vue";
import { isMergeRequest } from "./git.ts";
import {taskState} from "./task-state.ts";
import { SocketClient } from "./socket.ts";
import { applyPatch } from "./sync.ts";
import { api, httpAPI, APIError, setSocketRequest } from "./api.ts";
import type {
  Snapshot,
  Task,
  Receipt,
  MergePreview,
  MergeOperation,
  Conversation,
  Profile,
  TaskPreview,
  ProjectDeliveryPolicy,
  DeliverySummary,
} from "./types.ts";
import {
  buildAcceptPayload,
  buildControlPayload,
  buildSavePolicyPayload,
  isDeliveryActive,
  type PolicyDraft,
} from "./delivery.ts";
import {
  createLocalState,
  selectConversation,
  setDraft,
  acceptSent,
  mergeSnapshot,
  createRefreshQueue,
  createOperationID,
  isStartRequest,
} from "./workbench.ts";
import {
  clearInteractionDraft,
  clearInteractionDrafts,
  type InteractionAnswers,
  type InteractionOthers,
} from "./interaction.ts";
import {
  dispatchInteractionCommand,
  type InteractionCommandPayload,
} from "./interaction-command.ts";
const local = reactive(createLocalState());
const authenticated = ref(false),
  loaded = ref(false),
  online = ref(false),
  agentAvailable = ref(false),
  token = ref(""),
  showToken = ref(false),
  loginBusy = ref(false),
  error = ref(""),
  projectID = ref(""),
  taskID = ref(""),
  mobileTopic = ref(""),
  chatVisible = ref(false),
  dialog = ref(""),
  dialogTopic = ref(""),
  formError = ref(""),
  saving = ref(false);
const chatPanel=ref<InstanceType<typeof ChatPanel>>();
const resultTarget=ref<{projectId:string;taskId:string;roundId:string}>();
const taskPreview=ref<TaskPreview>();
const previewBusy=ref(false),previewError=ref('');
let previewEpoch=0,previewTimer:ReturnType<typeof setTimeout>|undefined;
const mergePreview = ref<MergePreview>();
const modelTarget = ref<{ scope: string; projectId?: string; conversationId?: string; taskId?: string; profile?: Profile }>();
const modelSaving = ref(false), modelError = ref("");
const deliverySettingsOpen = ref(false);
const deliverySaving = ref(false);
const deliveryError = ref("");
const mergeTarget = ref<Task>();
const mergeError = ref(""),
  mergeSaving = ref(false),
  mergeOpID = ref("");
let mergeRequest:
  | {
      operationId: string;
      projectId: string;
      taskId: string;
      previewId: string;
      commitMessage: string;
    }
  | undefined;
let mergeDraft: { conversationId: string; revision: number } | undefined;
const mergeOperation = computed(() => state.value?.merges?.[mergeOpID.value]);
let gitTimer: ReturnType<typeof setTimeout> | undefined,
  gitChecking = false;
const unread = reactive<Record<string, number>>({});
const lengths = new Map<string, number>();
const state = computed(() => local.snapshot);
const project = computed(() => state.value?.projects[projectID.value]);
const topics = computed(
  () =>
    state.value?.topics.filter((t) => t.projectId === projectID.value) ?? [],
);
const tasks = computed(() =>
  Object.values(state.value?.tasks ?? {}).filter(
    (t) => t.projectId === projectID.value,
  ),
);
const task = computed(() => state.value?.tasks[taskID.value]);
const conversation = computed(() => state.value?.conversations[local.current]);
const draft = computed(() => local.drafts[local.current]?.text ?? "");
let ws: SocketClient | undefined,
  retry: ReturnType<typeof setTimeout> | undefined,
  stopped = false,
  authEpoch = 0;
let streamID="";
const width = ref(innerWidth);
const operations = reactive(
  new Map<string, { id: string; signature: string; draftRevision?: number }>(),
);
function readLocation() {
  const match = location.pathname.match(
    /^\/projects\/([^/]+)(?:\/tasks\/([^/]+))?$/,
  );
  if (match) {
    projectID.value = match[1]!;
    taskID.value = match[2] ?? "";
  }
}
function navigate(task?: Task) {
  if (!project.value) return;
  unread[task?.conversationId ?? project.value.conversationId] = 0;
  taskID.value = task?.id ?? "";
  selectConversation(
    local,
    task?.conversationId ?? project.value.conversationId,
  );
  history.pushState(
    null,
    "",
    "/projects/" + project.value.id + (task ? "/tasks/" + task.id : ""),
  );
  if (width.value < 1024) chatVisible.value = true;
}
function chooseProject(id: string) {
  projectID.value = id;
  taskID.value = "";
  mobileTopic.value = "";
  navigate();
  chatVisible.value = false;
}
function clearIdentity() {
  closeResult();
  authEpoch++;
 streamID="";
  authenticated.value = false;
  online.value = false;
  loginBusy.value = false;
  showToken.value = false;
  saving.value = false;
  dialog.value = "";
  modelTarget.value = undefined;
  modelSaving.value = false;
  modelError.value = "";
  mergePreview.value = undefined;
  mergeTarget.value = undefined;
  mergeRequest = undefined;
  mergeDraft = undefined;
  mergeOpID.value = "";
  mergeSaving.value = false;
  clearTimeout(gitTimer);
  formError.value = "";
  ws?.close();
  ws=undefined;setSocketRequest(undefined);
  clearTimeout(retry);
  local.snapshot = null;
  local.drafts = {};
  local.pending = {};
  local.current = "";
  operations.clear();
  lengths.clear();
  clearInteractionDrafts();
  for (const id of Object.keys(unread)) delete unread[id];
  projectID.value = "";
  taskID.value = "";
}
function applyData(data:{state:Snapshot;capabilities:{agent:boolean}}){
    for (const c of Object.values(data.state.conversations)) {
      const length = c.messages.reduce(
          (n, m) => n + (m.role === "assistant" ? m.text.length : 0),
          0,
        ),
        previous = lengths.get(c.id);
      if (previous !== undefined && length > previous && c.id !== local.current)
        unread[c.id] = 1;
      lengths.set(c.id, length);
    }
    mergeSnapshot(local, data.state);
    authenticated.value = true;
    agentAvailable.value = data.capabilities.agent;
    const projects = Object.values(data.state.projects);
    if (!projectID.value && projects.length) projectID.value = projects[0]!.id;
    if (projectID.value && !data.state.projects[projectID.value]) {
      error.value = "项目不存在，请重新选择";
      selectConversation(local, "");
    } else if (project.value) {
      const selected = taskID.value
        ? data.state.tasks[taskID.value]
        : undefined;
      if (
        taskID.value &&
        (!selected || selected.projectId !== projectID.value)
      ) {
        error.value = "任务链接已失效，未切换发送目标";
        selectConversation(local, "");
      } else
        selectConversation(
          local,
          selected?.conversationId ?? project.value.conversationId,
        );
    }
    if (!topics.value.some((t) => t.id === mobileTopic.value))
      mobileTopic.value = topics.value[0]?.id ?? "";
    online.value = true;
}
const refresh = createRefreshQueue(async () => {
  const epoch = authEpoch;
  try {
    if(ws?.open){await ws.request("sync",{after:streamID?(state.value?.revision??0):0});return}
    const data = await api<{
      state: Snapshot;
      capabilities: { agent: boolean };
    }>("snapshot");
    if (epoch !== authEpoch) return;
    applyData(data);
  } catch (e) {
    if (epoch === authEpoch) {
      if (!loaded.value && !authenticated.value && e instanceof APIError && e.status === 401) {
        clearIdentity();
      } else {
        report(e);
      }
      online.value = false;
    }
  } finally {
    loaded.value = true;
  }
});
function report(e: unknown) {
  error.value = e instanceof Error ? e.message : "操作失败";
  if (e instanceof APIError && e.status === 401) clearIdentity();
}
function connect() {
 if(stopped||!authenticated.value)return;
 ws?.close();const epoch=authEpoch;
 let client:SocketClient;
 client=new SocketClient((location.protocol==="https:"?"wss:":"ws:")+"//"+location.host+"/api/socket",frame=>{
  if(ws!==client||epoch!==authEpoch||!authenticated.value)return;
  try{
   if(frame.type==="snapshot"){if(streamID!==frame.streamId)local.snapshot=null;streamID=frame.streamId;applyData({state:frame.state,capabilities:frame.capabilities})}
   if(frame.type==="patch"&&state.value){
 if(streamID!==frame.streamId){void client.request("sync",{after:0});return}
    if(frame.revision<=state.value.revision)return;
    applyData({state:applyPatch(state.value,frame),capabilities:{agent:agentAvailable.value}});
   }
  }catch{void refresh()}
 },()=>{if(ws!==client||epoch!==authEpoch)return;online.value=true;void reconcile();void updateWatch()},event=>{
  if(ws!==client||stopped||!authenticated.value||epoch!==authEpoch)return;
  if(event.code===1008){clearIdentity();error.value="登录已失效，请重新输入令牌";return}
  online.value=false;
  void (async()=>{
   try{await httpAPI('session')}catch(e){
    if(ws!==client||stopped||epoch!==authEpoch)return;
    if(e instanceof APIError&&e.status===401){clearIdentity();error.value="登录已失效，请重新输入令牌";return}
   }
   if(ws===client&&!stopped&&authenticated.value&&epoch===authEpoch)retry=setTimeout(connect,1500);
  })();
 });
 ws=client;
 setSocketRequest((path,body)=>client.request(path,body));
}
async function login() {
  const epoch = ++authEpoch;
  loginBusy.value = true;
  error.value = "";
  try {
    await api("login", { token: token.value });
    if (epoch !== authEpoch) return;
    token.value = "";
    showToken.value = false;
    readLocation();
    await refresh();
    connect();
  } catch (e) {
    if (epoch === authEpoch) report(e);
  } finally {
    if (epoch === authEpoch) loginBusy.value = false;
  }
}
async function logout() {
  clearIdentity();
  clearInteractionDrafts();
  error.value = "";
  const epoch = authEpoch;
  history.replaceState(null, "", "/");
  try {
    await api("logout", {});
  } catch (e) {
    if (epoch === authEpoch) report(e);
  }
}
async function command(body: Record<string, string>): Promise<Receipt> {
  const epoch = authEpoch;
  if (!state.value) throw new Error("服务数据尚未就绪");
  const scope = body.type + ":" + (body.conversationId ?? "form");
  const signature = JSON.stringify(body);
  let operation = operations.get(scope);
  if (operation && operation.signature !== signature)
    throw new Error("上一操作结果尚未确认，请保留原输入并先重试确认");
  operation ??= {
    id: createOperationID(),
    signature,
    draftRevision:
      body.text === undefined
        ? undefined
        : local.drafts[body.conversationId ?? ""]?.revision,
  };
  operations.set(scope, operation);
  try {
    const receipt = await api<Receipt>("commands", {
      ...body,
      operationId: operation.id,
      revision: state.value.revision,
      structureRevision: state.value.structureRevision ?? 0,
    });
    if (epoch !== authEpoch) throw new Error("身份已切换");
    operations.delete(scope);
    await refresh();
    return receipt;
  } catch (e) {
    if (epoch !== authEpoch) throw e;
    if (e instanceof APIError && e.status === 0) {
      try {
        const receipt = await api<Receipt>("operations/" + operation.id);
        if (epoch !== authEpoch) throw new Error("身份已切换");
        operations.delete(scope);
        await refresh();
        return receipt;
      } catch {
        if (epoch !== authEpoch) throw new Error("身份已切换");
      }
    } else if (e instanceof APIError) operations.delete(scope);
    await refresh();
    throw e;
  }
}
async function reconcile(retryOriginal = false) {
  const epoch = authEpoch;
  await refresh();
  if (epoch !== authEpoch) return;
  for (const [scope, operation] of [...operations]) {
    try {
      let receipt: Receipt;
      try {
        receipt = await api<Receipt>("operations/" + operation.id);
      } catch (e) {
        if (epoch !== authEpoch) return;
        // 如果是 404，说明服务端尚未记录或已丢弃该 operationId
        if (e instanceof APIError && e.status === 404) {
          if (retryOriginal && state.value) {
            // 区分 delivery_accept / delivery_control / commands
            if (scope.startsWith("delivery_accept:")) {
              const body = JSON.parse(operation.signature);
              receipt = await api<Receipt>("delivery/accept", body);
            } else if (scope.startsWith("delivery_control:")) {
              const body = JSON.parse(operation.signature);
              receipt = await api<Receipt>("delivery/control", body);
            } else {
              const body = JSON.parse(operation.signature) as Record<string, string>;
              receipt = await api<Receipt>("commands", {
                ...body,
                operationId: operation.id,
                revision: state.value.revision,
                structureRevision: state.value.structureRevision ?? 0,
              });
            }
          } else {
            throw e;
          }
        } else {
          throw e;
        }
      }
      if (epoch !== authEpoch) return;
      operations.delete(scope);
      if (scope.startsWith("delivery_accept:") || scope.startsWith("delivery_control:")) {
        // delivery 相关的 operation 完成
      } else {
        const body = JSON.parse(operation.signature) as Record<string, string>;
        if (["answer_interaction", "cancel_interaction", "retry_interaction_answer"].includes(body.type)) {
          clearInteractionDraft(
            body.conversationId ?? "",
            body.requestId ?? "",
            Number(body.requestVersion ?? 0),
          );
        }
        if (body.type === "send_message" || body.type === "start_task") {
          const d = local.drafts[body.conversationId!];
          if (d && operation.draftRevision !== undefined)
            acceptSent(local, body.conversationId!, operation.draftRevision);
        }
      }
      error.value = receipt.notice ?? "";
      await refresh();
    } catch (e) {
      if (epoch !== authEpoch) return;
      if (e instanceof APIError && e.status === 404)
        error.value =
          "上一操作暂无回执，输入已保留；可点击确认，以原操作标识重试";
      else report(e);
    }
  }
}
function open(kind: string, topic = "") {
  dialog.value = kind;
  dialogTopic.value = topic || projectID.value + "_other";
  formError.value = "";
}
function openModels(c?: Conversation) {
  const isStarted = !!c && (c.messages.length > 0 || !!c.sessionFile || c.runState !== 'idle');
  modelTarget.value = c
    ? {
        scope: isStarted ? 'next' : c.id,
        projectId: c.projectId,
        conversationId: c.id,
        taskId: c.taskId,
        profile: { ...(c.pendingProfile?.model ? c.pendingProfile : c.profile.model ? c.profile : state.value?.profile) } as Profile,
      }
    : { scope: 'default', profile: state.value?.profile };
  modelError.value = "";
}
async function handleAnswerInteraction(payload: {
  conversationId: string;
  requestId: string;
  requestVersion: number;
  answerMode: "form" | "text";
  answers?: InteractionAnswers;
  others?: InteractionOthers;
  comment?: string;
  text?: string;
}) {
  // Capture the owning conversation from the emitted request, never the
  // possibly-later selected card.
  const conv = state.value?.conversations[payload.conversationId];
  if (!conv || !online.value) {
    error.value = "交互所属会话已变化，请重新选择后重试";
    return;
  }
  await dispatchInteractionCommand(
    {
      authEpoch,
      getCurrentEpoch: () => authEpoch,
      online: online.value,
      createOperationId: createOperationID,
      operations,
      localPending: local.pending,
      api,
      refresh,
      report,
      setError: (msg: string) => { error.value = msg; },
      getConversation: (id: string) => state.value?.conversations[id],
    },
    {
      type: "answer_interaction",
      projectId: conv.projectId,
      conversationId: conv.id,
      taskId: conv.taskId || "",
      requestId: payload.requestId,
      requestVersion: payload.requestVersion,
      answerMode: payload.answerMode,
      answers: payload.answers,
      others: payload.others,
      comment: payload.comment,
      text: payload.text,
    },
  );
}

async function handleCancelInteraction(payload: {
  conversationId: string;
  requestId: string;
  requestVersion: number;
}) {
  const conv = state.value?.conversations[payload.conversationId];
  if (!conv || !online.value) {
    error.value = "交互所属会话已变化，请重新选择后重试";
    return;
  }
  await dispatchInteractionCommand(
    {
      authEpoch,
      getCurrentEpoch: () => authEpoch,
      online: online.value,
      createOperationId: createOperationID,
      operations,
      localPending: local.pending,
      api,
      refresh,
      report,
      setError: (msg: string) => { error.value = msg; },
      getConversation: (id: string) => state.value?.conversations[id],
    },
    {
      type: "cancel_interaction",
      projectId: conv.projectId,
      conversationId: conv.id,
      taskId: conv.taskId || "",
      requestId: payload.requestId,
      requestVersion: payload.requestVersion,
    },
  );
}

async function handleRetryInteraction(payload: {
  conversationId: string;
  requestId: string;
  requestVersion: number;
}) {
  const conv = state.value?.conversations[payload.conversationId];
  if (!conv || !online.value) {
    error.value = "交互所属会话已变化，请重新选择后重试";
    return;
  }
  await dispatchInteractionCommand(
    {
      authEpoch,
      getCurrentEpoch: () => authEpoch,
      online: online.value,
      createOperationId: createOperationID,
      operations,
      localPending: local.pending,
      api,
      refresh,
      report,
      setError: (msg: string) => { error.value = msg; },
      getConversation: (id: string) => state.value?.conversations[id],
    },
    {
      type: "retry_interaction_answer",
      projectId: conv.projectId,
      conversationId: conv.id,
      taskId: conv.taskId || "",
      requestId: payload.requestId,
      requestVersion: payload.requestVersion,
    },
  );
}

async function saveModel(values: Record<string, string>) {
  const target = modelTarget.value, epoch = authEpoch;
  if (!target || modelSaving.value) return;
  modelSaving.value = true; modelError.value = "";
  try {
    const cmdType = target.scope === 'default'
      ? 'set_default_profile'
      : target.scope === 'next'
        ? 'set_next_profile'
        : 'set_conversation_profile';
    await command({
      ...values,
      type: cmdType,
      projectId: target.projectId ?? '',
      conversationId: target.conversationId ?? '',
      taskId: target.taskId ?? '',
    });
    if (epoch === authEpoch && modelTarget.value?.scope === target.scope) modelTarget.value = undefined;
  } catch (e) { if (epoch === authEpoch) modelError.value = e instanceof Error ? e.message : '模型保存失败'; }
  finally { if (epoch === authEpoch) modelSaving.value = false; }
}
async function saveDeliveryPolicy(draft: PolicyDraft, operationId: string) {
  const epoch = authEpoch;
  const currentProject = projectID.value;
  deliverySaving.value = true;
  deliveryError.value = "";
  try {
    const payload = buildSavePolicyPayload(operationId, draft);
    await api("delivery/policy", payload);
    if (epoch !== authEpoch) return;
    if (projectID.value === currentProject) {
      deliverySettingsOpen.value = false;
    }
    await refresh();
  } catch (e) {
    if (epoch === authEpoch) {
      deliveryError.value = e instanceof Error ? e.message : "保存交付配置失败";
    }
  } finally {
    if (epoch === authEpoch) deliverySaving.value = false;
  }
}

async function acceptDelivery(summary: DeliverySummary) {
  const epoch = authEpoch;
  if (!summary || !summary.id || !summary.acceptanceToken || !online.value) return;
  const scope = "delivery_accept:" + summary.id;
  const existing = operations.get(scope);
  if (existing) {
    // 已经有在途或未知状态的操作，用户再次点击时先只读查询 operations/{id}
    local.pending[summary.id] = true;
    error.value = "";
    try {
      await api<Receipt>("operations/" + existing.id);
      if (epoch !== authEpoch) return;
      operations.delete(scope);
      await refresh();
      return;
    } catch (e) {
      if (epoch !== authEpoch) return;
      if (e instanceof APIError && e.status === 404) {
        error.value = "上一操作暂无回执，未在服务端落地；可点击上方“确认上一操作”以原标识重试";
      } else if (e instanceof APIError && e.status === 0) {
        error.value = "网络仍未连通，无法核实上一操作结果";
      } else {
        operations.delete(scope);
        report(e);
      }
      return;
    } finally {
      if (epoch === authEpoch) local.pending[summary.id] = false;
    }
  }

  const operationId = createOperationID();
  const payload = buildAcceptPayload(
    operationId,
    summary.id,
    summary.acceptanceToken,
    summary.version,
  );
  operations.set(scope, { id: operationId, signature: JSON.stringify(payload) });
  local.pending[summary.id] = true;
  error.value = "";
  try {
    await api("delivery/accept", payload);
    if (epoch !== authEpoch) return;
    operations.delete(scope);
    await refresh();
  } catch (e) {
    if (epoch === authEpoch) {
      if (e instanceof APIError && e.status === 0) {
        // 网络中断导致结果未知：立即尝试通过 operations/{id} 查一次看是否已落地
        try {
          await api<Receipt>("operations/" + operationId);
          if (epoch !== authEpoch) return;
          operations.delete(scope);
          await refresh();
          return;
        } catch {
          if (epoch !== authEpoch) return;
          error.value = "验收操作结果未知，请检查网络或点击重试确认";
        }
      } else {
        operations.delete(scope);
        report(e);
      }
    }
  } finally {
    if (epoch === authEpoch) local.pending[summary.id] = false;
  }
}

async function controlDelivery(summary: DeliverySummary, action: 'pause' | 'resume' | 'cancel' | 'extend_budget') {
  const epoch = authEpoch;
  if (!summary || !summary.id || !online.value) return;
  const scope = "delivery_control:" + summary.id + ":" + action;
  const existing = operations.get(scope);
  if (existing) {
    // 已经有在途或未知状态的操作，用户再次点击时先只读查询 operations/{id}
    local.pending[summary.id] = true;
    error.value = "";
    try {
      await api<Receipt>("operations/" + existing.id);
      if (epoch !== authEpoch) return;
      operations.delete(scope);
      await refresh();
      return;
    } catch (e) {
      if (epoch !== authEpoch) return;
      if (e instanceof APIError && e.status === 404) {
        error.value = "上一操作暂无回执，未在服务端落地；可点击上方“确认上一操作”以原标识重试";
      } else if (e instanceof APIError && e.status === 0) {
        error.value = "网络仍未连通，无法核实上一操作结果";
      } else {
        operations.delete(scope);
        report(e);
      }
      return;
    } finally {
      if (epoch === authEpoch) local.pending[summary.id] = false;
    }
  }

  const operationId = createOperationID();
  const payload = buildControlPayload(
    operationId,
    summary.id,
    action,
    summary.version,
  );
  operations.set(scope, { id: operationId, signature: JSON.stringify(payload) });
  local.pending[summary.id] = true;
  error.value = "";
  try {
    await api("delivery/control", payload);
    if (epoch !== authEpoch) return;
    operations.delete(scope);
    await refresh();
  } catch (e) {
    if (epoch === authEpoch) {
      if (e instanceof APIError && e.status === 0) {
        // 网络中断导致结果未知：立即尝试通过 operations/{id} 查一次看是否已落地
        try {
          await api<Receipt>("operations/" + operationId);
          if (epoch !== authEpoch) return;
          operations.delete(scope);
          await refresh();
          return;
        } catch {
          if (epoch !== authEpoch) return;
          error.value = "控制动作结果未知，请检查网络或点击重试确认";
        }
      } else {
        operations.delete(scope);
        report(e);
      }
    }
  } finally {
    if (epoch === authEpoch) local.pending[summary.id] = false;
  }
}

async function save(values: Record<string, string>) {
  const epoch = authEpoch;
  saving.value = true;
  formError.value = "";
  try {
    const kind = dialog.value;
    const receipt = await command({
      ...values,
      type: (
        {
          project: "add_project",
          topic: "add_topic",
          task: "add_task",
          delete: "delete_topic",
          git: "update_project_git",
        } as Record<string, string>
      )[kind]!,
      projectId: kind === "project" ? "" : projectID.value,
      ...(kind === "delete" ? { topicId: dialogTopic.value } : {}),
    });
    if (epoch !== authEpoch) return;
    dialog.value = "";
    if (kind === "project" && receipt.createdId)
      chooseProject(receipt.createdId);
    if (kind === "topic" && receipt.createdId)
      mobileTopic.value = receipt.createdId;
    if (kind === "task" && receipt.createdId) {
      const created = state.value?.tasks[receipt.createdId];
      if (created) {
        mobileTopic.value = created.topicId;
        navigate(created);
      }
    }
    if (receipt.notice) error.value = receipt.notice;
    await nextTick();
    if (width.value < 1024 && kind === "task")
      document.querySelector<HTMLElement>("#chat-title")?.focus();
  } catch (e) {
    if (epoch === authEpoch)
      formError.value = e instanceof Error ? e.message : "保存失败";
  } finally {
    if (epoch === authEpoch) saving.value = false;
  }
}
async function send() {
  const epoch = authEpoch;
  const id = local.current,
    conv = conversation.value,
    d = local.drafts[id];
  if (d && isMergeRequest(d.text)) {
    const target = conv?.taskId ? state.value?.tasks[conv.taskId] : undefined;
    if (!target) {
      error.value = "请先选择一张任务卡片，再合并到主分支";
      return;
    }
    await openMerge(target, { conversationId: id, revision: d.revision });
    return;
  }
  if (d && isStartRequest(d.text)) {
    const target = conv?.taskId ? state.value?.tasks[conv.taskId] : undefined;
    if (!target) {
      error.value = "请先选择一张任务卡片，再开始开发";
      return;
    }
    await start(target, { text: d.text, revision: d.revision });
    return;
  }
  if (
    !conv ||
    !d?.text.trim() ||
    local.pending[id] ||
    !online.value ||
    !agentAvailable.value ||
    !conv.profile.provider ||
    !conv.profile.model ||
    ["starting", "running"].includes(conv.runState)
  )
    return;

  // 如果当前任务有进行中的自动交付（且未暂停），提示用户先暂停交付
  if (conv.taskId) {
    const delivery = state.value?.deliveries?.[conv.taskId];
    if (delivery && isDeliveryActive(delivery.phase) && delivery.phase !== "paused") {
      error.value = "自动交付正在管理该候选，请先暂停交付再发送新消息或接回会话";
      return;
    }
  }

  const text = d.text,
    revision = d.revision;
  local.pending[id] = true;
  error.value = "";
  try {
    await command({
      type: "send_message",
      projectId: conv.projectId,
      conversationId: id,
      taskId: conv.taskId ?? "",
      text,
    });
    if (epoch === authEpoch) acceptSent(local, id, revision);
  } catch (e) {
    if (epoch === authEpoch) report(e);
  } finally {
    if (epoch === authEpoch) local.pending[id] = false;
  }
}
async function start(target: Task, sent?: { text: string; revision: number }) {
  const epoch = authEpoch,
    id = target.conversationId;
  const c = state.value?.conversations[id];
  if (
    !c ||
    !online.value ||
    local.pending[id] ||
    !["idle", "failed", "interrupted"].includes(taskState(target,c,state.value?.executions?.[target.executionId??'']).status) ||
    ["starting", "running"].includes(c.runState)
  )
    return;
  navigate(target);
  if (!c.profile.provider || !c.profile.model) { openModels(c); return; }
  local.pending[id] = true;
  error.value = "";
  try {
    await command({
      type: "start_task",
      projectId: target.projectId,
      taskId: target.id,
      conversationId: id,
      ...(sent ? { text: sent.text } : {}),
    });
    if (epoch === authEpoch && sent) acceptSent(local, id, sent.revision);
  } catch (e) {
    if (epoch === authEpoch) report(e);
  } finally {
    if (epoch === authEpoch) local.pending[id] = false;
  }
}
async function resume() {
  const epoch = authEpoch;
  const c = conversation.value;
  if (
    !c ||
    local.pending[c.id] ||
    !agentAvailable.value ||
    !c.profile.provider ||
    !c.profile.model
  )
    return;
  local.pending[c.id] = true;
  try {
    await command({
      type: "resume",
      projectId: c.projectId,
      conversationId: c.id,
      taskId: c.taskId ?? "",
    });
  } catch (e) {
    if (epoch === authEpoch) report(e);
  } finally {
    if (epoch === authEpoch) local.pending[c.id] = false;
  }
}
async function updateWatch(){
 if(!ws?.open||!authenticated.value)return;
 try{await ws.request("watch",{projectId:projectID.value,visible:!document.hidden})}catch(e){if(e instanceof APIError&&e.status===401)report(e)}
}
async function pollGit(manual=false){
 if(!authenticated.value||!projectID.value||document.hidden||gitChecking||stopped||!ws?.open)return;
 const epoch=authEpoch;gitChecking=true;
 try{await api("git/refresh",{projectId:projectID.value});if(epoch===authEpoch)await refresh()}
 catch(e){if(epoch===authEpoch&&manual)report(e)}finally{gitChecking=false}
}
function visibility(){void updateWatch()}
watch([authenticated,projectID],()=>void updateWatch());
watch(()=>tasks.value.map(t=>t.id+":"+t.status).join(","),()=>void pollGit());
async function openMerge(
  target: Task,
  sent?: { conversationId: string; revision: number },
) {
  const epoch = authEpoch,
    scope = projectID.value,
    selection = taskID.value;
  error.value = "";
  try {
    const previous = Object.values(state.value?.merges ?? {})
      .filter(
        (op) =>
          op.taskId === target.id &&
          [
            "queued",
            "preparing",
            "committing",
            "committed",
            "merging",
            "needs_check",
          ].includes(op.phase),
      )
      .sort((a, b) => b.startedAt.localeCompare(a.startedAt))[0];
    const preview =
      previous?.preview ??
      (await api<MergePreview>("git/preview", {
        projectId: target.projectId,
        taskId: target.id,
      }));
    if (
      epoch !== authEpoch ||
      scope !== projectID.value ||
      selection !== taskID.value
    )
      return;
    mergeTarget.value = target;
    mergePreview.value = preview;
    mergeError.value = "";
    mergeSaving.value = false;
    mergeOpID.value = previous?.id ?? "";
    mergeRequest = undefined;
    mergeDraft = sent;
  } catch (e) {
    if (epoch === authEpoch && scope === projectID.value) report(e);
  }
}
function closeMerge() {
  mergePreview.value = undefined;
  mergeTarget.value = undefined;
  mergeError.value = "";
}
async function recheckMerge() {
  const target = mergeTarget.value;
  const sent = mergeDraft;
  closeMerge();
  if (target) await openMergeFresh(target, sent);
}
async function openMergeFresh(
  target: Task,
  sent?: { conversationId: string; revision: number },
) {
  const epoch = authEpoch,
    scope = projectID.value,
    selection = taskID.value;
  try {
    const preview = await api<MergePreview>("git/preview", {
      projectId: target.projectId,
      taskId: target.id,
    });
    if (
      epoch !== authEpoch ||
      scope !== projectID.value ||
      selection !== taskID.value
    )
      return;
    mergeTarget.value = target;
    mergePreview.value = preview;
    mergeError.value = "";
    mergeOpID.value = "";
    mergeRequest = undefined;
    mergeDraft = sent;
  } catch (e) {
    if (epoch === authEpoch) report(e);
  }
}
async function confirmMerge(message: string) {
  const preview = mergePreview.value,
    target = mergeTarget.value,
    epoch = authEpoch;
  if (!preview || !target || mergeSaving.value) return;
  mergeRequest ??= {
    operationId: createOperationID(),
    projectId: target.projectId,
    taskId: target.id,
    previewId: preview.id,
    commitMessage: message,
  };
  const request = { ...mergeRequest },
    sent = mergeDraft;
  mergeSaving.value = true;
  mergeError.value = "";
  try {
    let op: MergeOperation;
    try {
      op = await api<MergeOperation>("git/merge", request);
    } catch (e) {
      if (!(e instanceof APIError) || e.status !== 0) throw e;
      try {
        op = await api<MergeOperation>("git/operations/" + request.operationId);
      } catch {
        throw new APIError(
          0,
          "merge_uncertain",
          "尚未查到合并回执；请以原操作标识重试确认",
        );
      }
    }
    if (epoch !== authEpoch) return;
    if (sent) acceptSent(local, sent.conversationId, sent.revision);
    if (mergePreview.value?.id === preview.id) mergeOpID.value = op.id;
    await refresh();
    void pollGit();
  } catch (e) {
    if (epoch === authEpoch && mergePreview.value?.id === preview.id) {
      mergeError.value = e instanceof Error ? e.message : "合并请求未确认";
      if (e instanceof APIError && e.status !== 0) mergeRequest = undefined;
      report(e);
    }
  } finally {
    if (epoch === authEpoch) mergeSaving.value = false;
  }
}
function closeResult(){previewEpoch++;clearTimeout(previewTimer);resultTarget.value=undefined;taskPreview.value=undefined;previewBusy.value=false;previewError.value=''}
watch([projectID,taskID,()=>conversation.value?.rounds?.at(-1)?.id],(next,previous)=>{
 if(resultTarget.value&&(resultTarget.value.taskId!==taskID.value||resultTarget.value.projectId!==projectID.value||(next[2]!==previous[2]&&next[2]!==resultTarget.value.roundId)))closeResult();
});
function resultChat(){chatVisible.value=true;nextTick(()=>chatPanel.value?.focusEditor())}
function reviseResult(){closeResult();resultChat()}
function previewInstructions(){
 if(!task.value||resultTarget.value?.taskId!==task.value.id)return;
 const previous=local.drafts[local.current]?.text??'';
 setDraft(local,previous+(previous.trim()?'\n\n':'')+'请补充本任务的预览说明：网页任务请在当前独立工作区提供 kaiwu-preview.json，使用 {port} 和 127.0.0.1，说明启动命令及访问路径；非网页任务请提供使用方法和实际验证输出。不要自动启动服务。');
 closeResult();resultChat();
}
async function openResult(roundID:string){
 const selected=task.value;if(!selected)return;
 closeResult();resultTarget.value={projectId:selected.projectId,taskId:selected.id,roundId:roundID};
 chatVisible.value=false;
 await loadTaskPreview('status',true);
}
async function loadTaskPreview(action:'status'|'start'|'stop'='status',forceOpen=false){
 const target=resultTarget.value;if(!target||previewBusy.value||!online.value)return;
 clearTimeout(previewTimer);const epoch=previewEpoch;previewBusy.value=true;previewError.value='';
 try{
  const previousURL=taskPreview.value?.state==='running'?taskPreview.value.url:undefined;
  const manifestId=action==='start'?taskPreview.value?.manifestId:undefined;
  if(action==='start'&&!manifestId)throw new Error('预览配置缺少确认指纹，请先刷新状态并核对命令。');
  const result=await api<TaskPreview>('previews/'+action,{...target,...(action==='status'?{}:{operationId:createOperationID()}),...(action==='start'?{manifestId}:{})});
  if(epoch!==previewEpoch)return;
  if(result.taskId!==target.taskId||result.roundId!==target.roundId)throw new Error('成果目标已变化，请重新打开任务结果。');
  if(result.state==='running'){
   if(!forceOpen&&safePreviewURL(previousURL))result.url=previousURL;
   else{
    const opened=await api<TaskPreview>('previews/open',target);
    if(epoch!==previewEpoch)return;
    if(opened.taskId!==target.taskId||opened.roundId!==target.roundId)throw new Error('预览入口与当前成果不匹配。');
    if(opened.state==='running'&&!safePreviewURL(opened.url))throw new Error('服务器未提供有效的受限预览入口。');
    Object.assign(result,opened);
   }
  }
  taskPreview.value=result;
 }catch(e){if(epoch===previewEpoch)previewError.value=e instanceof Error?e.message:'未能读取预览状态'}
 finally{
  if(epoch===previewEpoch){previewBusy.value=false;if(resultTarget.value&&online.value&&document.visibilityState==='visible')previewTimer=setTimeout(()=>void loadTaskPreview(),3000)}
 }
}
watch(online,value=>{if(value&&resultTarget.value)void loadTaskPreview()});
function previewVisibility(){if(document.visibilityState==='visible'&&resultTarget.value)void loadTaskPreview();else clearTimeout(previewTimer)}
async function confirmResult(roundID:string){
 const target=task.value;if(!target)return;
 try{await command({type:"confirm_result",projectId:target.projectId,taskId:target.id,conversationId:target.conversationId,roundId:roundID})}catch(e){report(e)}
}
function beforeUnload(event: BeforeUnloadEvent) {
  if (Object.values(local.drafts).some((d) => d.text.trim())) {
    event.preventDefault();
    event.returnValue = "";
  }
}
function resize() {
  width.value = innerWidth;
}
function pop() {
  readLocation();
  void refresh();
}
onMounted(async () => {
  readLocation();
  document.addEventListener("visibilitychange",previewVisibility);
  window.addEventListener("beforeunload", beforeUnload);
  window.addEventListener("resize", resize);
  window.addEventListener("popstate", pop);
  document.addEventListener("visibilitychange", visibility);
  await refresh();
  connect();
});
onBeforeUnmount(() => {
  stopped = true;
  setSocketRequest(undefined);
  clearTimeout(retry);
  clearTimeout(gitTimer);
  document.removeEventListener("visibilitychange", visibility);
  ws?.close();
  closeResult();document.removeEventListener("visibilitychange",previewVisibility);
  window.removeEventListener("beforeunload", beforeUnload);
  window.removeEventListener("resize", resize);
  window.removeEventListener("popstate", pop);
});
</script>
<template>
  <div v-if="!loaded" class="entry"><p>正在连接开悟……</p></div>
  <main v-else-if="!authenticated" class="login-page">
    <div class="entry">
      <div class="brand" aria-label="开悟">
        <span class="mark" aria-hidden="true">悟</span>
        <span class="brand-name">开悟</span>
      </div>
      <section class="card" aria-labelledby="heading">
        <h1 id="heading">进入你的工作台</h1>
        <p class="intro">从想法到作品，在创造中更新认知。</p>
        <form class="login-form" @submit.prevent="login">
          <label for="token">本机访问令牌</label>
          <div class="input-wrap">
            <input
              id="token"
              v-model="token"
              :type="showToken ? 'text' : 'password'"
              placeholder="输入本机访问令牌"
              autocomplete="current-password"
              aria-describedby="token-help"
              required
              autofocus
            />
            <button
              class="reveal"
              type="button"
              :aria-label="showToken ? '隐藏令牌' : '显示令牌'"
              :aria-pressed="showToken ? 'true' : 'false'"
              @click="showToken = !showToken"
            >
              <svg v-if="!showToken" viewBox="0 0 24 24" aria-hidden="true">
                <path d="M2 12s3.5-6 10-6 10 6 10 6-3.5 6-10 6S2 12 2 12Z"/>
                <circle cx="12" cy="12" r="2.5"/>
              </svg>
              <svg v-else viewBox="0 0 24 24" aria-hidden="true">
                <path d="M9.88 9.88a3 3 0 1 0 4.24 4.24m-2.12-2.12a3 3 0 0 1-2.12-2.12M10.73 5.08A10.43 10.43 0 0 1 12 5c7 0 10 7 10 7a13.16 13.16 0 0 1-1.67 2.68M6.61 6.61A13.526 13.526 0 0 0 2 12s3 7 10 7a9.74 9.74 0 0 0 5.39-1.61M2 2l20 20"/>
              </svg>
            </button>
          </div>
          <p v-if="error" class="login-error" role="alert">{{ error }}</p>
          <button class="submit" type="submit" :disabled="loginBusy">
            {{ loginBusy ? "连接中…" : "进入工作台" }}
            <svg v-if="!loginBusy" viewBox="0 0 24 24" aria-hidden="true">
              <path d="M5 12h14m-5-5 5 5-5 5"/>
            </svg>
          </button>
        </form>
        <p class="remember">
          <svg viewBox="0 0 24 24" aria-hidden="true">
            <rect x="6" y="10" width="12" height="10" rx="2"/>
            <path d="M8 10V7a4 4 0 0 1 8 0v3"/>
          </svg>
          登录保留 30 天，退出后清除
        </p>
        <div class="divider"></div>
        <p class="help" id="token-help">
          <strong>访问令牌在哪里？</strong><br />使用启动服务时设置的本机访问令牌。
        </p>
      </section>
      <p class="footer">本地优先，只连接你自己的开发环境</p>
    </div>
  </main>
  <template v-else
    ><header class="top">
      <span class="logo">悟</span><span class="brand">开悟</span
      ><span class="slash">/</span
      ><ProjectMenu
        :projects="Object.values(state?.projects ?? {})"
        :current-id="projectID"
        @choose="chooseProject"
        @add="open('project')"
        @git="open('git')"
        @delivery="deliverySettingsOpen = true"
        @refresh="pollGit(true)"
      />
      <div class="global-actions" role="group" aria-label="全局操作">
        <VersionBadge />
        <button aria-label="默认模型" title="为新项目和任务选择默认模型" @click="openModels()">默认模型</button>
        <span class="connection" :class="{ off: !online }" role="status">{{
          online ? "● 已连接" : "○ 正在重连"
        }}</span>
        <button class="logout" @click="logout">退出</button>
      </div>
    </header>
    <div v-if="error || operations.size" class="global-error" role="alert">
      {{ error || "正在确认操作结果"
      }}<button
        v-if="operations.size"
        :disabled="!online"
        @click="reconcile(true)"
      >
        确认上一操作</button
      ><button @click="error = ''">关闭提示</button>
    </div>
    <div v-if="!project" class="entry">
      <div>
        <h1>从你的第一个项目开始</h1>
        <p>添加服务器上的项目目录，也可初始化新 Git 项目。</p>
        <button class="primary" @click="open('project')">＋ 添加项目</button>
      </div>
    </div>
    <template v-else
      ><nav class="mobile-switch" aria-label="看板与聊天">
        <button :aria-pressed="!chatVisible" @click="chatVisible = false">
          ▦ 看板</button
        ><button :aria-pressed="chatVisible" @click="chatVisible = true">
          ◌ 聊天
        </button>
      </nav>
      <div class="app" :class="{ 'chat-visible': chatVisible, 'result-visible': !!resultTarget }">
        <ResultPreview v-if="resultTarget&&task&&conversation" :task="task" :conversation="conversation" :execution="task.executionId?state?.executions?.[task.executionId]:undefined" :round-id="resultTarget.roundId" :preview="taskPreview" :busy="previewBusy" :error="previewError" :online="online" @back="closeResult();chatVisible=false" @chat="resultChat" @refresh="loadTaskPreview('status',true)" @start="loadTaskPreview('start',true)" @stop="loadTaskPreview('stop')" @revise="reviseResult" @instructions="previewInstructions"/>
        <KanbanBoard v-else
          :executions="state?.executions??{}"
          :git="state?.taskGit ?? {}"
          :deliveries="state?.deliveries"
          :topics="topics"
          :tasks="tasks"
          :unread="unread"
          :selected="taskID"
          :mobile-topic="mobileTopic"
          :online="online"
          :pending="local.pending"
          :conversations="state?.conversations ?? {}"
          @select="navigate"
          @start="start($event)"
          @merge="openMerge($event)"
          @topic="mobileTopic = $event"
          @add-topic="open('topic')"
          @add-task="open('task', $event)"
          @delete-topic="open('delete', $event.id)"
        /><ChatPanel
          v-if="conversation"
          :key="'chat-panel'"
          ref="chatPanel"
          :conversation="conversation"
          :task="task"
          :git="task ? state?.taskGit?.[task.id] : undefined"
          :execution="
            task?.executionId
              ? state?.executions?.[task.executionId]
              : undefined
          "
          :delivery="task ? state?.deliveries?.[task.id] : undefined"
          :project-name="project.name"
          :draft="draft"
          :pending="!!local.pending[local.current] || (task ? !!local.pending[state?.deliveries?.[task.id]?.id ?? ''] : false)"
          :online="online"
          :agent-available="
            agentAvailable &&
            !!conversation.profile.provider &&
            !!conversation.profile.model
          "
          :visible="width >= 1024 || chatVisible"
          @draft="setDraft(local, $event)"
          @send="send"
          @project="navigate()"
          @create="open('task')"
          @resume="resume"
          @start="task && start(task)"
          @merge="task && openMerge(task)"
          :tasks="tasks"
          @confirm-result="confirmResult"
          @preview-result="openResult"
          @model="openModels(conversation)"
          @accept-delivery="acceptDelivery"
          @pause-delivery="s => controlDelivery(s, 'pause')"
          @resume-delivery="s => controlDelivery(s, 'resume')"
          @cancel-delivery="s => controlDelivery(s, 'cancel')"
          @extend-budget-delivery="s => controlDelivery(s, 'extend_budget')"
          @answer-interaction="handleAnswerInteraction"
          @cancel-interaction="handleCancelInteraction"
          @retry-interaction="handleRetryInteraction"
        />
        <aside v-else class="chat">
          <p class="error">聊天目标无效。请显式选择项目或任务。</p>
          <button @click="navigate()">返回项目对话</button>
        </aside>
      </div></template
    >
    <MergeDialog
      v-if="mergePreview && mergeTarget"
      :key="mergePreview.id"
      :preview="mergePreview"
      :task-title="mergeTarget.title"
      :saving="mergeSaving"
      :operation="mergeOperation"
      :error="mergeError"
      @close="closeMerge"
      @confirm="confirmMerge"
      @recheck="recheckMerge"
    />
    <ModelDialog v-if="modelTarget" :key="modelTarget.scope" :scope="modelTarget.scope" :profile="modelTarget.profile" :saving="modelSaving" :error="modelError" @close="modelTarget = undefined" @submit="saveModel" />
    <DeliverySettings
      v-if="deliverySettingsOpen && project"
      :key="'delivery-settings-' + project.id"
      :project="project"
      :saving="deliverySaving"
      :error="deliveryError"
      @close="deliverySettingsOpen = false"
      @save="saveDeliveryPolicy"
    />
    <TaskDialog
      v-if="dialog"
      :kind="dialog"
      :project="project"
      :topic-i-d="dialogTopic"
      :topics="topics"
      :error="formError"
      :saving="saving"
      @close="dialog = ''"
      @submit="save"
    />
  </template>
</template>
