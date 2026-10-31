<script setup lang="ts">
import {onMounted,onBeforeUnmount,ref,watch} from 'vue';
import {Terminal} from '@xterm/xterm';
import {FitAddon} from '@xterm/addon-fit';
import {WebglAddon} from '@xterm/addon-webgl';
import '@xterm/xterm/css/xterm.css';

const props=defineProps<{enabled:boolean;screenReader:boolean}>();
const emit=defineEmits<{input:[string];resize:[{cols:number;rows:number}]}>();
const host=ref<HTMLElement>();
let terminal:Terminal|undefined,fit:FitAddon|undefined,observer:ResizeObserver|undefined;
let frame=0,generation=0,replaying=false;
const renderer=ref('默认渲染');

function fitViewport(){
 if(!terminal||!fit||!host.value?.clientWidth||!host.value.clientHeight||replaying)return;
 const size=fit.proposeDimensions();
 if(size)terminal.resize(Math.max(2,Math.min(1000,size.cols)),Math.max(1,Math.min(500,size.rows)));
}
function scheduleFit(){cancelAnimationFrame(frame);frame=requestAnimationFrame(fitViewport)}
function resize(){if(terminal)emit('resize',{cols:terminal.cols,rows:terminal.rows})}
function reset(){++generation;replaying=false;terminal?.reset()}
function write(data:Uint8Array,initial:boolean,cols:number,rows:number,done:()=>void){
 const term=terminal;if(!term)return;
 const token=generation;
 if(initial){replaying=true;term.options.disableStdin=true;term.reset();term.resize(Math.max(2,Math.min(1000,cols)),Math.max(1,Math.min(500,rows)))}
 term.write(data,()=>{
  if(token!==generation||terminal!==term)return;
  if(initial){replaying=false;term.options.disableStdin=!props.enabled;fitViewport();resize()}
  done();
 });
}
defineExpose({write,reset,resize});
onMounted(()=>{
 const term=new Terminal({
  fontFamily:'ui-monospace, SFMono-Regular, Menlo, Consolas, "Liberation Mono", monospace',
  fontSize:14,lineHeight:1.2,cursorBlink:true,scrollback:5000,
  screenReaderMode:props.screenReader,disableStdin:!props.enabled,
  theme:{background:'#1c2922',foreground:'#dce5d9',cursor:'#e6efdf',selectionBackground:'#526b5980',
   black:'#26352d',red:'#e68b80',green:'#a4c58c',yellow:'#e3ce92',blue:'#94b9d9',magenta:'#c8a4d4',cyan:'#90c9c2',white:'#dce5d9',
   brightBlack:'#83958a',brightRed:'#f5a498',brightGreen:'#badd9f',brightYellow:'#f0dda5',brightBlue:'#b1cfee',brightMagenta:'#dcbce8',brightCyan:'#ade0d8',brightWhite:'#f4f7f0'},
 });
 terminal=term;fit=new FitAddon();term.loadAddon(fit);term.open(host.value!);
 term.textarea?.setAttribute('aria-label','终端输入');
 term.onData(value=>{if(props.enabled&&!replaying)emit('input',value)});
 term.onResize(()=>{if(!replaying)resize()});
 // Preserve normal selection copying; Ctrl+C without selection reaches bash.
 term.attachCustomKeyEventHandler(event=>{
  if(event.type==='keydown'&&(event.ctrlKey||event.metaKey)&&event.key.toLowerCase()==='c'&&term.hasSelection())return false;
  return true;
 });
 try{
  const webgl=new WebglAddon();
  webgl.onContextLoss(()=>{webgl.dispose();renderer.value='默认渲染'});
  term.loadAddon(webgl);renderer.value='WebGL';
 }catch{renderer.value='默认渲染'}
 observer=new ResizeObserver(scheduleFit);observer.observe(host.value!);
 document.fonts.ready.then(()=>{if(terminal===term)scheduleFit()});
 fitViewport();if(props.enabled)term.focus();
});
watch(()=>props.enabled,value=>{if(terminal){terminal.options.disableStdin=!value||replaying;if(value)terminal.focus()}});
watch(()=>props.screenReader,value=>{if(terminal)terminal.options.screenReaderMode=value});
onBeforeUnmount(()=>{++generation;cancelAnimationFrame(frame);observer?.disconnect();terminal?.dispose();terminal=undefined});
</script>

<template><div class="terminal-surface" ref="host" role="group" aria-label="交互终端" :data-renderer="renderer"></div></template>

<style scoped>
.terminal-surface{position:absolute;inset:12px 14px;overflow:hidden;min-width:0;min-height:0}
.terminal-surface :deep(.xterm){height:100%;padding:0}
.terminal-surface :deep(.xterm-viewport){overscroll-behavior:contain;background-color:#1c2922}
@media(max-width:650px){.terminal-surface{inset:10px 8px}}
</style>
