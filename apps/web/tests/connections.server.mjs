import {spawn,execFileSync} from 'node:child_process';
import {mkdtempSync,rmSync} from 'node:fs';
import {createServer} from 'node:http';
import {tmpdir} from 'node:os';
import {join,resolve} from 'node:path';
const dir=mkdtempSync(join(tmpdir(),'kaiwu-connections-')),root=resolve('../..');
let child,stopping=false;
const upstream=createServer((req,res)=>{
 const key=req.headers.authorization||req.headers['x-api-key']||req.headers['x-goog-api-key']||'';
 if(String(key).includes('invalid')){res.writeHead(401,{'Content-Type':'application/json'});res.end(JSON.stringify({error:String(key)}));return}
 res.writeHead(200,{'Content-Type':'application/json'});
 res.end(JSON.stringify({data:[{id:'demo-model',display_name:'Demo Model'},{id:'another-model',display_name:'Another Model'}]}));
});
function stop(){if(stopping)return;stopping=true;upstream.close();if(child){child.kill('SIGTERM');setTimeout(()=>child.kill('SIGKILL'),8000).unref()}else{rmSync(dir,{recursive:true,force:true});process.exit(1)}}
process.on('SIGTERM',stop);process.on('SIGINT',stop);
try{
 const env={...process.env,GOTOOLCHAIN:'local'};
 execFileSync('go',['-C',join(root,'server'),'build','-o',join(dir,'fakepi'),'./internal/agent/testdata/fakepi'],{env,stdio:'inherit',timeout:60000});
 execFileSync('go',['-C',join(root,'server'),'build','-o',join(dir,'server'),'./cmd/kaiwu-server'],{env,stdio:'inherit',timeout:60000});
 upstream.listen(17334,'127.0.0.1',()=>{
 child=spawn(join(dir,'server'),[],{env:{...env,KAIWU_WEB_TOKEN:'kaiwu-connections-test-token-00000',KAIWU_DATA_DIR:join(dir,'data'),KAIWU_LISTEN_ADDR:'127.0.0.1:17331',KAIWU_PI_PROGRAM:join(dir,'fakepi'),PI_CODING_AGENT_DIR:join(dir,'no-personal-pi')},stdio:'inherit'});
 child.on('exit',code=>{upstream.close();rmSync(dir,{recursive:true,force:true});process.exit(code??1)});
 });
 upstream.on('error',e=>{console.error(e);stop()});
}catch(e){console.error(e);stop()}
