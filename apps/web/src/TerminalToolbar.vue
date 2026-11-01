<script setup lang="ts">
import {ref,onBeforeUnmount} from 'vue';
import CustomSelect from './CustomSelect.vue';
import type {TerminalItem} from './terminal-state.ts';
const props=defineProps<{terminals:TerminalItem[];activeId:string;taskId:string;options:{value:string;label:string}[];busy:boolean;online:boolean;connected:boolean;running:boolean;screenReader:boolean;truncated:boolean}>();
const emit=defineEmits<{select:[string];scope:[string];start:[];reconnect:[];stop:[string];reader:[]}>();
const menu=ref<HTMLDetailsElement>(),picker=ref<HTMLDetailsElement>();
function close(){if(menu.value)menu.value.open=false;if(picker.value)picker.value.open=false}
function choose(id:string){emit('select',id);close()}
function stop(id:string){emit('stop',id);close()}
function outside(event:PointerEvent){for(const details of [menu.value,picker.value])if(details&&!details.contains(event.target as Node))details.open=false}
function escape(event:KeyboardEvent){if(event.key==='Escape'){close();(event.target as HTMLElement)?.blur()}}
document.addEventListener('pointerdown',outside);document.addEventListener('keydown',escape);
onBeforeUnmount(()=>{document.removeEventListener('pointerdown',outside);document.removeEventListener('keydown',escape)});
function move(event:KeyboardEvent,index:number){if(!['ArrowLeft','ArrowRight','Home','End'].includes(event.key))return;event.preventDefault();const next=event.key==='Home'?0:event.key==='End'?props.terminals.length-1:(index+(event.key==='ArrowRight'?1:-1)+props.terminals.length)%props.terminals.length;choose(props.terminals[next]!.id);(event.currentTarget as HTMLElement).parentElement?.parentElement?.querySelectorAll<HTMLElement>('[role=tab]')[next]?.focus()}
</script>
<template><div class="terminal-toolbar" aria-label="终端工具栏">
 <div class="terminal-workspace"><CustomSelect :model-value="taskId" :options="options" :disabled="busy||!online" aria-label="终端工作区" @update:model-value="emit('scope',$event)" /></div>
 <div class="terminal-tabs" role="tablist" aria-label="工作区终端">
  <div v-for="(item,index) in terminals" :key="item.id" class="terminal-tab-wrap" :class="{active:item.id===activeId}">
   <button class="terminal-tab" role="tab" :aria-selected="item.id===activeId" :data-terminal-id="item.id" :tabindex="item.id===activeId?0:-1" @click="choose(item.id)" @keydown="move($event,index)">bash {{item.number}}</button>
   <button class="terminal-tab-close" :aria-label="'结束 bash '+item.number" :disabled="busy||!online" title="结束并关闭终端" @click.stop="stop(item.id)"><svg viewBox="0 0 16 16"><path d="m4 4 8 8m0-8-8 8"/></svg></button>
  </div>
 </div>
 <details ref="picker" class="terminal-picker">
  <summary role="button" aria-label="切换终端">{{activeId?'bash '+terminals.find(t=>t.id===activeId)?.number:'终端'}}<svg viewBox="0 0 16 16"><path d="m5 6 3 3 3-3"/></svg></summary>
  <div class="terminal-dropdown"><CustomSelect :model-value="taskId" :options="options" :disabled="busy||!online" aria-label="终端工作区" @update:model-value="emit('scope',$event);close()" /><div class="terminal-menu-rule"></div><button v-for="item in terminals" :key="item.id" :aria-current="item.id===activeId?'true':undefined" @click="choose(item.id)">bash {{item.number}} <span v-if="item.id===activeId">✓</span></button><p v-if="!terminals.length">没有运行中的终端</p></div>
 </details>
 <button class="terminal-icon terminal-new" :disabled="busy||!online" aria-label="新建终端" title="新建终端" @click="emit('start')"><svg viewBox="0 0 16 16"><path d="M8 3v10M3 8h10"/></svg></button>
 <details ref="menu" class="terminal-more">
  <summary role="button" aria-label="终端更多操作" title="终端更多操作"><svg viewBox="0 0 16 16"><circle cx="3" cy="8" r=".7"/><circle cx="8" cy="8" r=".7"/><circle cx="13" cy="8" r=".7"/></svg></summary>
  <div class="terminal-dropdown">
   <p class="terminal-status" role="status" aria-label="终端连接状态">{{running?(connected?'已连接':'连接已断开，可接回原终端'):'未运行'}}{{truncated?' · 较早输出已截断':''}}</p>
   <button :disabled="busy||!online" @click="emit('reconnect');close()">重新连接</button>
   <button :aria-pressed="screenReader" aria-label="终端读屏模式" title="供屏幕阅读器朗读，部分软键盘输入需关闭" @click="emit('reader');close()">读屏模式<span>{{screenReader?'开启':'关闭'}}</span></button>
   <div class="terminal-menu-rule"></div>
   <p>Tab 补全 · ↑↓ 历史 · Ctrl+C 中断</p><p>关闭网页不结束 Shell。服务器账号权限；同工作区与 AI／合并互斥。</p><p>保留最近 256 KiB 输出；服务重启后不能接回原 Shell。</p>
   <div class="terminal-menu-rule"></div>
   <button class="terminal-danger" :disabled="busy||!online||!activeId" @click="stop(activeId)">结束终端</button>
  </div>
 </details>
