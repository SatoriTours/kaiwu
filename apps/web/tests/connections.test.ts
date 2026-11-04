import {test} from 'node:test';
import assert from 'node:assert/strict';
import {connectionDraft, mergeDiscoveredModels, cleanModelID, credentialChanged} from '../src/connections.ts';
test('editing starts with no returned key and independent selected models',()=>{
 const original={id:'one',revision:2,name:'my',provider:'kaiwu-one',api:'openai-completions',baseUrl:'https://example.com',models:['a'],kind:'api_key',hasKey:true};
 const draft=connectionDraft(original);assert.equal(draft.key,'');draft.models.push('b');assert.deepEqual(original.models,['a']);assert.equal(draft.id,'one');
});
test('refresh deduplicates models and keeps selected manual or temporarily missing models',()=>{
 const result=mergeDiscoveredModels([{id:'manual',name:'manual',manual:true}],[{id:'fresh',name:'Fresh'},{id:'fresh',name:'Duplicate'}],['manual','old']);
 assert.deepEqual(result.map(m=>m.id),['fresh','manual','old']);assert.equal(result.find(m=>m.id==='old')?.manual,true);
});
test('unchosen old directory entries disappear when refreshed',()=>{
 assert.deepEqual(mergeDiscoveredModels([{id:'old',name:'Old'}],[{id:'new',name:'New'}],[]).map(m=>m.id),['new']);
});
test('manual model rejects empty, controls, command prefix and overlong IDs',()=>{
 for(const id of ['','!bad','a\nb','x'.repeat(301)])assert.equal(cleanModelID(id),undefined);
 assert.equal(cleanModelID(' models/example '),'models/example');
});
test('URL and protocol changes require a fresh secret on edit',()=>{
 const c={...connectionDraft(),hasKey:true};
 assert.equal(credentialChanged(c,{...c,baseUrl:'https://other.example'}),true);
 assert.equal(credentialChanged(c,{...c,name:'renamed'}),false);
});
