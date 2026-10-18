// Test launcher only; application and fixture backend are Go.
import { spawn, execFileSync } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
const root = resolve('../..'), dir = mkdtempSync(join(tmpdir(), 'kaiwu-delivery-browser-'));
const env = { ...process.env, GOTOOLCHAIN: 'local' };
for (const key of Object.keys(env)) if (/^(KAIWU_GITHUB_|GITHUB_TOKEN$|GH_TOKEN$|SSH_AUTH_SOCK$)/.test(key)) delete env[key];
const fake = join(dir, 'fakepi'), binary = join(dir, 'fixture');
let child, stopped = false;
const clean = () => rmSync(dir, { recursive: true, force: true });
try {
  execFileSync('go', ['-C', join(root, 'server'), 'build', '-o', fake, './internal/agent/testdata/fakepi'], { env, stdio: 'inherit' });
  execFileSync('go', ['-C', join(root, 'server'), 'build', '-o', binary, './internal/delivery/testdata/webfixture'], { env, stdio: 'inherit' });
  child = spawn(binary, ['-root', dir, '-pi', fake], { env, stdio: 'inherit' });
  const stop = () => {
    if (stopped) return;
    stopped = true; child.kill('SIGTERM');
    setTimeout(() => child.kill('SIGKILL'), 10000).unref();
  };
  process.on('SIGTERM', stop); process.on('SIGINT', stop);
  child.on('error', err => { console.error(err); clean(); process.exit(1); });
  child.on('exit', code => { clean(); process.exit(code ?? 1); });
} catch (err) { console.error(err); clean(); process.exit(1); }
