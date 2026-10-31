export interface Profile {
  credentialSource?: string;
  provider: string;
  model: string;
  thinking: string;
}
export interface Project {
  defaultBranch?: string;
  mainBranchOverride?: string;
  commitName?: string;
  commitEmail?: string;
  id: string;
  name: string;
  path: string;
  conversationId: string;
}
export interface Topic {
  id: string;
  projectId: string;
  name: string;
  protected: boolean;
}
export interface Task {
  id: string;
  projectId: string;
  topicId: string;
  title: string;
  request: string;
  status: string;
  conversationId: string;
  profile: Profile;
  executionId?: string;
}
export interface Execution {
  startedAt?:string;
  finishedAt?:string;
  id: string;
  workspace?: string;
  phase: string;
  error?: string;
  summary?: string;
  tools: {
    id: string;
    name: string;
    input?: string;
    result?: string;
    failed: boolean;
  }[];
}
export interface Attachment { id:string; name:string; mime:string; size:number }
export interface Message {
 attachments?:Attachment[];
 kind?:string;
 displayText?:string;
  id: string;
  role: string;
  text: string;
  delivery: string;
}
export interface Conversation {
  rounds?: Round[];
  sessionFile?: string;
  id: string;
  projectId: string;
  taskId?: string;
  sessionId: string;
  runState: string;
  error?: string;
  profile: Profile;
  pendingProfile?: Profile;
  messages: Message[];
  interactions?: InteractionRequest[];
}
export interface Snapshot {
  structureRevision?: number;
  taskGit?: Record<string, GitStatus>;
  merges?: Record<string, MergeOperation>;
  deliveries?: Record<string, DeliverySummary>;
  deliveryPolicies?: Record<string, DeliveryPolicySummary>;
  revision: number;
  projects: Record<string, Project>;
  topics: Topic[];
  tasks: Record<string, Task>;
  conversations: Record<string, Conversation>;
  profile: Profile;
  executions?: Record<string, Execution>;
}
export interface Receipt {
  revision: number;
  createdId?: string;
  conversationId?: string;
  notice?: string;
  type: string;
  interactionId?: string;
  interactionVersion?: number;
  delivery?: string;
  interaction?: InteractionRequest;
}
export interface GitFile {
  path: string;
  status: string;
}
export interface GitStatus {
  taskId: string;
  state: string;
  mainBranch: string;
  mainSHA: string;
  taskSHA: string;
  reason?: string;
  checkedAt: string;
  dirtyFiles: GitFile[];
  canMerge: boolean;
}
export interface MergePreview {
  id: string;
  projectId: string;
  taskId: string;
  mainBranch: string;
  mainSHA: string;
  taskSHA: string;
  fingerprint: string;
  commitName: string;
  commitEmail: string;
  reason?: string;
  expiresAt: string;
  files: GitFile[];
  needsCommit: boolean;
  canMerge: boolean;
  mode?: string;
  roundId?: string;
}
/** One commit a push review would upload. */
export interface MergeUpload {
  sha: string;
  subject: string;
}
/** Read-only push review: the local proof plus the observed remote state. */
export interface MergeReview extends MergePreview {
  remoteName?: string;
  remoteUrl?: string;
  mainRef?: string;
  observedRemoteSHA?: string;
  localMainSHA?: string;
  uploads?: MergeUpload[];
  pending?: GitFile[];
  pendingNote?: string;
  safetyProof?: string;
  credentialRef?: string;
  alreadyMerged?: boolean;
  pushOnly?: boolean;
  canPush?: boolean;
  pushReason?: string;
  reviewFingerprint?: string;
}
/** Remote authorisation frozen into a push operation. */
export interface PushReviewFreeze {
  fingerprint: string;
  reviewFingerprint: string;
  remoteName: string;
  remoteUrl: string;
  mainRef: string;
  credentialRef?: string;
  observedSHA: string;
  localMainSHA: string;
  taskSHA: string;
  uploads?: MergeUpload[];
  pending?: GitFile[];
  pendingNote?: string;
  safetyProof?: string;
  roundId: string;
  pushOnly?: boolean;
}
export interface MergeOperation {
  id: string;
  projectId: string;
  taskId: string;
  phase: string;
  error?: string;
  startedAt: string;
  finishedAt?: string;
  taskBeforeSHA?: string;
  taskAfterSHA: string;
  mainBeforeSHA?: string;
  mainAfterSHA: string;
  mode?: string;
  roundId?: string;
  preview: MergePreview;
  push?: PushReviewFreeze;
  intendedSHA?: string;
  remoteAfterSHA?: string;
  pushAttemptedAt?: string;
  ciStatus?: string;
}

export interface ToolRecord {id:string;name:string;input?:string;result?:string;failed:boolean}
export interface ReportCheck {title:string;claim:string;toolId?:string}
export interface Report {summary:string;stage:string;plan:string[];results:string[];checks:ReportCheck[];blockers:string[];next:string[];question:string}
export interface Round {
  failure?: { kind: 'execution' | 'session' | 'send' | 'unknown'; detail?: string };
  id: string;
  phase: string;
  startedAt: string;
  finishedAt?: string;
  reportError?: string;
  interactionError?: string;
  interactionNotice?: string;
  report?: Report;
  tools: ToolRecord[];
}

export interface PreviewArtifact { id: string; name: string; size: number; type: string; url?: string }

