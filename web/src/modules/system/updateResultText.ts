// Immutable node results describe the original attempt, not current runtime health.
const labels: Record<string, string> = {
  success: '节点报告升级成功',
  rolled_back: '节点报告已回退',
  scope_mismatch: '请求范围与节点状态不符',
  trust_failed: '签名、信任或发布序号核验未通过',
  download_failed: '版本材料下载或保存未完成',
  preflight_failed: '切换前校验未通过',
  history_invalid: '原更新历史不可用或不一致',
  installation_failed: '安装或启动结果未确认，保留保护',
  recovery_failed: '恢复结果未确认，保留保护',
};
const guidance: Record<string, string> = {
  scope_mismatch: '刷新核对节点、程序平台与当前配置版本。新操作须绑定当前事实；原尝试记录保留。',
  trust_failed: '核对节点原发布公钥、签名时效和已接受序号，不降低序号或扩大信任。',
  download_failed: '核对可信发布目录与磁盘材料；保留原下载、接受和任务记录。',
  preflight_failed: '核对程序能力、存储契约、核心配置及当前版本，保留原结果。',
  history_invalid: '先核对原事件链、独立检查点与密钥。使用固定本地诊断，不重建空历史。',
  installation_failed: '保留原事务、已确认镜像和消费状态；使用对应固定诊断与显式恢复入口。',
  recovery_failed: '核对原材料及本次恢复尝试；原失败不可改写，条件满足后提交新恢复尝试。',
};
export function updateResultLabel(code?: string): string {
  return code ? Object.prototype.hasOwnProperty.call(labels, code) ? labels[code] : '结果类型需要核对' : '等待节点确认';
}
export function updateResultGuidance(code?: string): string {
  return code && Object.prototype.hasOwnProperty.call(guidance, code) ? guidance[code] : '';
}
