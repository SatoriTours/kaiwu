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
export interface Message {
 kind?:string;
 displayText?:string;
  id: string;
  role: string;
  text: string;
  delivery: string;
}
export interface Conversation {
 rounds?:Round[];
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
}
export interface MergeOperation {
  id: string;
  projectId: string;
  taskId: string;
  phase: string;
  error?: string;
  startedAt: string;
  taskAfterSHA: string;
  mainAfterSHA: string;
  preview: MergePreview;
}

export interface ToolRecord {id:string;name:string;input?:string;result?:string;failed:boolean}
export interface ReportCheck {title:string;claim:string;toolId?:string}
export interface Report {summary:string;stage:string;plan:string[];results:string[];checks:ReportCheck[];blockers:string[];next:string[];question:string}
export interface Round {id:string;phase:string;startedAt:string;finishedAt?:string;reportError?:string;report?:Report;tools:ToolRecord[]}

export interface TaskPreview {
 taskId: string;
 roundId: string;
 state: 'unavailable' | 'stopped' | 'starting' | 'running' | 'failed';
 manifestId?: string;
 message: string;
 command?: string[];
 path?: string;
 url?: string;
 logs?: string;
}

export interface DeliveryCheck {
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
