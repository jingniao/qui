let csrfToken = '';
export function setCSRFToken(token: string) { csrfToken = token; }

export class APIError extends Error {
  constructor(public status: number, public code: string, message: string) { super(message); }
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
  subscription_unavailable: '订阅已失效或当前用户、设备不可用。'
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
    throw new APIError(response.status, code, messages[code] || data.error?.message || '请求失败');
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


