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
import ProjectTools from "./ProjectTools.vue";
import {addDraftAttachments,removeDraftAttachment,uploadAttachment} from "./attachments.ts";
import KanbanBoard from "./KanbanBoard.vue";
import ProjectMenu from "./ProjectMenu.vue";
import ChatPanel from "./ChatPanel.vue";
import CompleteTaskDialog from './CompleteTaskDialog.vue';
import {createCompletionIntent,type CompletionIntent} from './task-completion.ts';
import ChatDrawer from "./ChatDrawer.vue";
import ResultPreview from "./ResultPreview.vue";
import {safePreviewURL, preserveArtifactLinks, isWebPreviewable, isManifestApproved, recordManifestApproval, clearManifestApprovals} from "./preview.ts";
import {
  createPreviewIntent,
  beginPreviewIntent,
  invalidatePreviewIntent,
  isPreviewIntentCurrent,
  markPreviewNavigated,
  releasePreviewIntent,
  previewIntentKey,
  previewStartOperation,
  peekPreviewStart,
  clearPreviewCompletion,
  clearPreviewStarts,
  settlePreviewStart,
  reconcilePreviewStart,
  type PreviewScope,
} from "./preview-intent.ts";
import TaskDialog from "./TaskDialog.vue";
import MergeDialog from "./MergeDialog.vue";
import { createMergeIntent, invalidateMergeIntent, beginMerge, settleMerge, rejectMerge, reconcileMerge, uncertainMergeOperation, reviewedRoundId, isPushOnlyReview } from "./merge-intent.ts";
import { mergeFinished } from "./merge.ts";
import ModelDialog from "./ModelDialog.vue";
import DeliverySettings from "./DeliverySettings.vue";
import VersionBadge from "./VersionBadge.vue";
import ThemePicker from "./ThemePicker.vue";
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
  MergeReview,
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
const completionTarget=ref<Readonly<CompletionIntent>>();
const completionBusy=ref(false),completionError=ref('');
function completionFor(t:Task){return createCompletionIntent({task:t,conversation:state.value?.conversations[t.conversationId],execution:state.value?.executions?.[t.executionId??''],delivery:state.value?.deliveries?.[t.id],online:online.value,pending:!!local.pending[t.conversationId]})}
const completionEligible=computed(()=>{const target=completionTarget.value;if(!target)return false;const t=state.value?.tasks[target.taskId];return !!t&&completionFor(t)?.roundId===target.roundId});
function requestCompletion(t:Task){const target=completionFor(t);if(!target)return;completionTarget.value=target;completionError.value=''}
async function finishCompletion(){const target=completionTarget.value,epoch=authEpoch;if(!target||!completionEligible.value||completionBusy.value)return;completionBusy.value=true;completionError.value='';try{await command({type:'confirm_result',projectId:target.projectId,taskId:target.taskId,conversationId:target.conversationId,roundId:target.roundId});if(epoch===authEpoch&&completionTarget.value===target)completionTarget.value=undefined}catch(e){if(epoch===authEpoch&&completionTarget.value===target)completionError.value=e instanceof Error?e.message:String(e)}finally{if(epoch===authEpoch)completionBusy.value=false}}
const settingsOpen=ref(false);
const mainView=ref<"board"|"files"|"terminal">("board");
const uploading=reactive<Record<string,boolean>>({});
const chatPanel=ref<InstanceType<typeof ChatPanel>>();
const resultTarget=ref<{projectId:string;taskId:string;roundId:string}>();
const taskPreview=ref<TaskPreview>();
const previewBusy=ref(false),previewError=ref('');
let previewEpoch=0,previewTimer:ReturnType<typeof setTimeout>|undefined;
const mergePreview = ref<MergePreview>();
const mergeReview = ref<MergeReview>();
const mergeReceipt = ref<MergeOperation>();
const mergeStale = ref(false);
let mergeEpoch = 0;
let mergeIntent = createMergeIntent();
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
      mode: "local" | "push";
      roundId: string;
    }
  | undefined;
