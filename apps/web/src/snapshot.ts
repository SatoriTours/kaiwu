// Owned by one socket. A disconnect discards incomplete snapshots atomically.
export class SnapshotAssembler {
 private parts:string[]|null=null;
 private expected=0;
 accept(frame:any):any|null {
  if(frame.type==='snapshot_start'){
   if(this.parts||!Number.isSafeInteger(frame.parts)||frame.parts<1)throw new Error('invalid snapshot start');
   this.parts=[];this.expected=frame.parts;return null;
  }
  if(frame.type==='snapshot_chunk'){
   if(!this.parts||frame.index!==this.parts.length||this.parts.length>=this.expected||typeof frame.data!=='string'||frame.data.length>256*1024)throw new Error('invalid snapshot chunk');
   this.parts.push(frame.data);return null;
  }
  if(frame.type==='snapshot_end'){
   if(!this.parts||this.parts.length!==this.expected)throw new Error('incomplete snapshot');
   const result=JSON.parse(this.parts.join(''));this.parts=null;this.expected=0;
   if(result.type!=='snapshot')throw new Error('invalid snapshot content');return result;
  }
  if(this.parts)throw new Error('interleaved snapshot');
  return frame;
 }
}
