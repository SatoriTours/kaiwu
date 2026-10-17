export interface Patch {after:number;revision:number;ops:{op:string;path:string[];value?:any}[]}
export function applyPatch<T extends {revision:number}>(state:T,patch:Patch):T {
 if(state.revision!==patch.after||patch.revision<=patch.after)throw new Error('同步版本缺口');
 const next={...state};
 const owned=new Set<object>([next]);
 for(const item of patch.ops){
  if(!item.path.length||item.path.some(k=>['__proto__','prototype','constructor'].includes(k)))throw new Error('同步路径无效');
  let parent:any=next;
  for(const key of item.path.slice(0,-1)){if(!parent||!Object.hasOwn(parent,key))throw new Error('同步目标缺失');if(!parent[key]||typeof parent[key]!=="object")throw new Error("同步容器无效");if(!owned.has(parent[key])){parent[key]=Array.isArray(parent[key])?[...parent[key]]:{...parent[key]};owned.add(parent[key])}parent=parent[key]}
  const key=item.path[item.path.length-1]!;
  if(item.op==='remove'){if(Array.isArray(parent))parent.splice(Number(key),1);else delete parent[key]}
  else if(item.op==='append'){if(typeof parent[key]!=='string'||typeof item.value!=='string')throw new Error('同步追加无效');parent[key]+=item.value}
  else if(item.op==='add'||item.op==='replace')parent[key]=item.value??null;
  else throw new Error('同步操作无效');
 }
 if(next.revision!==patch.revision)throw new Error('同步版本无效');
 return next;
}
