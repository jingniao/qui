import { request, type Page } from '../../app/apiClient';

export interface ProxyUser {
 trafficResetCycle: 'none' | 'monthly' | '30days';
 trafficResetAnchor: string;
 trafficResetAt: string;
 deviceLimit: number;
 onlineDevices: number | null;
 deviceLimitEnforced: boolean;
 deviceLimitUnsupportedNodes: string[];
 deviceLimitUdpUnsupportedProtocols?: string[] | null;
  loginEnabled: boolean;
  id: string;
  username: string;
  displayName: string;
  status: string;
  expiresAt: string;
  trafficLimit: number;
  usedTraffic: number;
  allowedProtocols: string[];
  nodeGroupIds: string[];
  remark: string;
  createdAt: string;
  onlineConnections: number | null;
}

export interface UserInput {
 trafficResetCycle?: 'none' | 'monthly' | '30days';
 trafficResetAnchor?: string;
 deviceLimit: number;
  username: string;
  displayName: string;
  status: string;
  expiresAt: string;
  trafficLimit: number;
  allowedProtocols: string[];
  nodeGroupIds: string[];
  remark: string;
}

export interface TokenView {
  id: string;
  userId: string;
  deviceId: string;
  createdAt: string;
  lastFetchedAt: string;
}

export interface UserMeta {
  resetCycle: 'none' | 'monthly' | '30days';
  rawRemark: string;
}

// The protocols a user may be granted.
export const protocols = ['http', 'socks5', 'shadowsocks', 'vless', 'trojan', 'wireguard', 'hysteria2', 'tuic'];

export interface DevicesOnline { items: { address: string; allowed: boolean; active: boolean; firstSeen: string; lastSeen: string; nodes: string[] }[]; onlineDevices: number | null; deviceLimit: number; deviceLimitEnforced: boolean; deviceLimitUnsupportedNodes: string[]; deviceLimitUdpUnsupportedProtocols?: string[] | null; }
export const userAPI = {
  grantGroup: (userId: string, groupId: string) => request<void>(`/users/${encodeURIComponent(userId)}/node-groups/${encodeURIComponent(groupId)}`, 'POST'),
  revokeGroup: (userId: string, groupId: string) => request<void>(`/users/${encodeURIComponent(userId)}/node-groups/${encodeURIComponent(groupId)}`, 'DELETE'),
 devicesOnline: (id: string) => request<DevicesOnline>(`/users/${id}/devices-online`),
 resetDevicesOnline: (id: string) => request<DevicesOnline>(`/users/${id}/devices-online/reset`, "POST"),
  list: (page: number, keyword = '', pageSize = 20) =>
    request<Page<ProxyUser>>(`/users?page=${page}&pageSize=${pageSize}&keyword=${encodeURIComponent(keyword)}`),
  get: (id: string) => request<ProxyUser>(`/users/${id}`),
  save: (id: string | null, value: UserInput) =>
    request<ProxyUser>(id ? `/users/${id}` : '/users', id ? 'PUT' : 'POST', value),
  delete: (id: string) => request(`/users/${id}`, 'DELETE'),
  resetTraffic: (userId: string) => request(`/users/${userId}/reset-traffic`, 'POST'),
  // The subscription token belongs to the user (v1.1); a reset revokes the old link.
  rotateToken: (userId: string) =>
    request<{ subscription: TokenView; token: string; path: string }>(`/users/${userId}/subscription-token`, 'POST', {}),
  // Re-issues every proxy credential of the user; the old settings stop working
  // once the automatic revision has been applied on the nodes.
  resetCredentials: (userId: string) => request(`/users/${userId}/credentials/reset`, 'POST'),
  revokeToken: (userId: string) => request(`/users/${userId}/subscription-token`, 'DELETE'),
  tokens: (page: number) => request<Page<TokenView>>(`/subscriptions?page=${page}&pageSize=20`),
  preview: (userId: string) =>
    request<{ items: { nodeId: string; nodeName: string; inboundId: string; protocol: string; address: string; port: number; link?: string }[]; total: number; warnings: { stage: string; ref: string; message: string }[] }>(`/users/${userId}/subscription-preview`)
};
