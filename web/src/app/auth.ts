import { reactive, computed } from 'vue';
import type { AdminUser } from '../types';
import { request, setCSRFToken } from './apiClient';

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
  try { apply(await request<SessionView>('/auth/session')); }
  catch (error) { authState.error = (error as Error).message; authState.ready = false; throw error; }
}
export async function ensureSession() {
  if (authState.ready) return;
  if (!loading) loading = refreshSession().finally(() => { loading = null; });
  return loading;
}
export async function login(username: string, password: string) {
  apply(await request<SessionView>('/auth/login', 'POST', { username, password }));
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
export async function changePassword(currentPassword: string, newPassword: string) {
  await request('/auth/password', 'PUT', { currentPassword, newPassword });
  clearSession();
}
export const isOwner = computed(() => authState.currentUser?.role === 'Owner');
export const isAdmin = isOwner;
export const isViewer = computed(() => authState.currentUser?.role === 'Viewer');

export function formatTime(value: string) {
  if (!value) return '从未';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : new Intl.DateTimeFormat('zh-CN', {
    timeZone: authState.timezone, dateStyle: 'short', timeStyle: 'medium', hour12: false
  }).format(date);
}

