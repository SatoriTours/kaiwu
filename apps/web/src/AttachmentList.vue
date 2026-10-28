<script setup lang="ts">
import {ref,watch,nextTick} from 'vue';
import type {Attachment} from './types.ts';
import {attachmentURL} from './attachments.ts';
const props=defineProps<{items:Attachment[];conversationId:string;editable?:boolean}>();
const viewer=ref<HTMLDialogElement>(),selected=ref<Attachment>();
async function open(item:Attachment){selected.value=item;await nextTick();viewer.value?.showModal()}
watch(()=>props.conversationId,()=>{viewer.value?.close();selected.value=undefined});
const emit=defineEmits<{remove:[string]}>();
</script>
<template><div v-if="items.length" class="attachment-list">
 <div v-for="item in items" :key="item.id" class="attachment-chip">
  <a :href="attachmentURL(conversationId,item.id)" :title="'下载 '+item.name">
   <img v-if="['image/png','image/jpeg','image/webp','image/gif'].includes(item.mime)" :src="attachmentURL(conversationId,item.id,true)" :alt="item.name" loading="lazy" role="button" tabindex="0" :aria-label="'预览 '+item.name" @click.prevent="open(item)" @keydown.enter.prevent="open(item)" />
   <span v-else class="attachment-file-icon" aria-hidden="true">▤</span>
   <span><strong>{{item.name}}</strong><small>{{Math.max(1,Math.ceil(item.size/1024))}} KB · {{editable?'待发送':'附件'}}</small></span>
  </a>
  <button v-if="editable" :aria-label="'移除 '+item.name" @click="emit('remove',item.id)">×</button>
 </div>
</div><dialog ref="viewer" class="attachment-viewer" aria-label="图片预览" @click="($event.target===viewer)&&viewer?.close()"><template v-if="selected"><header><strong>{{selected.name}}</strong><a :href="attachmentURL(conversationId,selected.id)">下载</a><button aria-label="关闭图片预览" @click="viewer?.close()">×</button></header><img :src="attachmentURL(conversationId,selected.id,true)" :alt="selected.name" /></template></dialog></template>
<style scoped>
.attachment-list{display:flex;flex-wrap:wrap;gap:8px;margin:8px 0;min-width:0}
.attachment-chip{display:flex;align-items:center;gap:5px;border:1px solid var(--border);border-radius:8px;padding:7px;max-width:100%;min-width:0;background:var(--theme-inset,#f6f8f5)}
.attachment-chip a{display:flex;align-items:center;gap:8px;text-decoration:none;color:inherit;min-width:0}
.attachment-chip img{width:42px;height:42px;object-fit:cover;border-radius:4px}
.attachment-chip strong{display:block;font-size:11px;font-weight:500;max-width:170px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.attachment-chip small{display:block;color:var(--muted);font-size:10px}
.attachment-chip button{border:0;background:none;padding:1px 5px}
.attachment-file-icon{font-size:26px;color:var(--blue)}
.attachment-viewer{border:1px solid var(--border);border-radius:12px;padding:16px;max-width:95vw;max-height:95dvh;background:var(--theme-surface,#fff);color:var(--ink)}.attachment-viewer::backdrop{background:#101a1680}.attachment-viewer header{display:flex;align-items:center;gap:16px;margin-bottom:12px}.attachment-viewer header strong{flex:1;font-size:12px}.attachment-viewer>img{display:block;max-width:85vw;max-height:75dvh;object-fit:contain}
</style>
