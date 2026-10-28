import { reactive } from 'vue';

export interface Profile {
  id: string; username: string; displayName: string; status: string; reason: string;
  trafficLimit: number; usedTraffic: number; expiresAt: string; allowedProtocols: string[];
  deviceLimitEnforced: boolean; deviceLimitUnsupportedNodes: string[];
 deviceLimit: number | null; onlineDevices: number | null; wireGuardNote: string;
}
export interface PortalNode { id: string; name: string; region: string; online: boolean; }
export interface Subscription { state: 'ready' | 'missing' | 'not_recoverable'; path?: string; formats?: { id: string; url: string }[]; }
export interface Snapshot { me: Profile; nodes: { items: PortalNode[] }; subscription: Subscription; }
export interface Session { isAuthenticated: boolean; currentUser: Profile; csrfToken: string; }
export interface Traffic { items: { bucket: string; uploadBytes: number; downloadBytes: number }[]; granularity: string; }
export const portalState = reactive<{ session: Session | null }>({ session: null });

// Full literal API paths make this boundary statically auditable. Portal CSRF
// state is never shared with the administrator API client.
async function request<T>(path: `/api/v1/portal/${string}`, method = 'GET', body?: unknown): Promise<T> {
  if (!path.startsWith('/api/v1/portal/')) throw new Error('门户接口地址无效');
  const headers: Record<string, string> = { Accept: 'application/json' };
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  if (method !== 'GET' && portalState.session) headers['X-CSRF-Token'] = portalState.session.csrfToken;
  const response = await fetch(path, { method, headers, credentials: 'same-origin', cache: 'no-store',
    body: body === undefined ? undefined : JSON.stringify(body), signal: AbortSignal.timeout(15000) });
  if (!response.ok) {
    const data = await response.json().catch(() => ({}));
    // A wrong current password is a 401 from a still-valid session.
    const credentialCheck = path.endsWith('/auth/login') || path.endsWith('/auth/password');
    if (response.status === 401 && !credentialCheck) {
      portalState.session = null;
      window.location.assign('/portal/login');
    }
    throw new Error(response.status === 401 && path.endsWith('/auth/password') ? '当前密码错误。' :
      response.status === 401 ? '用户名或密码错误，或登录已失效。' :
      response.status === 429 ? '登录尝试次数过多，请稍后重试。' :
      data.error?.code === 'invalid_request' ? '输入无效，请检查密码是否符合要求（12 至 128 个字符）。' :
      data.error?.code === 'subscription_unavailable' ? '账号已禁用、到期或额度已用完，暂时无法重置订阅。' : '请求失败，请稍后重试。');
  }
  return response.status === 204 ? undefined as T : response.json();
}

export const portalAPI = {
  async session() { return portalState.session = await request<Session>('/api/v1/portal/auth/session'); },
  async login(username: string, password: string) {
    return portalState.session = await request<Session>('/api/v1/portal/auth/login', 'POST', { username, password });
  },
  async logout() { await request('/api/v1/portal/auth/logout', 'POST'); portalState.session = null; },
  devicesOnline: () => request<{ items: { address: string; allowed: boolean; active: boolean }[] }>("/api/v1/portal/devices-online"),
 me: () => request<Profile>('/api/v1/portal/me'),
  nodes: () => request<{ items: PortalNode[] }>('/api/v1/portal/nodes'),
  subscription: () => request<Subscription>('/api/v1/portal/subscription'),
  reset: () => request<Subscription>('/api/v1/portal/subscription/reset', 'POST'),
  traffic: (range: '24h' | '7d' | '30d') => request<Traffic>(`/api/v1/portal/traffic?range=${range}`),
  password: (currentPassword: string, newPassword: string) =>
    request('/api/v1/portal/auth/password', 'PUT', { currentPassword, newPassword })
};
