import assert from 'node:assert/strict';
import {checksToUI,uiToChecks} from '../src/delivery-settings.ts';
const checks=[{id:'test',argv:['sh','scripts/check.sh'],dir:'.',timeoutSeconds:30,definitionPaths:['scripts/check.sh','apps/web/package.json']}];
const ui=checksToUI(checks);
assert.deepEqual(uiToChecks(ui),checks,'reopening and saving must preserve declared protected definitions');
ui[0].definitionPathsText='scripts/check file.sh\n\n package.json \r\n';
assert.deepEqual(uiToChecks(ui)[0].definitionPaths,['scripts/check file.sh','package.json']);
console.log('delivery definition paths round-trip passed');
