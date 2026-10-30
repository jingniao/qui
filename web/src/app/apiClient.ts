let csrfToken = '';
export function setCSRFToken(token: string) { csrfToken = token; }

export class APIError extends Error {
  // details carries the extra fields of the error body, e.g. the blockers of a
  // refused node deletion.
  constructor(public status: number, public code: string, message: string, public details: Record<string, unknown> = {}) { super(message); }
}

const messages: Record<string, string> = {
  invalid_credentials: '用户名或密码错误，或账号已被禁用。',
  unauthorized: '登录已过期，请重新登录。',
  forbidden: '当前账号没有执行此操作的权限。',
  csrf_failed: '会话校验失败，请刷新页面后重试。',
  rate_limited: '登录尝试次数过多，请在 15 分钟后重试。',
  owner_protected: 'Owner 账号受保护，不能禁用、删除或由其他账号重置。',
  conflict: '系统已初始化，或该用户名已经存在。',
  not_found: '目标记录不存在。',
  internal_error: '服务暂时无法完成操作，请稍后重试。',
  user_inactive: '用户或设备已禁用、到期，或流量额度已用完。',
  subscription_unavailable: '订阅已失效或当前用户、设备不可用。',
  node_delete_not_allowed: '节点已登记，不能按取消登记处理。',
  node_in_use: '节点已被入站、出站、代理链、分流规则或发布记录引用，请先移除这些配置。',
  node_delete_blocked: '节点仍被配置引用或有进行中的发布，请先处理下列项目。',
  node_deleted: '节点已删除，不能修改。',
  unmanaged_user_credentials: '配置版本不能直接携带用户凭据，请在入站或用户接入中配置',
  node_offline: '服务器当前离线或心跳过期，请等待其重新上线后重试。',
  node_unavailable: '服务器当前不可用，请稍后重试。',
  node_target_limit: '每台服务器最多关联 5 个延迟检测目标。',
  unknown_target: '所选延迟检测目标不存在或已被删除。',
  core_in_use: '该核心仍被入站使用，请先在草稿和已发布配置中移除这些入站。',
  core_pending: '该核心正在添加或移除中，请等待 Agent 上报新的能力后再操作。',
  last_core: '不能移除最后一个已启用的核心。',
  revision_mismatch: '服务器当前确认的配置版本已变化，请刷新后重试。',
  core_unavailable: '该核心未在服务器上上报为启用，不能执行此操作。',
  core_update_conflict: '该服务器此核心已有进行中的更新任务，或版本、地址与摘要与现有任务冲突。',
  restart_conflict: '该核心状态不允许重启，请刷新后重试。',
  health_unavailable: '服务器未上报健康检查能力，或状态不允许发起检查。',
  tuning_busy: '该服务器已有等待确认的调优请求，请等待回执后再提交。',
  tuning_unsupported: '该 Agent 未声明服务器调优能力（需升级到支持调优的版本）。',
  invalid_tuning: '调优请求包含白名单以外的键或超出允许范围的取值。',
  tunnel_in_use: '该隧道仍被方案引用，请先移除这些引用。',
  object_owned: '该对象已由其他隧道认领，不能在此处接管。'
};

export async function request<T>(path: string, method = 'GET', body?: unknown, options: { timeoutMs?: number } = {}): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' };
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  if (!['GET', 'HEAD'].includes(method) && csrfToken) headers['X-CSRF-Token'] = csrfToken;
  let response: Response;
  try {
    response = await fetch('/api/v1' + path, { method, headers, credentials: 'same-origin',
      body: body === undefined ? undefined : JSON.stringify(body), signal: AbortSignal.timeout(options.timeoutMs ?? 15000) });
  } catch { throw new APIError(0, 'unavailable', '无法连接中央服务，请检查服务是否启动。'); }
  if (!response.ok) {
    const data = await response.json().catch(() => ({}));
    const code = data.error?.code || 'request_failed';
    if (response.status === 401 && !path.startsWith('/auth/')) window.dispatchEvent(new Event('auth-expired'));
    throw new APIError(response.status, code, messages[code] || data.error?.message || '请求失败', data.error || {});
  }
  return response.status === 204 ? undefined as T : response.json();
}

// Multipart uploads retain cookie/CSRF rules and let the browser set the boundary.
export async function upload<T>(path: string, body: FormData, timeoutMs = 310000): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' };
  if (csrfToken) headers['X-CSRF-Token'] = csrfToken;
  let response: Response;
  try { response = await fetch('/api/v1' + path, { method: 'POST', headers, credentials: 'same-origin', body, signal: AbortSignal.timeout(timeoutMs) }); }
  catch { throw new APIError(0, 'unavailable', '上传未完成，请检查连接并刷新备份列表确认结果。'); }
  if (!response.ok) {
    const data = await response.json().catch(() => ({})); const code = data.error?.code || 'request_failed';
    if (response.status === 401) window.dispatchEvent(new Event('auth-expired'));
    throw new APIError(response.status, code, messages[code] || data.error?.message || '上传失败');
  }
  return response.json();
}

export interface Page<T> { items: T[]; total: number; page: number; pageSize: number; }
