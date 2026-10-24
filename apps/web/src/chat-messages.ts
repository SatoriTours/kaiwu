import type {Message, Round} from './types.ts';

export interface ChatEntry {
 id: string;
 role: 'user' | 'assistant' | 'system';
 message: Message;
 updates?: Message[];
 userMessage?: Message;
 isPlaceholder?: boolean;
 loading?: boolean;
 round?: Round;
 runState?: string;
}

export interface GroupChatOptions {
 runState?: string;
 taskStatus?: string;
 rounds?: Round[];
}

// Group chat messages by user turn:
// Each user message is followed by at most ONE assistant bubble that merges all
// subsequent progress updates and final replies until the next turn.
export function groupChatMessages(messages: Message[], options?: GroupChatOptions): ChatEntry[] {
 const entries: ChatEntry[] = [];
 const rounds = options?.rounds ?? [];
 const runState = options?.runState ?? 'idle';
 const taskStatus = options?.taskStatus;
 const isWaitingOrSettled = ['waiting_user', 'needs_confirmation', 'completed'].includes(taskStatus ?? '');
 const isBusy = ['starting', 'running'].includes(runState) && !isWaitingOrSettled;

 let currentAssistantEntry: ChatEntry | undefined;
 let currentUserMessage: Message | undefined;

 for (let i = 0; i < messages.length; i++) {
  const msg = messages[i]!;
  if (msg.role === 'user') {
   currentAssistantEntry = undefined;
   currentUserMessage = msg;
   entries.push({
    id: msg.id,
    role: 'user',
    message: msg,
   });
  } else if (msg.role === 'assistant') {
   if (currentAssistantEntry) {
    currentAssistantEntry.updates?.push(msg);
    currentAssistantEntry.message = msg;
   } else {
    const round = rounds.find(r => r.id === currentUserMessage?.id) ?? (currentUserMessage ? undefined : rounds.at(-1));
    currentAssistantEntry = {
     id: msg.id,
     role: 'assistant',
     message: msg,
     updates: [msg],
     userMessage: currentUserMessage,
     round,
    };
    entries.push(currentAssistantEntry);
   }
  } else {
   currentAssistantEntry = undefined;
   currentUserMessage = undefined;
   entries.push({
    id: msg.id,
    role: msg.role as any,
    message: msg,
   });
  }
 }

 const lastEntry = entries.at(-1);
 if (lastEntry && lastEntry.role === 'user') {
  if (options && (isBusy || isWaitingOrSettled || ['interrupted', 'failed'].includes(runState))) {
   const uMsg = lastEntry.message;
   const round = rounds.find(r => r.id === uMsg.id);
   entries.push({
    id: 'placeholder-' + uMsg.id,
    role: 'assistant',
    isPlaceholder: true,
    loading: isBusy,
    message: {
     id: 'placeholder-' + uMsg.id,
     role: 'assistant',
     text: '',
     delivery: isBusy ? 'pending' : runState === 'interrupted' ? 'uncertain' : 'failed',
    },
    userMessage: uMsg,
    round,
    runState,
   });
  }
 } else if (lastEntry && lastEntry.role === 'assistant') {
  if (isBusy) {
   lastEntry.loading = true;
  }
  lastEntry.runState = runState;
 }

 return entries;
}

export function filterChatEntries(entries: ChatEntry[], filter: 'all' | 'questions'): ChatEntry[] {
 if (filter === 'questions') {
  return entries.filter(e => e.role === 'user');
 }
 return entries;
}

export function messageText(message: Message): string {
 if (message.displayText !== undefined) return message.displayText;
 const marker = message.text.indexOf('<kaiwu-report>');
 return message.delivery === 'accepted' && marker >= 0 ? message.text.slice(0, marker) : message.text;
}

