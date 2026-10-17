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
  messages: Message[];
}
export interface Snapshot {
  structureRevision?: number;
  taskGit?: Record<string, GitStatus>;
  merges?: Record<string, MergeOperation>;
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
