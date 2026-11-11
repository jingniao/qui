// The one Chinese name list for a deployment result code. The configuration
// history, the server detail page and the node list all show these.
const resultLabels: Record<string, string> = {
  success: '已应用且健康',
  validation_failed: '核心校验失败',
  compile_failed: '配置编译失败',
  apply_failed: '应用失败',
  storage_preflight_failed: '存储格式检查失败',
  revision_store_failed: '保存配置修订失败',
  start_failed: '启动失败',
  rollback_failed: '回滚失败',
  stop_failed: '停止失败',
  persistence_failed: '保存成功清单失败，已恢复旧配置',
  failed_closed: '保护性停止',
  accounting_interrupted: '记账中断',
  failed: '发布失败',
  rollback: '发布失败，已回滚',
};

export function revisionResult(code: string): string {
  return resultLabels[code] || code || '—';
}
