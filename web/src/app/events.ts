import { authState, refreshSession } from './auth';
import { nodeStatusDetail, parseNodeStatusMessage } from './nodeStatusEvents';
let connection: EventSource | null = null;
let settingsRefresh: Promise<void> | null = null;
let settingsRefreshAgain = false;
export function refreshSettingsView() {
  if (settingsRefresh) { settingsRefreshAgain = true; return settingsRefresh; }
  settingsRefresh = (async () => {
    do {
      settingsRefreshAgain = false;
      await refreshSession();
      if (!authState.isAuthenticated) { window.dispatchEvent(new Event('auth-expired')); return; }
      window.dispatchEvent(new Event('proxy-system-settings'));
    } while (settingsRefreshAgain);
  })().finally(() => { settingsRefresh = null; });
  return settingsRefresh;
}
export function connectEvents() {
  if (connection) return;
  authState.sseStatus = 'reconnecting';
  connection = new EventSource('/api/v1/events', { withCredentials: true });
  connection.onopen = () => { authState.sseStatus = 'connected'; window.dispatchEvent(new CustomEvent('proxy-node-status', { detail: nodeStatusDetail('') })); void refreshSettingsView().catch(() => {}); };
  connection.onerror = async () => { authState.sseStatus = 'reconnecting'; try { await refreshSession(); if (!authState.isAuthenticated) window.dispatchEvent(new Event('auth-expired')); } catch { /* EventSource retries temporary failures. */ } };
  connection.addEventListener('deployment_status', () => window.dispatchEvent(new Event('proxy-deployment-status')));
  connection.addEventListener('node_status', (event) => window.dispatchEvent(new CustomEvent('proxy-node-status', { detail: parseNodeStatusMessage((event as MessageEvent).data) })));
  connection.addEventListener('system_settings', () => { void refreshSettingsView().catch(() => {}); });
  connection.addEventListener('dashboard_status', () => window.dispatchEvent(new Event('proxy-dashboard-status')));
  connection.addEventListener('backup_status', () => window.dispatchEvent(new Event('proxy-backup-status')));
  connection.addEventListener('system.alert', () => window.dispatchEvent(new Event('proxy-system-alert')));
  connection.addEventListener('notification', () => window.dispatchEvent(new Event('proxy-notification')));
}
export function disconnectEvents() { connection?.close(); connection = null; authState.sseStatus = 'disconnected'; }