let mergeDraft: { conversationId: string; revision: number } | undefined;
const mergeOperation = computed(() => state.value?.merges?.[mergeOpID.value] ?? mergeReceipt.value);
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
  chatVisible.value = true;
}
async function processInteraction(task: Task) {
  navigate(task);
  await nextTick();
  await chatPanel.value?.openInteraction();
}
function chooseProject(id: string) {
  projectID.value = id;
  taskID.value = "";
  mobileTopic.value = "";
  navigate();
  chatVisible.value = false;
}
function clearIdentity() {
  completionTarget.value=undefined;completionBusy.value=false;completionError.value='';
  clearManifestApprovals();
  invalidatePreview('auth');
  previewBlockedURL.value = '';
  closeResult();
  authEpoch++;
 streamID="";
  authenticated.value = false;
  chatVisible.value = false;
  closeResult();
  clearManifestApprovals();
  online.value = false;
  loginBusy.value = false;
  showToken.value = false;
  saving.value = false;
  dialog.value = "";
  modelTarget.value = undefined;
  modelSaving.value = false;
  modelError.value = "";
  closeMerge();
  mergeIntent = createMergeIntent();
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
async function command(body: Record<string, unknown> & {conversationId?:string;text?:string}): Promise<Receipt> {
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
async function attachFiles(files:File[],target=local.current){
 const id=target,epoch=authEpoch;if(!id||uploading[id])return;
 if((local.drafts[id]?.attachments?.length??0)+files.length>8){error.value="每条消息最多 8 个附件";return}
 if(files.some(f=>f.size>8*1024*1024)){error.value="单个附件不能超过 8 MiB";return}
 uploading[id]=true;
 try{for(const file of files){if(epoch!==authEpoch)break;const item=await uploadAttachment(id,file);if(epoch===authEpoch)addDraftAttachments(local,id,[item])}}catch(e){if(epoch===authEpoch)report(e)}finally{if(epoch===authEpoch)uploading[id]=false}
}
async function screenshot(){
 const target=local.current,epoch=authEpoch;
 let stream:MediaStream|undefined;
 try{
  if(!navigator.mediaDevices?.getDisplayMedia)throw new Error("当前浏览器不支持截图；请粘贴截图或选择图片。浏览器截图需要 HTTPS 或 localhost。");
  stream=await navigator.mediaDevices.getDisplayMedia({video:true});
  const video=document.createElement('video');video.srcObject=stream;await video.play();
  const canvas=document.createElement('canvas');canvas.width=video.videoWidth;canvas.height=video.videoHeight;canvas.getContext('2d')?.drawImage(video,0,0);
  const blob=await new Promise<Blob|null>(resolve=>canvas.toBlob(resolve,'image/png'));
  if(blob&&epoch===authEpoch)await attachFiles([new File([blob],'截图.png',{type:'image/png'})],target);
 }catch(e){report(e)}finally{stream?.getTracks().forEach(track=>track.stop())}
}
// Explicit retry is available only for definitely unsent messages. Unknown
// delivery is never replayed; keep any in-progress draft untouched.
async function retryMessage(messageId: string) {
 const c = conversation.value;
 if (!c || !online.value || !agentAvailable.value || local.pending[c.id] || uploading[c.id] ||
   ['starting', 'running'].includes(c.runState)) return;
 const d = local.drafts[c.id];
 if (d?.text.trim() || d?.attachments?.length) return;
 const message = c.messages.find(m => m.id === messageId && m.role === 'user' && m.delivery === 'not_sent');
 if (!message) return;
 setDraft(local, message.text);
 if (message.attachments?.length) addDraftAttachments(local, c.id, message.attachments);
 await send();
}
async function send() {
  const epoch = authEpoch;
  const id = local.current,
    conv = conversation.value,
    d = local.drafts[id];
  if (d && !d.attachments?.length && isMergeRequest(d.text)) {
    const target = conv?.taskId ? state.value?.tasks[conv.taskId] : undefined;
    if (!target) {
      error.value = "请先选择一张任务卡片，再合并到主分支";
      return;
    }
    await openMerge(target, { conversationId: id, revision: d.revision });
    return;
  }
  if (d && !d.attachments?.length && isStartRequest(d.text)) {
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
    (!d?.text.trim()&&!d?.attachments?.length) ||
    uploading[id] ||
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
      attachments:d.attachments??[],
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
function mergeStamp(target: Task): string {
  const round = state.value?.conversations[target.conversationId]?.rounds?.at(-1);
  const git = state.value?.taskGit?.[target.id];
  return JSON.stringify([projectID.value, taskID.value, round?.id, git?.state, git?.mainBranch, git?.mainSHA, git?.taskSHA, git?.dirtyFiles]);
}
function closeMerge() {
  mergeEpoch++;
  invalidateMergeIntent(mergeIntent); // retain uncertain writes, even across close/ABA
  mergePreview.value = undefined;
  mergeReview.value = undefined;
  mergeTarget.value = undefined;
  mergeReceipt.value = undefined;
  mergeOpID.value = "";
  mergeRequest = undefined;
  mergeError.value = "";
  mergeSaving.value = false;
  mergeStale.value = false;
}
function applyMergeReceipt(op: MergeOperation) {
  mergeReceipt.value = op;
  mergeOpID.value = op.id;
  reconcileMerge(mergeIntent, { projectId: op.projectId, taskId: op.taskId, roundId: op.roundId ?? "", mode: op.mode === "push" ? "push" : "local" }, true);
}
async function openMerge(target: Task, sent?: { conversationId: string; revision: number }) {
  closeMerge();
  const generation = mergeEpoch, epoch = authEpoch, stamp = mergeStamp(target);
  const current = () => generation === mergeEpoch && epoch === authEpoch && stamp === mergeStamp(target);
  error.value = "";
  try {
    const uncertain = uncertainMergeOperation(mergeIntent, { projectId: target.projectId, taskId: target.id });
    let previous = Object.values(state.value?.merges ?? {})
      .filter(op => op.taskId === target.id && (!mergeFinished(op.phase) || op.phase === "needs_check"))
      .sort((a, b) => b.startedAt.localeCompare(a.startedAt))[0];
    if (uncertain) {
      // 404 is not proof that a delayed original request can never arrive.
      try { previous = await api<MergeOperation>("git/operations/" + uncertain); }
      catch { throw new Error("合并回执尚未确认；只能稍后只读核查，不会重新发送操作。"); }
      if (previous.id !== uncertain || previous.taskId !== target.id || previous.projectId !== target.projectId) throw new Error("合并回执不匹配");
    }
    if (previous) {
      if (!current()) return;
      mergeTarget.value = target;
      mergePreview.value = previous.preview;
      applyMergeReceipt(previous);
      mergeDraft = sent;
      return;
    }
    const conv = state.value?.conversations[target.conversationId];
    const execution = state.value?.executions?.[target.executionId ?? ""];
    const roundId = reviewedRoundId(conv, execution);
    if (!roundId) throw new Error("请等待最新一轮执行结束后再审核合并");
    const body = { projectId: target.projectId, taskId: target.id, roundId };
    const preview = await api<MergePreview>("git/preview", { ...body, mode: "local" });
    if (!current()) return;
    let review: MergeReview | undefined, remoteError = "";
    try { review = await api<MergeReview>("git/preview", { ...body, mode: "push" }); }
    catch (e) {
      if (e instanceof APIError && e.status === 401) throw e;
      remoteError = "远程审核不可用：" + (e instanceof Error ? e.message : "请稍后重试");
    }
    if (!current()) return;
    if (preview.roundId !== roundId || preview.mode !== "local" || (review && (review.roundId !== roundId || review.mode !== "push" || review.fingerprint !== preview.fingerprint))) throw new Error("审核期间成果或 Git 状态已变化，请重新审核");
    mergeTarget.value = target;
    mergePreview.value = preview;
    mergeReview.value = review;
    mergeError.value = remoteError;
    mergeDraft = sent;
  } catch (e) { if (current()) report(e); }
}
async function recheckMerge() {
  const target = mergeTarget.value;
  if (!target || mergeSaving.value) return;
  const op = mergeOperation.value;
  if (op && (!mergeFinished(op.phase) || op.phase === "needs_check")) {
    const generation = mergeEpoch;
    try {
      const fresh = await api<MergeOperation>("git/operations/" + op.id);
      if (generation === mergeEpoch) { applyMergeReceipt(fresh); mergeError.value = ""; }
    } catch (e) { if (generation === mergeEpoch) mergeError.value = e instanceof Error ? e.message : "只读核查失败"; }
    return;
  }
  await openMerge(target, mergeDraft);
}
async function confirmMerge(message: string, mode: "local" | "push") {
  const target = mergeTarget.value;
  const preview = mode === "push" ? mergeReview.value : mergePreview.value;
  if (!target || !preview?.roundId || mergeSaving.value || mergeStale.value || mergeOperation.value || (mode === "push" ? !mergeReview.value?.canPush : !preview.canMerge)) return;
  const scope = { projectId: target.projectId, taskId: target.id, roundId: preview.roundId, mode };
  const began = beginMerge(mergeIntent, scope, createOperationID);
  if (began.action === "wait") return;
  if (began.action === "reconcile") { await openMerge(target, mergeDraft); return; }
  const epoch = authEpoch, generation = mergeEpoch, intentEpoch = mergeIntent.epoch;
  const request = { ...scope, operationId: began.operationId, previewId: preview.id, commitMessage: message };
  mergeRequest = request;
  const sent = mergeDraft;
  mergeSaving.value = true;
  mergeError.value = "";
  try {
    const op = await api<MergeOperation>("git/merge", request);
    if (epoch !== authEpoch) return;
    if (op.id !== request.operationId || op.taskId !== target.id || op.mode !== mode || op.roundId !== scope.roundId) throw new Error("合并回执不匹配，请只读核查");
    settleMerge(mergeIntent, intentEpoch, scope, { operationId: op.id, ok: true });
    if (sent) acceptSent(local, sent.conversationId, sent.revision);
    if (generation === mergeEpoch) applyMergeReceipt(op);
    await refresh();
    void pollGit();
  } catch (e) {
    if (epoch !== authEpoch) return;
    const rejected = e instanceof APIError && [400, 403, 409, 422].includes(e.status);
    if (rejected) rejectMerge(mergeIntent, intentEpoch, scope);
    else settleMerge(mergeIntent, intentEpoch, scope, undefined);
    if (generation !== mergeEpoch) return;
    mergeStale.value = true;
    mergeError.value = rejected ? "审核已失效，请重新审核后显式确认：" + (e as Error).message : "操作回执未知；重新检查只查询原操作，不会再次提交或推送。";
    if (e instanceof APIError && e.status === 401) report(e);
  } finally { if (epoch === authEpoch && generation === mergeEpoch) mergeSaving.value = false; }
}
function closeResult(keepBlockedNotice = false){
  // Closing the result view is a user-visible cancel: the pending gesture must
  // not navigate its reserved blank afterwards.
  invalidatePreview('close');
  previewEpoch++;
  clearTimeout(previewTimer);
  resultTarget.value=undefined;
  taskPreview.value=undefined;
  previewBusy.value=false;
  previewError.value='';
  if (!keepBlockedNotice) previewBlockedURL.value='';
}
watch([projectID,taskID,()=>conversation.value?.rounds?.at(-1)?.id],(next,previous)=>{
 // Any selection change, task switch or new latest round invalidates the live
 // preview intent — even with no result panel open, and even if the values later
 // look identical again (A → B → A must never revive a late receipt).
 // Confirming a legacy result may materialize the same bound round, not a new intent.
 const legacyAcknowledged = mergeSaving.value && !previous[2] && next[2] === mergeRequest?.roundId && next[0] === previous[0] && next[1] === previous[1];
 if (!legacyAcknowledged) closeMerge();
 invalidatePreview(next[1]!==previous[1]||next[0]!==previous[0]?'task':'round');
 if(resultTarget.value&&(resultTarget.value.taskId!==taskID.value||resultTarget.value.projectId!==projectID.value||(next[2]!==previous[2]&&next[2]!==resultTarget.value.roundId)))closeResult();
 if(previewBlockedURL.value&&(taskID.value!==previous[1]||projectID.value!==previous[0]||next[2]!==previous[2]))previewBlockedURL.value='';
},{flush:'sync'});
watch(() => mergeTarget.value ? mergeStamp(mergeTarget.value) : "", (now, before) => {
  if (before && now !== before && !mergeSaving.value && !mergeOperation.value && (!mergeTarget.value || !uncertainMergeOperation(mergeIntent, { projectId: mergeTarget.value.projectId, taskId: mergeTarget.value.id }))) closeMerge();
}, { flush: 'sync' });
function resultChat(){chatVisible.value=true;nextTick(()=>chatPanel.value?.focusEditor())}
function reviseResult(){closeResult();resultChat()}
function previewInstructions(){
 if(!task.value||resultTarget.value?.taskId!==task.value.id)return;
 const previous=local.drafts[local.current]?.text??'';
 setDraft(local,previous+(previous.trim()?'\n\n':'')+'请补充本任务的预览说明：网页任务请在当前独立工作区提供 kaiwu-preview.json，使用 {port} 和 127.0.0.1，说明启动命令及访问路径；非网页任务请提供使用方法和实际验证输出。不要自动启动服务。');
 closeResult();resultChat();
}
// One unified preview intent owns the reserved blank tab, the monotonic
// generation and the per-gesture operation id. Selection / task / latest round /
// auth changes invalidate it even when no result panel is open.
const previewIntent = createPreviewIntent({
  createOperationId: createOperationID,
  openBlank: () => {
    let win: Window | null = null;
    try {
      win = window.open('about:blank', '_blank');
      if (win) win.opener = null;
    } catch { win = null; }
    return win;
  },
});
const previewBlockedURL = ref<string>("");
function currentPreviewScope(): PreviewScope | undefined {
  const selected = task.value;
  const roundId = conversation.value?.rounds?.at(-1)?.id;
  if (!selected || !roundId) return;
  return { projectId: selected.projectId, taskId: selected.id, roundId };
}
function isPreviewScopeLive(scope: PreviewScope): boolean {
  const now = currentPreviewScope();
  return !!now && now.projectId === scope.projectId && now.taskId === scope.taskId && now.roundId === scope.roundId;
}
function invalidatePreview(reason: string) {
  invalidatePreviewIntent(previewIntent, reason);
  // Auth change / unmount: these operation receipts belong to a login session
  // that is over. A plain selection change keeps them so an unknown start can
  // still be reconciled read-only later in the same login.
  if (reason === 'auth' || reason === 'unmount') clearPreviewStarts(previewIntent);
}

async function runPreviewFlow(
  target: { projectId: string; taskId: string; roundId: string },
  explicitStart = false
) {
  if (!target) return;
  const scope: PreviewScope = { ...target };
  // A historical round only ever shows the stored record: the current worktree
  // can no longer reproduce it, so no Status/Open/Start is issued at all.
  const isHistorical = target.roundId !== conversation.value?.rounds?.at(-1)?.id;
  if (isHistorical) {
    invalidatePreview('historical');
    closeResult();
    resultTarget.value = target;
    chatVisible.value = false;
    return;
  }

  // One gesture binds one intent: it dedupes in-flight repeats, late repeats just
  // after completion, and owns exactly one pre-opened blank tab.
  const intent = beginPreviewIntent(previewIntent, {
    scope,
    gesture: explicitStart ? 'start' : 'open',
  });
  if (!intent) return;
  const previewWindow = intent.blank ?? null;
  let navigated = false;
  previewBlockedURL.value = "";

  function stillCurrent(res?: TaskPreview): boolean {
    if (!intent) return false;
    if (!isPreviewScopeLive(scope)) return false;
    return isPreviewIntentCurrent(previewIntent, intent, currentPreviewScope() ?? scope, res);
  }

  function discardBlank() {
    if (previewWindow && !previewWindow.closed && !navigated) previewWindow.close();
  }

  function navigateOrBlock(url: string | undefined): boolean {
    const safeUrl = safePreviewURL(url);
    if (!safeUrl) return false;
    if (previewWindow && !previewWindow.closed) {
      previewWindow.location.href = safeUrl;
      markPreviewNavigated(intent!);
      navigated = true;
      return true;
    }
    // Popup was blocked: keep the notice, never drop the verified URL.
    closeResult(true);
    resultTarget.value = target;
    chatVisible.value = false;
    previewBlockedURL.value = safeUrl;
    navigated = true;
    return true;
  }

  // Stable operation id per (scope + manifest + start). Reusing it means an
  // unknown receipt can be reconciled read-only and retried idempotently.
  function startOperationId(manifestId: string): string | undefined {
    const key = previewIntentKey(scope) + ':' + manifestId;
    const decision = previewStartOperation(previewIntent, key);
    if (decision.action === 'write') return decision.operationId;
    if (decision.action === 'wait') return;
    // Blocked: only a read-only status check may decide what happens next.
    return;
  }

  try {
    let currentPreview = taskPreview.value;
    if (!explicitStart || !currentPreview || currentPreview.taskId !== target.taskId || currentPreview.roundId !== target.roundId) {
      const status = await api<TaskPreview>('previews/status', target);
      if (!stillCurrent(status)) {
        discardBlank();
        return;
      }
      currentPreview = status;
      taskPreview.value = status;
    }

    // Explicit file-only results have no webpage. Open only mints download
    // capabilities; close our un-navigated blank and show the real file list.
    if (currentPreview.kind === 'files') {
      const opened = await api<TaskPreview>('previews/open', target);
      if (!stillCurrent(opened)) { discardBlank(); return; }
      discardBlank();
      closeResult();
      resultTarget.value = target;
      chatVisible.value = false;
      taskPreview.value = opened;
      return;
    }

    // 1. Static preview: directly Open capability URL
    if (currentPreview.kind === 'static') {
      const opened = await api<TaskPreview>('previews/open', target);
      if (!stillCurrent(opened)) {
        discardBlank();
        return;
      }
      taskPreview.value = opened;
      if (navigateOrBlock(opened.url)) { taskPreview.value = opened; return; }
      discardBlank();
      throw new Error(opened.message || '未获取到有效的静态预览入口');
    }

    // 2. Running preview: viewing is not executing. `previews/open` is read-only
    // and mints a fresh login-bound capability URL, so an already running preview
    // is opened even when this page has no URL yet, and without re-approving the
    // execution command. It must never fall back to Start just to obtain a URL.
    const manifestId = currentPreview.manifestId;
    if (currentPreview.state === 'running') {
      const opened = await api<TaskPreview>('previews/open', target);
      if (!stillCurrent(opened)) {
        discardBlank();
        return;
      }
      taskPreview.value = opened;
      if (navigateOrBlock(opened.url)) return;
      discardBlank();
      throw new Error(opened.message || '未获取到有效的服务预览入口');
    }

    // 3. Runnable command: it runs only from a user gesture — the explicit Start
    // button, or the chat result action while this exact login/task/round/manifest
    // is already approved. Background work (Status, restore, poll) never starts a
    // command; with no approval it only shows the panel and waits.
    const sameScopedApproval = !!manifestId && isManifestApproved(target.taskId, target.roundId, manifestId);
    if (currentPreview.command?.length && manifestId && (explicitStart || sameScopedApproval)) {
      const startKey = previewIntentKey(scope) + ':' + manifestId;
      // An earlier start whose receipt never arrived must be reconciled read-only
      // first; never re-send that write blindly.
      if (peekPreviewStart(previewIntent, startKey) === 'unknown') {
        const status = await api<TaskPreview>('previews/status', target);
        if (!stillCurrent(status)) {
          discardBlank();
          return;
        }
        const verdict = reconcilePreviewStart(previewIntent, startKey, status.state === 'running');
        taskPreview.value = status;
        if (verdict === 'blocked') {
          discardBlank();
          previewError.value = '上次启动结果未知，请刷新状态确认后再试；系统不会自动重发启动命令。';
          return;
        }
        // Read-only status showed the preview running: only open it. This branch
        // must never fall through into another Start write.
        const opened = await api<TaskPreview>('previews/open', target);
        if (!stillCurrent(opened)) {
          discardBlank();
          return;
        }
        if (!navigateOrBlock(opened.url)) {
          discardBlank();
          previewError.value = opened.message || '上次启动结果未知，但服务器未提供有效的预览入口，请刷新状态确认。';
          return;
        }
        taskPreview.value = { ...status, ...opened };
        return;
      }
      const operationId = startOperationId(manifestId);
      if (!operationId) {
        discardBlank();
        previewError.value = '预览启动正在进行中，请稍候刷新状态。';
        return;
      }
      let started: TaskPreview;
      try {
        started = await api<TaskPreview>('previews/start', { ...target, manifestId, operationId });
        settlePreviewStart(previewIntent, startKey, operationId, started?.state === 'failed' ? 'failed' : 'succeeded');
      } catch (e) {
        // Any error after the request left the page may have happened after the
        // side effect (500/502/504, lost reply, offline). Keep the id and require a
        // read-only status check instead of silently writing again. Only a
        // provable no-work rejection could be retried, and the service exposes no
        // such evidence, so every failure here stays unknown.
        settlePreviewStart(previewIntent, startKey, operationId, 'unknown');
        throw e;
      }
      if (!stillCurrent(started)) {
        discardBlank();
        return;
      }
      taskPreview.value = started;
      if (started.state === 'running') {
        recordManifestApproval(target.taskId, target.roundId, manifestId);
        const opened = await api<TaskPreview>('previews/open', target);
        if (!stillCurrent(opened)) {
          discardBlank();
          return;
        }
        if (navigateOrBlock(opened.url)) {
          taskPreview.value = { ...started, ...opened };
          return;
        }
      }
    }

    // 4. Cannot directly open in a new tab (needs authorization or is not a
    // runnable webpage): close only our own blank and show the ResultPreview pane.
    discardBlank();
    if (!navigated && !explicitStart) {
      closeResult();
      resultTarget.value = target;
      chatVisible.value = false;
      taskPreview.value = currentPreview;
    }
  } catch (e) {
    discardBlank();
    if (!stillCurrent()) return;
    if (!explicitStart) {
      closeResult();
      resultTarget.value = target;
      chatVisible.value = false;
    }
    previewError.value = e instanceof Error ? e.message : '打开预览失败';
  } finally {
    releasePreviewIntent(previewIntent, intent);
  }
}
async function openResult(roundID: string) {
  const selected = task.value;
  if (!selected) return;
  await runPreviewFlow({ projectId: selected.projectId, taskId: selected.id, roundId: roundID }, false);
}
async function openArtifacts(roundID: string) {
  const selected = task.value;
  if (!selected) return;
  if (roundID !== conversation.value?.rounds?.at(-1)?.id) { await openResult(roundID); return; }
  closeResult();
  resultTarget.value = { projectId: selected.projectId, taskId: selected.id, roundId: roundID };
  chatVisible.value = false;
  await loadTaskPreview('status', true); // an explicit read-only gesture, never Start
}
function isCurrentPreviewTarget(target:{projectId:string;taskId:string;roundId:string}|undefined){
 return !!target&&!!task.value&&!!conversation.value&&target.projectId===task.value.projectId&&target.taskId===task.value.id&&target.roundId===conversation.value.rounds?.at(-1)?.id;
}
async function loadTaskPreview(action:'status'|'start'|'stop'='status',forceOpen=false){
 const target=resultTarget.value;
 // Only the live latest round of the selected task may talk to the preview API;
 // a historical target is a stored record and never issues Status/Open/Start.
 if(!target||!isCurrentPreviewTarget(target)||previewBusy.value||!online.value)return;
 clearTimeout(previewTimer);const epoch=previewEpoch;previewBusy.value=true;previewError.value='';
 try{
  const previous = taskPreview.value;
  const previousURL=previous?.state==='running'?previous.url:undefined;
  const manifestId=action==='start'?taskPreview.value?.manifestId:undefined;
  if(action==='start'&&!manifestId)throw new Error('预览配置缺少确认指纹，请先刷新状态并核对命令。');
  const result=await api<TaskPreview>('previews/'+action,{...target,...(action==='status'?{}:{operationId:createOperationID()}),...(action==='start'?{manifestId}:{})});
  if(epoch!==previewEpoch)return;
  if(result.taskId!==target.taskId||result.roundId!==target.roundId)throw new Error('成果目标已变化，请重新打开任务结果。');
  if(action==='start'&&result.state==='running'&&manifestId){
   recordManifestApproval(target.taskId,target.roundId,manifestId);
  }
  // A confirmed Stop ends that gesture: an explicit reopen is allowed again.
  if(action==='stop'&&result.state==='stopped')clearPreviewCompletion(previewIntent);
  result.artifacts = preserveArtifactLinks(result, previous);
  if(forceOpen && result.artifacts?.length && (result.kind==='files'||result.kind==='static')) {
   const opened = await api<TaskPreview>('previews/open', target);
   if(epoch!==previewEpoch)return;
   if(opened.taskId!==target.taskId||opened.roundId!==target.roundId)throw new Error('文件入口与当前成果不匹配。');
   Object.assign(result,opened);
  } else if(result.state==='running' && result.kind!=='files'){
   if(!forceOpen&&safePreviewURL(previousURL))result.url=previousURL;
   else{
    const opened=await api<TaskPreview>('previews/open',target);
    if(epoch!==previewEpoch)return;
    if(opened.taskId!==target.taskId||opened.roundId!==target.roundId)throw new Error('预览入口与当前成果不匹配。');
    if(opened.state==='running'&&!safePreviewURL(opened.url))throw new Error('服务器未提供有效的受限预览入口。');
    Object.assign(result,opened);
   }
  }
  // Passive replies must not publish a frame ahead of a foreground Open or
  // overwrite its newer capability: changing the keyed URL destroys the live document.
  if(action==='status'&&!forceOpen&&(previewIntent.current||taskPreview.value!==previous))return;
  taskPreview.value=result;
 }catch(e){if(epoch===previewEpoch)previewError.value=e instanceof Error?e.message:'未能读取预览状态'}
 finally{
  if(epoch===previewEpoch){previewBusy.value=false;if(resultTarget.value&&online.value&&document.visibilityState==='visible')previewTimer=setTimeout(()=>void loadTaskPreview(),3000)}
 }
}
watch(online,value=>{if(value&&resultTarget.value)void loadTaskPreview()});
function previewVisibility(){if(document.visibilityState==='visible'&&resultTarget.value)void loadTaskPreview();else clearTimeout(previewTimer)}
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
  chatVisible.value = !!taskID.value;
  void refresh();
}
onMounted(async () => {
  readLocation();
  chatVisible.value = !!taskID.value;
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
  invalidatePreview('unmount');
  clearManifestApprovals();
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
    <ThemePicker class="login-theme-picker" />
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
      <nav v-if="project" class="project-tools-nav" aria-label="项目视图">
        <button v-for="v in (['board','files','terminal'] as const)" :key="v" :aria-pressed="mainView===v" :aria-label="({board:'看板',files:'文件',terminal:'终端'})[v]" :title="({board:'看板',files:'文件',terminal:'终端'})[v]" @click="mainView=v;closeResult()"><span aria-hidden="true">{{({board:'▦',files:'▱',terminal:'>_'})[v]}}</span><span class="tool-nav-label">{{({board:'看板',files:'文件',terminal:'终端'})[v]}}</span></button>
      </nav>
      <button v-if="project" class="project-chat-top" aria-label="项目对话" title="项目对话" @click="navigate()">◌<span> 项目对话</span></button>
      <div class="compact-global-actions"><ThemePicker /><button class="settings-toggle" aria-label="全局设置" :aria-expanded="settingsOpen" @click="settingsOpen=!settingsOpen">⋯</button>
      <div class="global-actions" :class="{'settings-open':settingsOpen}" role="group" aria-label="全局操作">
        <VersionBadge />
        <button aria-label="默认模型" title="为新项目和任务选择默认模型" @click="openModels()">默认模型</button>
        <span class="connection" :class="{ off: !online }" role="status">{{
          online ? "● 已连接" : "○ 正在重连"
        }}</span>
        <button class="logout" @click="logout">退出</button>
      </div>
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
        <ResultPreview v-if="resultTarget&&task&&conversation" :task="task" :conversation="conversation" :execution="task.executionId?state?.executions?.[task.executionId]:undefined" :round-id="resultTarget.roundId" :preview="taskPreview" :preview-blocked-url="previewBlockedURL" :busy="previewBusy" :error="previewError" :online="online" @back="closeResult();chatVisible=false" @chat="resultChat" @refresh="loadTaskPreview('status',true)" @start="resultTarget && runPreviewFlow(resultTarget, true)" @stop="loadTaskPreview('stop')" @revise="reviseResult" @instructions="previewInstructions"/>
        <ProjectTools v-else-if="mainView!=='board'" :key="projectID" :view="mainView" :project="project" :tasks="tasks" :executions="state?.executions??{}" :conversation-id="local.current" :online="online" @attach="addDraftAttachments(local,$event.conversationId,[$event.attachment]);chatVisible=true" />
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
          @process="processInteraction"
          @project="navigate()"
          @start="start($event)"
          @merge="openMerge($event)"
          @complete="requestCompletion"
          @topic="mobileTopic = $event"
          @add-topic="open('topic')"
          @add-task="open('task', $event)"
          @delete-topic="open('delete', $event.id)"
        /><ChatDrawer :open="chatVisible" @close="chatVisible = false"><ChatPanel
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
          :attachments="local.drafts[local.current]?.attachments??[]"
          :uploading="uploading[local.current]"
          @attach="attachFiles"
          @remove-attachment="removeDraftAttachment(local,local.current,$event)"
          @screenshot="screenshot"
          :pending="!!local.pending[local.current] || (task ? !!local.pending[state?.deliveries?.[task.id]?.id ?? ''] : false)"
          :online="online"
          :agent-available="
            agentAvailable &&
            !!conversation.profile.provider &&
            !!conversation.profile.model
          "
          :visible="chatVisible"
          @close="chatVisible = false"
          :preview="taskPreview"
          @draft="setDraft(local, $event)"
          @send="send"
          @retry-message="retryMessage"
          @project="navigate()"
          @create="open('task')"
          @resume="resume"
          @start="task && start(task)"
          @merge="task && openMerge(task)"
          :tasks="tasks"
          @files="openArtifacts"
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
          <button @click="chatVisible = false">收起聊天</button>
        </aside></ChatDrawer>
      </div></template
    >
    <MergeDialog
      v-if="mergePreview && mergeTarget"
      :key="mergePreview.id"
      :preview="mergePreview"
      :review="mergeReview"
      :mode="isPushOnlyReview(mergeReview) ? 'push_only' : 'local'"
      :stale="mergeStale"
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
    <CompleteTaskDialog v-if="completionTarget" :intent="completionTarget" :busy="completionBusy" :eligible="completionEligible" :error="completionError" @close="completionTarget=undefined" @confirm="finishCompletion" />
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