</div></template>
<style scoped>
.terminal-toolbar{display:flex;align-items:center;gap:0;width:100%;height:48px;min-width:0;color:var(--ink)}
.terminal-workspace{flex-shrink:0;max-width:150px;margin-right:9px}.terminal-workspace :deep(.custom-select-trigger){min-height:28px;border:0;background:transparent;padding:0 4px;font-size:11px;color:var(--muted);max-width:150px}
.terminal-tabs{display:flex;align-items:center;gap:3px;min-width:0;overflow-x:auto;scrollbar-width:none}.terminal-tabs::-webkit-scrollbar{display:none}
.terminal-tab-wrap{display:flex;align-items:center;gap:6px;padding:0 10px;height:28px;flex-shrink:0;border:1px solid transparent;border-radius:6px;color:var(--muted)}
.terminal-tab-wrap.active{background:var(--terminal-tab-active,#edf2ee);border-color:var(--terminal-tab-border,#dbe3dc);color:var(--ink)}
.terminal-tab{border:0;border-radius:0;background:transparent!important;min-height:26px;padding:0;font:12px/1.2 system-ui,sans-serif;color:inherit;white-space:nowrap}
.terminal-tab-close{display:grid;place-items:center;width:12px;min-height:18px;padding:0;visibility:hidden;border:0;background:transparent;color:var(--muted)}
.terminal-tab-wrap.active .terminal-tab-close,.terminal-tab-wrap:hover .terminal-tab-close,.terminal-tab-wrap:focus-within .terminal-tab-close{visibility:visible}
.terminal-tab-close:hover{color:var(--ink)}svg{width:15px;height:15px;fill:none;stroke:currentColor;stroke-width:1.5;stroke-linecap:round;stroke-linejoin:round;pointer-events:none}.terminal-tab-close svg{width:10px;height:10px}
.terminal-icon,.terminal-more>summary{display:grid;place-items:center;width:28px;min-height:28px;border:0;border-radius:5px;background:transparent;color:var(--muted);padding:0;flex-shrink:0;cursor:pointer}
.terminal-new{margin-left:5px}.terminal-icon:hover,.terminal-more>summary:hover{background:var(--theme-hover,#f0f3f0)}
.terminal-more{margin-left:auto;flex-shrink:0}.terminal-more>summary,.terminal-picker>summary{list-style:none}.terminal-more>summary::-webkit-details-marker,.terminal-picker>summary::-webkit-details-marker{display:none}
.terminal-picker{display:none}.terminal-dropdown{position:absolute;right:0;top:44px;z-index:60;width:250px;max-width:calc(100vw - 16px);padding:6px;background:var(--theme-surface,#fff);border:1px solid var(--border);border-radius:8px;box-shadow:0 8px 24px var(--theme-shadow,#1026171a);color:var(--ink)}
.terminal-dropdown button{width:100%;display:flex;justify-content:space-between;border:0;background:transparent;text-align:left;padding:8px 10px;font-size:12px;min-height:32px}
.terminal-dropdown button:hover,.terminal-dropdown button[aria-current=true]{background:var(--theme-hover,#f0f3f0)}
.terminal-dropdown p{margin:0;padding:7px 10px;font-size:10px;line-height:1.7;color:var(--muted);white-space:normal}
.terminal-dropdown .terminal-danger{color:var(--theme-danger,#a4544d)}.terminal-menu-rule{height:1px;margin:4px 6px;background:var(--border)}
@media(max-width:650px){.terminal-workspace,.terminal-tabs{display:none}.terminal-picker{display:block;min-width:0;flex-shrink:1}.terminal-picker>summary{display:flex;align-items:center;gap:5px;min-height:28px;padding:2px 3px;white-space:nowrap;font-size:11px;cursor:pointer}.terminal-picker>summary svg{width:11px;height:11px}.terminal-icon,.terminal-more>summary{width:24px}.terminal-new{margin-left:2px}.terminal-dropdown{right:0}.terminal-toolbar{gap:0}.terminal-picker .terminal-dropdown :deep(.custom-select){width:100%}.terminal-picker .terminal-dropdown :deep(.custom-select-trigger){width:100%;font-size:11px}}
</style>
