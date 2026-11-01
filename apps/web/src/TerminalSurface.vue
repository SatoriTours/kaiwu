<script setup lang="ts">
import {onMounted,onBeforeUnmount,ref,watch} from 'vue';
import {Terminal} from '@xterm/xterm';
import {FitAddon} from '@xterm/addon-fit';
import {WebglAddon} from '@xterm/addon-webgl';
import '@xterm/xterm/css/xterm.css';

const props=defineProps<{enabled:boolean;screenReader:boolean}>();
const emit=defineEmits<{input:[string];resize:[{cols:number;rows:number}]}>();
const host=ref<HTMLElement>();
let terminal:Terminal|undefined,fit:FitAddon|undefined,observer:ResizeObserver|undefined,themeObserver:MutationObserver|undefined;
const darkMedia=matchMedia('(prefers-color-scheme: dark)');
function applyTheme(){
 if(!terminal)return;
 const dark=document.documentElement.dataset.theme==='dark'||document.documentElement.dataset.theme!=='light'&&darkMedia.matches;
 terminal.options.theme=dark?{background:'#292e35',foreground:'#e7ebf1',cursor:'#e7ebf1',selectionBackground:'#62748c80',black:'#343c46',red:'#efa49a',green:'#a9c5ac',yellow:'#dfca94',blue:'#a6c9ed',magenta:'#d3b6e5',cyan:'#a2d3cf',white:'#e7ebf1',brightBlack:'#a0abb9',brightRed:'#ffb4aa',brightGreen:'#c1dfb8',brightYellow:'#f0dda5',brightBlue:'#c0daf6',brightMagenta:'#e3c9f2',brightCyan:'#c0e9e4',brightWhite:'#fff'}:{background:'#fafbfa',foreground:'#303635',cursor:'#303635',selectionBackground:'#a9c2b280',black:'#303635',red:'#a33f35',green:'#3e6c49',yellow:'#806020',blue:'#356394',magenta:'#805297',cyan:'#2f706a',white:'#626d66',brightBlack:'#6b776f',brightRed:'#b74738',brightGreen:'#497e55',brightYellow:'#8f6c28',brightBlue:'#4478ac',brightMagenta:'#9565ac',brightCyan:'#3e857b',brightWhite:'#303635'};
}
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
  fontSize:14,lineHeight:1.08,cursorBlink:true,scrollback:5000,
  screenReaderMode:props.screenReader,disableStdin:!props.enabled,
 });
 terminal=term;applyTheme();themeObserver=new MutationObserver(applyTheme);themeObserver.observe(document.documentElement,{attributes:true,attributeFilter:['data-theme']});darkMedia.addEventListener('change',applyTheme);fit=new FitAddon();term.loadAddon(fit);term.open(host.value!);
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
 fitViewport();if(props.enabled)focusForInput();
});
function focusForInput(){
 // Roving tab focus is a user's navigation intent, not a request to type into
 // bash. A delayed restored first frame must not steal it back.
 if(!document.activeElement?.closest('[role="tab"]'))terminal?.focus();
}
watch(()=>props.enabled,value=>{if(terminal){terminal.options.disableStdin=!value||replaying;if(value)focusForInput()}});
watch(()=>props.screenReader,value=>{if(terminal)terminal.options.screenReaderMode=value});
onBeforeUnmount(()=>{++generation;cancelAnimationFrame(frame);observer?.disconnect();themeObserver?.disconnect();darkMedia.removeEventListener('change',applyTheme);terminal?.dispose();terminal=undefined});
</script>

<template><div class="terminal-surface" ref="host" role="group" aria-label="交互终端" :data-renderer="renderer"></div></template>

<style scoped>
.terminal-surface{position:absolute;inset:0;overflow:hidden;min-width:0;min-height:0;isolation:isolate}
.terminal-surface :deep(.xterm){height:100%;padding:0}
.terminal-surface :deep(.xterm-viewport){overscroll-behavior:contain;background-color:var(--terminal-bg,#fafbfa)}

</style>
