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
  unmanaged_user_credentials: '配置版本不能直接携带用户凭据，请在入站或用户接入中配置'
};

export async function request<T>(path: string, method = 'GET', body?: unknown, options: { timeoutMs?: number; retried?: boolean } = {}): Promise<T> {
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
    // If CSRF verification failed and haven't retried yet, refresh the session to get a fresh token and retry once!
    if (code === 'csrf_failed' && !options.retried && !['GET', 'HEAD'].includes(method)) {
      try {
        const sessionRes = await fetch('/api/v1/auth/session', { headers: { Accept: 'application/json' }, credentials: 'same-origin' });
        if (sessionRes.ok) {
          const sessionData = await sessionRes.json();
          if (sessionData?.csrfToken) {
            setCSRFToken(sessionData.csrfToken);
            return request<T>(path, method, body, { ...options, retried: true });
          }
        }
      } catch {
        // Fall back to original error handling
      }
    }
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
