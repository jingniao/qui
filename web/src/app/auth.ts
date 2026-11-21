import { reactive, computed, watch } from 'vue';
import type { AdminUser } from '../types';
import { request, setCSRFToken, advanceRequestScope, requestScope } from './apiClient';

interface SessionView {
  isAuthenticated: boolean;
  isInitialized: boolean;
  currentUser: AdminUser | null;
  csrfToken: string;
  expiresAt?: string;
  timezone: string;
  platformName: string;
}
export const authState = reactive({
  isAuthenticated: false,
  isInitialized: false,
  currentUser: null as AdminUser | null,
  loginGeneration: 0,
  ready: false,
  error: '',
  sseStatus: 'disconnected' as 'connected' | 'reconnecting' | 'disconnected',
  timezone: 'Asia/Shanghai',
  platformName: 'Proxy Control'
});
let loading: Promise<void> | null = null;

function apply(session: SessionView) {
  authState.isAuthenticated = session.isAuthenticated;
  authState.isInitialized = session.isInitialized;
  authState.currentUser = session.currentUser;
  authState.ready = true;
  authState.error = '';
  authState.timezone = session.timezone;
  authState.platformName = session.platformName;
  setCSRFToken(session.csrfToken);
}

export async function refreshSession() {
  const scope = requestScope();
  try {
    const session = await request<SessionView>('/auth/session');
    if (scope === requestScope()) apply(session);
  } catch (error) {
    if (scope !== requestScope()) return;
    authState.error = (error as Error).message; authState.ready = false; throw error;
  }
}
export async function ensureSession() {
  if (authState.ready) return;
  if (!loading) loading = refreshSession().finally(() => { loading = null; });
  return loading;
}
export async function login(username: string, password: string) {
  const session = await request<SessionView>('/auth/login', 'POST', { username, password });
  authState.loginGeneration++;
  apply(session);
}
export async function completeSetup(token: string, username: string, password: string, platformName: string, timezone: string) {
  apply(await request<SessionView>('/auth/setup', 'POST', { token, username, password, platformName, timezone }));

}
export async function logout() {
  await request('/auth/logout', 'POST');
  clearSession();
}
export function clearSession() {
  authState.isAuthenticated = false;
  authState.currentUser = null;
  setCSRFToken('');
}
export const isOwner = computed(() => authState.currentUser?.role === 'Owner');
export const adminReadScope = computed(() => authState.isAuthenticated && authState.currentUser
  ? JSON.stringify([authState.currentUser.id, authState.currentUser.role, authState.loginGeneration]) : '');
watch(adminReadScope, advanceRequestScope, { flush: 'sync' });
export const isViewer = computed(() => authState.currentUser?.role === 'Viewer');

export { formatTime } from './time';
