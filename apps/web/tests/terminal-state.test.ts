import assert from 'node:assert/strict';
import {describe,it} from 'node:test';
import {readTerminalView,writeTerminalView,chooseTerminal} from '../src/terminal-state.ts';

function memory(){const values=new Map<string,string>();return {getItem:(k:string)=>values.get(k)??null,setItem:(k:string,v:string)=>{values.set(k,v)}}}
describe('terminal selection only persistence',()=>{
 it('restores scope and selected process without persisting shell data',()=>{
  const storage=memory();
  writeTerminalView(storage,'project',{taskId:'task',terminalId:'second',closed:false});
  assert.deepEqual(readTerminalView(storage,'project'),{taskId:'task',terminalId:'second',closed:false});
  assert.equal(readTerminalView(storage,'another').terminalId,'');
 });
 it('tolerates blocked storage and malformed records',()=>{
  const storage={getItem(){throw Error('blocked')},setItem(){throw Error('blocked')}};
  assert.equal(readTerminalView(storage,'project').terminalId,'');
  assert.doesNotThrow(()=>writeTerminalView(storage,'project',{taskId:'',terminalId:'',closed:true}));
 });
 it('uses stable process ID even when tab labels match',()=>{
  const list=[{id:'one',running:true,number:1},{id:'two',running:true,number:2}];
  assert.equal(chooseTerminal(list,'two')?.id,'two');
  assert.equal(chooseTerminal(list,'missing')?.id,'one');
 });
});
