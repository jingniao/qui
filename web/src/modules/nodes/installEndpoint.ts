import { settingsAPI, type InstallEndpoint } from '../system/settingsApi';

// The three install dialogs (server enrollment, original-identity recovery and
// the core panel on the versions page) default the panel address to what the
// Owner saved once in 系统设置 → 节点安装连接地址. When nothing is saved, or the
// setting cannot be read (Viewer, offline, old panel), they keep the address the
// browser used to reach the panel — the behaviour before that setting existed.
export interface ResolvedInstallEndpoint {
  host: string;
  port: number;
  // '' means "same as the panel address": the command then omits --grpc-host.
  grpcHost: string;
  // null means "not saved": the dialog keeps the Agent gRPC port the panel
  // actually listens on, which the enrollment response carries.
  grpcPort: number | null;
  // True when at least one field came from the saved setting, so the dialog can
  // say where the default came from. A one-off edit in the dialog never writes
  // back to the setting.
  fromSettings: boolean;
}

// browserInstallEndpoint is the fallback: the host and Web port the browser is
// on. An empty port means the default HTTPS port.
export function browserInstallEndpoint(): { host: string; port: number } {
  return { host: window.location.hostname, port: Number(window.location.port) || 443 };
}

// One fetch per page load: every dialog in the session reuses the first answer.
// A failed fetch is not cached, so opening a dialog later retries.
let cached: InstallEndpoint | null = null;
let pending: Promise<InstallEndpoint | null> | null = null;

export async function loadInstallEndpoint(): Promise<InstallEndpoint | null> {
  if (cached) return cached;
  if (!pending) {
    pending = settingsAPI.installEndpoint()
      .then(value => (cached = value))
      .catch(() => null)
      .finally(() => { pending = null; });
  }
  return pending;
}

export function clearInstallEndpointCache() {
  cached = null;
  pending = null;
}

// resolveInstallEndpoint answers the defaults for one dialog. Each field falls
// back independently, so a saved port is still used when no host was saved.
export async function resolveInstallEndpoint(): Promise<ResolvedInstallEndpoint> {
  const fallback = browserInstallEndpoint();
  const saved = await loadInstallEndpoint();
  if (!saved) return { ...fallback, grpcHost: '', grpcPort: null, fromSettings: false };
  const host = /^[A-Za-z0-9.-]+$/.test(saved.host) ? saved.host : '';
  const port = Number.isInteger(saved.httpPort) && saved.httpPort >= 1 && saved.httpPort <= 65535 ? saved.httpPort : 0;
  const grpcHost = /^[A-Za-z0-9.-]+$/.test(saved.grpcHost ?? '') ? saved.grpcHost : '';
  const grpcPort = Number.isInteger(saved.grpcPort) && saved.grpcPort >= 1 && saved.grpcPort <= 65535 ? saved.grpcPort : null;
  return { host: host || fallback.host, port: port || fallback.port, grpcHost, grpcPort,
    fromSettings: Boolean(host || port || grpcHost || grpcPort) };
}
