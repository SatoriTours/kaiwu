<script setup lang="ts">
import {computed} from 'vue';
import type {Message} from './types.ts';
import MarkdownContent from './MarkdownContent.vue';
import {messageText,replySummary,replyLinks} from './chat-messages.ts';
const props=defineProps<{message:Message;updates?:Message[]}>();
const text=computed(()=>messageText(props.message));
const folded=computed(()=>!!props.updates||props.message.kind==='progress'||text.value.length>800||!text.value.trim());
const summaryMessage=computed(()=>[...(props.updates??[])].reverse().find(m=>messageText(m).trim()||(m.displayText!==undefined&&m.text.includes('<kaiwu-report>')))??props.message);
const summary=computed(()=>replySummary(summaryMessage.value));
const links=computed(()=>replyLinks(summaryMessage.value));
</script>
<template>
 <template v-if="folded">
  <MarkdownContent class="assistant-summary" :text="summary"/>
  <details class="assistant-detail"><summary>{{updates?'查看执行详情':message.kind==='progress'?'查看过程详情':'查看完整回复'}}</summary>
   <template v-if="updates"><div v-for="(update,i) in updates" :key="update.id" class="progress-update" :data-message-id="update.id"><small>第 {{i+1}} 条更新</small><MarkdownContent class="text" :text="messageText(update)"/><details v-if="update.displayText!==undefined" class="raw-report"><summary>原始回复与报告</summary><MarkdownContent :text="update.text"/></details></div></template>
   <template v-else><MarkdownContent class="text" :text="text.trim()?text:message.text"/><details v-if="text.trim()&&message.displayText!==undefined" class="raw-report"><summary>原始回复与报告</summary><MarkdownContent :text="message.text"/></details></template>
  </details>
 </template>
 <MarkdownContent v-else class="text" :text="text"/>
 <div v-if="links.length" class="reply-links"><small>相关链接</small><a v-for="url in links" :key="url" :href="url" target="_blank" rel="noopener noreferrer">{{url}}</a></div>
 <details v-if="!folded&&message.displayText!==undefined" class="assistant-detail raw-report"><summary>原始回复与报告</summary><MarkdownContent :text="message.text"/></details>
</template>
