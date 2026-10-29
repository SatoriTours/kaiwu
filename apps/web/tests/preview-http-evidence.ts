import {createHash} from 'node:crypto';
import {readFileSync, readdirSync, writeFileSync} from 'node:fs';
import {resolve} from 'node:path';
import type {Page, TestInfo, Frame, Response} from '@playwright/test';

// Only this isolated preview scenario. No DOM hooks, snapshots, request routing,
// credentials, arbitrary response bodies, or background collection.
export function previewHTTPEvidence(page:Page, info:TestInfo) {
 const start=Date.now(), events:any[]=[], pending:Promise<unknown>[]=[], caps=new Map<string,number>();
 const frames=new Map<Frame,number>(), pages=new Map<Page,number>(), removers:(()=>void)[]=[];
 const hash=(body:Buffer|string)=>createHash('sha256').update(body).digest('hex');
 const mask=(value:string)=>value.replace(/\/preview\/[^/\s?#"'\\]+/g,key=>{
  if(!caps.has(key))caps.set(key,caps.size+1);
  return '/preview/<cap-'+caps.get(key)+'>';
 });
 const safeURL=(value:string)=>{try{const u=new URL(value);return u.protocol==='about:'?'about:blank':mask(u.origin+u.pathname)}catch{return '<invalid-url>'}};
 const frameID=(f:Frame)=>{if(!frames.has(f))frames.set(f,frames.size+1);return frames.get(f)};
 const record=(type:string,data:any={})=>{if(events.length<400)events.push({ms:Date.now()-start,type,...data})};
 const root=resolve('../..');
 const source:Record<string,string>={};
 for(const name of ['apps/web/src/App.vue','apps/web/src/ResultPreview.vue','apps/web/tests/browser.spec.ts',
  'apps/web/tests/view-navigation.ts','apps/web/tests/preview-http-evidence.ts','apps/web/playwright.config.ts',
  'apps/web/tests/server.mjs','apps/web/scripts/copy-build.mjs','server/internal/preview/gateway.go','server/internal/preview/manager.go']){
  source[name]=hash(readFileSync(resolve(root,name)));
 }
 const assets:Record<string,string>={};
 for(const directory of ['apps/web/dist/assets','server/internal/webui/dist/assets']){
  for(const file of readdirSync(resolve(root,directory)))assets[directory+'/'+file]=hash(readFileSync(resolve(root,directory,file)));
 }
 const metadata={base:process.env.KAIWU_EVIDENCE_BASE??'unknown',source,assets,node:process.version,
  browser:page.context().browser()?.version(),workers:info.config.workers,retry:info.retry,
  trace:info.project.use.trace??'off',viewport:info.project.use.viewport??'default',project:info.project.name};
 const on=(emitter:any,event:string,listener:(...args:any[])=>void)=>{
  emitter.on(event,listener);removers.push(()=>emitter.off(event,listener));
 };
 let nextPage=0, stage='setup';
 const install=(p:Page)=>{
  const pageId=++nextPage;pages.set(p,pageId);
  on(p,'frameattached',(f:Frame)=>record('frame-attached',{pageId,frame:frameID(f)}));
  on(p,'framedetached',(f:Frame)=>record('frame-detached',{pageId,frame:frameID(f),url:safeURL(f.url())}));
  on(p,'framenavigated',(f:Frame)=>record('navigation',{pageId,frame:frameID(f),url:safeURL(f.url())}));
  on(p,'request',q=>{if(new URL(q.url()).pathname.startsWith('/preview/'))record('request',{
   pageId,frame:frameID(q.frame()),url:safeURL(q.url()),resource:q.resourceType(),origin:q.headers().origin})});
  on(p,'requestfailed',q=>{if(q.url().includes('/preview/'))record('request-failed',{
   pageId,url:safeURL(q.url()),errorHash:hash(q.failure()?.errorText??'unknown')})});
  on(p,'pageerror',e=>record('page-error',{pageId,messageHash:hash(e.message)}));
  on(p,'console',m=>{if(m.type()==='error')record('console-error',{pageId,messageHash:hash(m.text())})});
  on(p,'response',(response:Response)=>{
   const u=new URL(response.url()), url=safeURL(response.url());
   if(u.pathname.startsWith('/preview/'))record('response',{
    pageId,url,status:response.status(),cors:response.headers()['access-control-allow-origin']});
   const api=/^\/preview\/[^/]+\/api\/hello$/.test(u.pathname);
   const asset=u.pathname.startsWith('/assets/')&&/\.(js|css)$/.test(u.pathname);
   if(!api&&!asset)return;
   // Request bytes now, while the document still exists, not after Stop/navigation.
   pending.push(response.body().then(body=>record(api?'api-body':'served-asset',{
    pageId,url,sha256:hash(body),size:body.length,...(api?{fixtureBody:body.toString()==='API OK'?'API OK':'<other-body-redacted>'}:{})
   })).catch(()=>record('body-unavailable',{pageId,url})));
  });
 };
 install(page);on(page.context(),'page',install);
 return {
  stage(name:string){stage=name;record('stage',{name})},
  async run(body:()=>Promise<void>){
   let outcome='passed';
   try{await body()}catch(error){
    outcome='failed';record('failed',{stage});
    // Failure-only, after the original assertion has already failed. Never a
    // pre-click readiness probe and never a retry of the failed interaction.
    const postmortem=Promise.allSettled(page.context().pages().flatMap(p=>p.frames().map(async f=>{
     const state=await f.evaluate(()=>{
      const button=document.querySelector<HTMLButtonElement>('#ping');
      const rect=(el:Element|null)=>el?.getBoundingClientRect().toJSON();
      return {ready:document.readyState,visible:document.visibilityState,focus:document.hasFocus(),
       active:document.activeElement?.tagName,hover:[...document.querySelectorAll(':hover')].map(el=>el.tagName),
       handler:button?typeof button.onclick:null,button:rect(button),buttonHovered:button?.matches(':hover'),
       parentAccess:document.body?.dataset.parentAccess,
       frameBounds:[...document.querySelectorAll('iframe')].map(el=>rect(el))};
     });
     record('post-failure-document',{pageId:pages.get(p),frame:frameID(f),url:safeURL(f.url()),...state});
    })));
    let timer:ReturnType<typeof setTimeout>|undefined;
    await Promise.race([postmortem,new Promise<void>(done=>{timer=setTimeout(done,1000)})]);clearTimeout(timer);
    throw error;
   }
   finally{
    // A bounded flush is cleanup only, never a readiness wait in the scenario.
    let timer:ReturnType<typeof setTimeout>|undefined;
    await Promise.race([Promise.allSettled(pending),new Promise<void>(done=>{timer=setTimeout(done,1000)})]);
    clearTimeout(timer);for(const remove of removers)remove();
    writeFileSync(info.outputPath('http-preview-evidence.json'),JSON.stringify({metadata,outcome,stage,events},null,2)+'\n');
   }
  }
 };
}
