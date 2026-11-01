export type TerminalView={taskId:string;terminalId:string;closed:boolean};
export type TerminalItem={id:string;running:boolean;number:number;operationId?:string};
type StorageLike=Pick<Storage,'getItem'|'setItem'>;
export const terminalStorage:StorageLike={getItem:key=>window.localStorage.getItem(key),setItem:(key,value)=>window.localStorage.setItem(key,value)};
const empty=():TerminalView=>({taskId:'',terminalId:'',closed:false});
function key(project:string){return 'kaiwu.terminal.view.'+project}
export function readTerminalView(storage:StorageLike,project:string):TerminalView{
 try{const value=JSON.parse(storage.getItem(key(project))??'null');if(!value||typeof value.taskId!=='string'||typeof value.terminalId!=='string'||value.taskId.length>200||value.terminalId.length>200)return empty();return {taskId:value.taskId,terminalId:value.terminalId,closed:value.closed===true}}catch{return empty()}
}
export function writeTerminalView(storage:StorageLike,project:string,value:TerminalView){
 try{storage.setItem(key(project),JSON.stringify({taskId:value.taskId,terminalId:value.terminalId,closed:value.closed}))}catch{/* Persistence is optional, never block an active terminal. */}
}
export function chooseTerminal<T extends TerminalItem>(items:T[],id:string):T|undefined{
 return items.find(t=>t.id===id)??items.find(t=>t.running)??items[0];
}
export function readMainView(storage:StorageLike,project:string):'board'|'files'|'terminal'{
 try{const value=storage.getItem('kaiwu.view.'+project);return value==='terminal'||value==='files'?value:'board'}catch{return 'board'}
}
export function writeMainView(storage:StorageLike,project:string,value:string){try{storage.setItem('kaiwu.view.'+project,value)}catch{}}
export function readPendingTerminal(storage:StorageLike,project:string,task:string):string{
 try{const value=storage.getItem('kaiwu.terminal.pending.'+project+'/'+task)??'';return value.length<=100?value:''}catch{return ''}
}
export function writePendingTerminal(storage:StorageLike,project:string,task:string,value:string){try{storage.setItem('kaiwu.terminal.pending.'+project+'/'+task,value)}catch{}}