function summarySource(message: Message): string {
 let text = messageText(message).split('<kaiwu-report>')[0] ?? '';
 if (message.displayText !== undefined) {
  const report = message.text.match(/<kaiwu-report>([\s\S]*?)<\/kaiwu-report>/);
  if (report) try {
   const value = JSON.parse(report[1]!);
   if (typeof value.summary === 'string' && value.summary.trim()) text = value.summary;
  } catch {
   /* Keep visible text fallback */
  }
 }
 return text;
}

function cleanProseText(text: string): string {
 return text
  // Remove fenced code blocks
  .replace(/```[^\n]*\n[\s\S]*?(?:```|$)/g, '')
  // Remove inline code
  .replace(/`[^`\n]+`/g, '')
  // Remove shell commands lines starting with $ or > or #
  .replace(/^[ \t]*[$>#][ \t]+.*$/gm, '')
  // Remove list bullets / numbers at start of lines to avoid exposing implementation task lists
  .replace(/^[ \t]*[-*+][ \t]+/gm, '')
  .replace(/^[ \t]*\d+\.[ \t]+/gm, '')
  .trim();
}

function truncateToSentences(text: string, maxSentences = 2, maxChars = 200): string {
 const cleaned = cleanProseText(text);
 if (!cleaned) return '';
 // Split by empty lines first
 const firstBlock = cleaned.split(/\n\s*\n/)[0]?.trim() || cleaned;
 const sentences = firstBlock.split(/(?<=[。！？])|(?<=[.!?])\s+/).map(s => s.trim()).filter(Boolean);
 const brief = sentences.slice(0, maxSentences).join('');
 const chosen = brief || firstBlock;
 const chars = Array.from(chosen);
 return chars.length > maxChars ? chars.slice(0, maxChars).join('') + '…' : chosen;
}

export function turnSummary(entry: ChatEntry): string {
 if (entry.isPlaceholder) {
  if (entry.loading) {
   return entry.runState === 'starting' ? '正在准备启动…' : '正在执行…';
  }
  if (entry.runState === 'interrupted') return '执行已中断，尚未收到回复。';
  if (entry.runState === 'failed') return '执行失败，尚未收到回复。';
  return '等待回复…';
 }

 const reportSummary = entry.round?.report?.summary?.trim();
 if (reportSummary) {
  const summarized = truncateToSentences(reportSummary, 2, 200);
  if (summarized) return summarized;
 }

 const targetMsg = entry.message;
 const rawText = summarySource(targetMsg);
 const summarized = truncateToSentences(rawText, 2, 200);

 if (!summarized) {
  if (entry.loading) return '正在处理…';
  return rawText.trim() ? '已返回代码或执行输出，展开查看。' : '本条更新没有文字说明，展开查看记录。';
 }

 return summarized;
}

export function replySummary(message: Message): string {
 const text = summarySource(message);
 const prose = text.replace(/```[^\n]*\n[\s\S]*?(?:```|$)/g, '').trim();
 if (!prose) return text.trim() ? '已返回代码或执行输出，展开查看。' : message.delivery === 'accepted' ? '正在处理，等待进展说明…' : '本条更新没有文字说明，展开查看记录。';
 const formatted = prose.includes('\n') ? prose : prose.replace(/([。！？])\s*(?=\S)/g, '$1\n\n');
 const preview = formatted.split(/\n\s*\n/).slice(0, 3).join('\n\n');
 const chars = Array.from(preview);
 return chars.length > 600 ? chars.slice(0, 600).join('') + '…' : preview;
}

export function replyLinks(message: Message): string[] {
 const source = messageText(message) + '\n' + summarySource(message);
 const links = new Set<string>();
 for (const match of source.matchAll(/https?:\/\/[^\s<>"'`，。；！？、）】》]+/gi)) {
  const value = match[0].replace(/[)\]}>.,;，。；！？、）】》]+$/g, '');
  try {
   const url = new URL(value);
   if (['http:', 'https:'].includes(url.protocol) && !url.username && !url.password) links.add(value);
  } catch {
   /* Ignore incomplete streamed URLs. */
  }
  if (links.size === 6) break;
 }
 return [...links];
}
