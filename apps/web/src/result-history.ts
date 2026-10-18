import type { Conversation, Round } from './types';

/**
 * 格式化轮次时间为 MM/DD HH:mm
 */
export function formatRoundTime(dateStr?: string): string {
  if (!dateStr) return '';
  const d = new Date(dateStr);
  if (isNaN(d.getTime())) return '';
  const month = (d.getMonth() + 1).toString().padStart(2, '0');
  const day = d.getDate().toString().padStart(2, '0');
  const hours = d.getHours().toString().padStart(2, '0');
  const minutes = d.getMinutes().toString().padStart(2, '0');
  return `${month}/${day} ${hours}:${minutes}`;
}

/**
 * 剥离换行、多余空格并截断
 */
export function cleanTextSummary(text: string, maxLength = 36): string {
  if (!text) return '';
  let cleaned = text.trim();
  // 若包含序列化的 JSON 尝试提取 summary 或 request 字段
  if (cleaned.startsWith('{') && cleaned.endsWith('}')) {
    try {
      const parsed = JSON.parse(cleaned);
      if (typeof parsed?.request === 'string') cleaned = parsed.request;
      else if (typeof parsed?.summary === 'string') cleaned = parsed.summary;
    } catch {
      // 保持原样
    }
  }
  // 将换行符与连续空白压缩为空格
  cleaned = cleaned.replace(/[\r\n\t]+/g, ' ').replace(/\s{2,}/g, ' ').trim();
  if (cleaned.length <= maxLength) return cleaned;
  return cleaned.slice(0, maxLength) + '…';
}

/**
 * 获取对应轮次的用户请求摘要，诚实回退
 */
export function getRoundRequestSummary(round: Round, conversation?: Conversation): string {
  if (conversation?.messages && conversation.messages.length > 0) {
    // 轮次匹配对应用户输入消息（通过 round.id 或 index 对应）
    const matched = conversation.messages.find(m => m.id === round.id || (m.role === 'user' && m.id === (round as any).userMessageId));
    if (matched) {
      if (matched.displayText && matched.displayText.trim()) {
        return cleanTextSummary(matched.displayText);
      }
      if (matched.text && matched.text.trim()) {
        return cleanTextSummary(matched.text);
      }
    }
  }
  if (round.report?.summary && round.report.summary.trim()) {
    return cleanTextSummary(round.report.summary);
  }
  return '请求记录不可用';
}

export interface RoundOptionItem {
  id: string;
  label: string;
  timeStr: string;
  requestSummary: string;
}

/**
 * 生成不包含“第N轮”的往期下拉列表项
 */
export function getRoundOptions(rounds: Round[], conversation?: Conversation): RoundOptionItem[] {
  if (!rounds || rounds.length === 0) return [];
  return rounds.map((round) => {
    const timeStr = formatRoundTime(round.startedAt || (round as any).createdAt);
    const requestSummary = getRoundRequestSummary(round, conversation);
    const label = timeStr ? `${timeStr} · ${requestSummary}` : requestSummary;
    return {
      id: round.id,
      label,
      timeStr,
      requestSummary,
    };
  });
}

/**
 * 格式化 ResultPreview 标题，避免暴露“第N轮”
 */
export function formatResultHeading(
  round: Round,
  isLatest: boolean,
  conversation?: Conversation
): string {
  const timeStr = formatRoundTime(round.startedAt || (round as any).createdAt);
  const requestSummary = getRoundRequestSummary(round, conversation);
  const prefix = isLatest ? '最新结果' : '历史结果';
  const parts = [prefix];
  if (timeStr) parts.push(timeStr);
  if (requestSummary && requestSummary !== '请求记录不可用') parts.push(requestSummary);
  return parts.join(' · ');
}
