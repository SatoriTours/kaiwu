export type CreationPhase = 'recovered' | 'starting' | 'needs_model' | 'failed' | 'unknown';
export interface CreationNotice {taskId:string; phase:CreationPhase; message:string}
export function creationFailure(taskId:string, error:unknown):CreationNotice {
 const uncertain=!!error && typeof error==='object' && 'status' in error && error.status===0;
 return {taskId,phase:uncertain?'unknown':'failed',message:uncertain
  ?'卡片已创建，启动结果待确认；不会自动重复启动'
  :'卡片已创建，启动失败：'+(error instanceof Error?error.message:'请检查配置')};
}
