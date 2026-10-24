// Keep the real service's 10/min login and 8-resident limits intact.
// Every test is selected by Playwright's individual-test sharding, not a
// hand-maintained grep list. Each small shard gets a fresh, gracefully reaped
// service; the existing port and workers:1 remain strictly serial.
import { spawnSync } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const cwd = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const args = [join(cwd, 'node_modules/@playwright/test/cli.js'), 'test',
  '-c', 'playwright.chat-turn.config.ts'];
const discovery = spawnSync(process.execPath, [...args, '--list', '--reporter=json'],
  { cwd, encoding: 'utf8', timeout: 15000, maxBuffer: 4 * 1024 * 1024 });
if (discovery.error || discovery.status !== 0) {
  console.error(discovery.error || discovery.stderr || discovery.stdout);
  process.exit(1);
}
const report = JSON.parse(discovery.stdout);
function count(suite) {
  return (suite.specs || []).reduce((n, spec) => n + spec.tests.length, 0)
    + (suite.suites || []).reduce((n, child) => n + count(child), 0);
}
const total = count(report);
if (report.errors?.length || !total) throw new Error('Test discovery failed or selected no tests');
const shards = Math.ceil(total / 4);
const output = mkdtempSync(join(tmpdir(), 'kaiwu-chat-turn-batches-'));
console.log('[chat-turn-batches]', JSON.stringify({ total, shards, workers: 1 }));
for (let shard = 1; shard <= shards; shard++) {
  console.log('[chat-turn-batch]', shard + '/' + shards);
  const result = spawnSync(process.execPath, [...args, '--shard=' + shard + '/' + shards,
    '--output=' + join(output, String(shard))], { cwd, stdio: 'inherit', timeout: 180000 });
  if (result.error || result.status !== 0) {
    console.error(result.error || 'Shard failed: ' + shard, 'Evidence retained:', output);
    process.exit(result.status > 0 ? result.status : 1);
  }
}
rmSync(output, { recursive: true, force: true }); // Only this run's passing output.
console.log('[chat-turn-batches] All ' + total + ' discovered tests passed in ' + shards + ' fresh serial services.');
