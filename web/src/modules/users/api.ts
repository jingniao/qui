import { request, type Page } from '../../app/apiClient';
export interface ProxyUser { id: string; username: string; displayName: string; status: string; expiresAt: string; trafficLimit: number; usedTraffic: number; maxDevices: number; maxConnections: number; allowedProtocols: string[]; nodeGroupIds: string[]; remark: string; createdAt: string; devicesCount: number; onlineConnections: number | null; overLimit: boolean; }
export interface UserInput { username: string; displayName: string; status: string; expiresAt: string; trafficLimit: number; maxDevices: number; maxConnections: number; allowedProtocols: string[]; nodeGroupIds: string[]; remark: string; }
export interface DeviceCredential { id: string; userId: string; deviceId: string; protocol: string; username: string; secret?: string; publicKey?: string; status: string; createdAt: string; }
export interface ProxyDevice { id: string; userId: string; name: string; status: string; createdAt: string; lastOnlineAt: string; lastNodeId: string; usedTraffic: number; credentials: DeviceCredential[]; }
export interface TokenView { id: string; userId: string; deviceId: string; createdAt: string; lastFetchedAt: string; }
// The protocols a user or device may be granted. The list equals the inbound
// basis of desiredstate.ProtocolSupportTable (docs/protocol-support-matrix.md);
// the proxy module reads the same basis from GET /api/v1/protocol-support.
export const protocols = ['http', 'socks5', 'shadowsocks', 'vless', 'trojan', 'wireguard', 'hysteria2', 'tuic'];
export const userAPI = {
  list: (page: number, keyword = '', pageSize = 20) => request<Page<ProxyUser>>(`/users?page=${page}&pageSize=${pageSize}&keyword=${encodeURIComponent(keyword)}`),
  get: (id: string) => request<ProxyUser>(`/users/${id}`),
  save: (id: string | null, value: UserInput) => request<ProxyUser>(id ? `/users/${id}` : '/users', id ? 'PUT' : 'POST', value),
  delete: (id: string) => request(`/users/${id}`, 'DELETE'),
  devices: (userId: string, page = 1, pageSize = 20) => request<Page<ProxyDevice>>(userId ? `/users/${userId}/devices?page=${page}&pageSize=${pageSize}` : `/devices?page=${page}&pageSize=${pageSize}`),
  createDevice: (userId: string, name: string, protocols: string[]) => request<ProxyDevice>(`/users/${userId}/devices`, 'POST', { name, protocols }),
  updateDevice: (device: ProxyDevice, status: string) => request(`/devices/${device.id}`, 'PUT', { name: device.name, status }),
  deleteDevice: (id: string) => request(`/devices/${id}`, 'DELETE'),
  resetCredential: (deviceId: string, protocol: string) => request<DeviceCredential>(`/devices/${deviceId}/credentials/${protocol}/reset`, 'POST'),
  revokeCredential: (deviceId: string, protocol: string) => request(`/devices/${deviceId}/credentials/${protocol}`, 'DELETE'),
  rotateToken: (userId: string, deviceId: string) => request<{ subscription: TokenView; token: string; path: string }>(`/users/${userId}/subscription-token`, 'POST', { deviceId }),
  revokeToken: (userId: string) => request(`/users/${userId}/subscription-token`, 'DELETE'),
  tokens: (page: number) => request<Page<TokenView>>(`/subscriptions?page=${page}&pageSize=20`),
  preview: (userId: string, deviceId: string) => request<{ items: { nodeId: string; nodeName: string; inboundId: string; protocol: string; address: string; port: number }[]; total: number }>(`/users/${userId}/subscription-preview?deviceId=${deviceId}`)
};
