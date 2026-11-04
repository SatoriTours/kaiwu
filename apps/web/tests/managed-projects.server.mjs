import {spawn,execFileSync} from 'node:child_process';
import {mkdtempSync,rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join,resolve} from 'node:path';
const dir=mkdtempSync(join(tmpdir(),'kaiwu-managed-projects-')),binary=join(dir,'fixture');
let child;
try{
 execFileSync('go',['-C',resolve('../../server'),'build','-o',binary,'./internal/webapi/testdata/managedfixture'],{env:{...process.env,GOTOOLCHAIN:'local'},stdio:'inherit',timeout:60000});
 child=spawn(binary,['-root',dir],{stdio:'inherit'});
 const stop=()=>{child.kill('SIGTERM');setTimeout(()=>child.kill('SIGKILL'),8000).unref()};
 process.on('SIGTERM',stop);process.on('SIGINT',stop);
 child.on('exit',code=>{rmSync(dir,{recursive:true,force:true});process.exit(code??1)});
}catch(e){console.error(e);rmSync(dir,{recursive:true,force:true});process.exit(1)}