export interface TaskPreview {
 taskId: string;
 roundId: string;
 state: 'unavailable' | 'stopped' | 'starting' | 'running' | 'failed';
 manifestId?: string;
 message: string;
 kind?: 'static' | 'files';
 artifacts?: PreviewArtifact[];
 root?: string;
 entry?: string;
 command?: string[];
 path?: string;
 url?: string;
 logs?: string;
}

export interface DeliveryCheck {
  definitionPaths?: string[];
  id: string;
  argv: string[];
  dir: string;
  timeoutSeconds: number;
}
export interface DeliveryWorkflow {
  id: number;
  jobs: string[];
}
export interface DeliveryRepository {
  id: number;
  owner: string;
  name: string;
  url: string;
  mainBranch: string;
}
export interface ProjectDeliveryPolicy {
  projectId: string;
  version: number;
  enabled: boolean;
  requireAcceptance: boolean;
  allowCreateMain: boolean;
  repository: DeliveryRepository;
  checks: DeliveryCheck[];
  taskWorkflows: DeliveryWorkflow[];
  mainWorkflows: DeliveryWorkflow[];
  maxRepairs: number;
  activeSeconds: number;
  triggerSeconds: number;
  ciSeconds: number;
  gitCredentialRef: string;
  apiCredentialRef: string;
  authorizationDigest: string;
  actor: string;
  authorizedAt: string;
  revokedAt?: string;
}
export interface DeliveryStep {
  id: string;
  deliveryId: string;
  kind: string;
  inputFingerprint: string;
  phase: string;
  beforeSHA?: string;
  afterSHA?: string;
  tree?: string;
  ref?: string;
  summary: string;
  bootId?: string;
  processGroup?: number;
  exitCode?: number;
  exitKnown?: boolean;
  truncated?: boolean;
  sequence: number;
  startedAt: string;
  finishedAt?: string;
}
export interface DeliveryCI {
  deliveryId: string;
  stage: string;
  branch: string;
  sha: string;
  event: string;
  status: string;
  conclusion?: string;
  url: string;
  repositoryId?: number;
  workflowId?: number;
  runId?: number;
  attempt?: number;
  jobs?: Record<string, string>;
  observedAt?: string;
}
export interface DeliverySummary {
  id: string;
  projectId: string;
  taskId: string;
  phase: string;
  version: number;
  cycle: number;
  candidateSHA?: string;
  localMainSHA?: string;
  remoteMainSHA?: string;
  requireAcceptance: boolean;
  acceptanceToken?: string;
  acceptedSHA?: string;
  repairsUsed: number;
  repairsLimit: number;
  blockCode?: string;
  blockReason?: string;
  waitingCode?: string;
  waitingReason?: string;
  holdsTurn: boolean;
  steps: DeliveryStep[];
  ci: DeliveryCI[];
}
export interface DeliveryPolicySummary {
  version: number;
  enabled: boolean;
  requireAcceptance: boolean;
  repository: DeliveryRepository;
}
export interface DeliveryStepPage {
  items: DeliveryStep[];
  next: number;
}
export interface PreflightReport {
  ready: boolean;
  fingerprint: string;
  blockers: string[];
}

export type InteractionFieldType =
  | "confirm"
  | "text"
  | "textarea"
  | "single_select"
  | "multi_select"
  | "number";

export interface InteractionOption {
  id: string;
  label: string;
  description?: string;
  recommended?: boolean;
}

export interface InteractionField {
  id: string;
  type: InteractionFieldType;
  label: string;
  description?: string;
  placeholder?: string;
  unit?: string;
  yesLabel?: string;
  noLabel?: string;
  prefill?: string;
  required?: boolean;
  allowEmpty?: boolean;
  allowOther?: boolean;
  integer?: boolean;
  minLength?: number;
  maxLength?: number;
  minSelections?: number;
  maxSelections?: number;
  min?: number;
  max?: number;
  options?: InteractionOption[];
}

export type InteractionSource = "assistant_question" | "agent_ui";

export type InteractionStatus =
  | "pending"
  | "answered"
  | "cancelled"
  | "expired"
  | "superseded";

export type InteractionDelivery =
  | "recorded"
  | "dispatching"
  | "accepted"
  | "not_sent"
  | "uncertain";

export interface InteractionResponse {
  operationId: string;
  mode: "form" | "text" | "cancel";
  answers?: Record<string, boolean | string | string[] | number>;
  others?: Record<string, string>;
  comment?: string;
  text?: string;
  delivery: InteractionDelivery;
  note?: string;
  messageId?: string;
  followUpRoundId?: string;
  answeredAt?: string;
  updatedAt?: string;
}

export interface InteractionNative {
  method: "confirm" | "select" | "input" | "editor";
  upstreamId: string;
  runId?: string;
  generation?: number;
  choices?: string[];
  hasTimeout?: boolean;
}

export interface InteractionRequest {
  schemaVersion: number;
  id: string;
  version: number;
  projectId: string;
  conversationId: string;
  taskId?: string;
  roundId?: string;
  sourceMessageId?: string;
  source: InteractionSource;
  title: string;
  description?: string;
  fields: InteractionField[];
  blocking: boolean;
  allowCancel: boolean;
  allowTextFallback?: boolean;
  status: InteractionStatus;
  statusReason?: string;
  supersededBy?: string;
  createdAt: string;
  expiresAt?: string;
  dedupeKey?: string;
  native?: InteractionNative;
  response?: InteractionResponse;
}

