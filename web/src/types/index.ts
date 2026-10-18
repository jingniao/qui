// Common status types
export type StatusType = 'online' | 'offline' | 'warning' | 'error' | 'maintenance' | 'active' | 'disabled' | 'expired' | 'draft' | 'pending' | 'success' | 'failed' | 'rollback';

export type ProtocolType = 'VLESS' | 'Trojan' | 'Shadowsocks' | 'SOCKS5' | 'HTTP' | 'WireGuard' | 'Direct' | 'Block';

export type AdminRole = 'Owner' | 'Viewer';

export interface User {
  id: string;
  username: string;
  displayName: string;
  status: 'active' | 'disabled' | 'expired';
  nodeGroups: string[];
  devicesCount: number;
  maxDevices: number;
  maxConnections: number;
  allowedProtocols: ProtocolType[];
  monthlyTrafficUsed: number; // in bytes
  trafficLimit: number; // in bytes
  todayTraffic: number; // in bytes
  expireAt: string;
  lastOnlineAt: string;
  remark: string;
  createdAt: string;
}

export interface Device {
  id: string;
  userId: string;
  userName: string;
  name: string;
  status: 'online' | 'offline' | 'disabled';
  protocol: ProtocolType;
  credentialStatus: 'active' | 'revoked' | 'expired';
  lastOnlineAt: string;
  recentNode: string;
  monthlyTraffic: number;
  ip: string;
  platform: string;
}

export interface Subscription {
  id: string;
  userId: string;
  userName: string;
  status: 'active' | 'disabled' | 'expired';
  nodeGroups: string[];
  availableNodesCount: number;
  allowedProtocols: ProtocolType[];
  lastFetchedAt: string;
  tokenStatus: 'active' | 'revoked';
  tokenMasked: string;
  tokenFull?: string;
  createdAt: string;
}

export interface Node {
  id: string;
  name: string;
  status: 'online' | 'offline' | 'maintenance';
  publicIp: string;
  nodeGroup: string;
  coreType: 'Xray' | 'sing-box';
  coreVersion: string;
  agentVersion: string;
  cpuUsage: number; // 0 - 100
  memUsage: number; // 0 - 100
  diskUsage: number;
  activeConnections: number;
  todayUpload: number;
  todayDownload: number;
  configVersion: string;
  lastHeartbeat: string;
  os: string;
  uptime: string;
  allowSubscription: boolean;
  purpose: 'user_inbound' | 'relay' | 'egress' | 'hybrid';
  latency: number;
  remark?: string;
}

export interface NodeGroup {
  id: string;
  name: string;
  purpose: '用户订阅' | '中转' | '出口' | '混合';
  nodesCount: number;
  onlineNodesCount: number;
  authorizedUsersCount: number;
  subscriptionVisible: boolean;
  status: 'active' | 'disabled';
  nodes: string[]; // node IDs
  remark?: string;
  usedInChains?: string[];
}

export interface Inbound {
  id: string;
  name: string;
  nodeId: string;
  nodeName: string;
  protocol: ProtocolType;
  listenAddress: string;
  port: number;
  usersCount: number;
  subscriptionVisible: boolean;
  status: 'active' | 'disabled';
  defaultOutbound: string;
  // Advanced protocol fields
  transport?: 'tcp' | 'ws' | 'grpc' | 'h2' | 'quic';
  tls?: 'none' | 'tls' | 'reality';
  flow?: string;
  alpn?: string[];
  enableUdp: boolean;
}

export interface Outbound {
  id: string;
  name: string;
  nodeId: string;
  nodeName: string;
  type: ProtocolType | 'Direct' | 'Block' | 'RemoteNode';
  protocol: ProtocolType;
  targetNodeId?: string;
  targetNodeName?: string;
  address: string;
  port: number;
  supportTcp: boolean;
  supportUdp: boolean;
  healthStatus: 'healthy' | 'warning' | 'unhealthy';
  latency: number;
  status: 'active' | 'disabled';
  failoverPolicy?: 'fallback' | 'block';
  backupOutboundId?: string;
}

export interface OutboundGroup {
  id: string;
  name: string;
  strategy: '主备' | '轮询' | '延迟优先';
  membersCount: number;
  availableMembersCount: number;
  usedByRulesCount: number;
  status: 'active' | 'disabled';
  members: {
    outboundId: string;
    name: string;
    weight: number;
    priority: number;
    status: 'healthy' | 'unhealthy';
    latency: number;
  }[];
  healthCheckInterval: number; // in seconds
  healthCheckUrl: string;
}

export interface ChainHop {
  nodeId: string;
  nodeName: string;
  protocol: ProtocolType;
  inboundId?: string;
  port: number;
  latency: number;
}

export interface ProxyChain {
  id: string;
  name: string;
  hops: ChainHop[];
  hopsCount: number;
  protocolPath: string; // e.g., 'Trojan → WireGuard'
  healthStatus: 'healthy' | 'warning' | 'unhealthy';
  usedByRulesCount: number;
  status: 'active' | 'disabled';
  finalEgress: string;
  failoverPolicy: 'fallback_direct' | 'block' | 'backup_chain';
  remark?: string;
}

export interface RoutingRule {
  id: string;
  priority: number;
  name: string;
  scope: {
    users: 'all' | string[];
    devices: 'all' | string[];
    inboundNodes: 'all' | string[];
    nodeGroups: 'all' | string[];
  };
  conditions: {
    domains?: string[];
    domainSuffixes?: string[];
    serviceRules?: string[];
    ipCidr?: string[];
    ports?: string[];
    network?: 'all' | 'tcp' | 'udp';
    timeRange?: string;
  };
  action: 'Direct' | 'Block' | 'Outbound' | 'OutboundGroup' | 'ProxyChain';
  targetId?: string;
  targetName?: string;
  failoverPolicy: 'Block' | 'Direct' | 'BackupOutbound' | 'BackupOutboundGroup' | 'BackupProxyChain';
  backupTargetId?: string;
  logging: {
    logDomain: boolean;
    logTraffic: boolean;
    instantAlertOnBlock: boolean;
  };
  status: 'active' | 'disabled';
  remark?: string;
}

export interface ServiceRule {
  id: string;
  name: string;
  type: '内置' | '自定义';
  domainCount: number;
  ipRulesCount: number;
  version: string;
  updatedAt: string;
  usedByRulesCount: number;
  domains: string[];
  domainSuffixes: string[];
  ipCidr: string[];
  sourceDescription: string;
}

export interface WebsiteControlRule {
  id: string;
  name: string;
  category: '禁止访问' | '允许访问' | '指定出口' | '自定义分类';
  scope: string; // '所有用户' or specific
  targetDomain: string;
  action: 'Direct' | 'Block' | 'Proxy';
  assignedEgress?: string;
  status: 'active' | 'disabled';
  updatedAt: string;
  timeRange?: string;
  logRecord: boolean;
  remark?: string;
}

export interface AccessRecord {
  id: string;
  timestamp: string;
  userId: string;
  userName: string;
  deviceId?: string;
  inboundNode: string;
  inboundProtocol: ProtocolType;
  domain: string;
  targetIp: string;
  targetPort: number;
  matchedRule: string;
  action: 'Allow' | 'Block';
  outboundProtocol: ProtocolType;
  proxyChain?: string;
  finalEgress: string;
  connections: number;
  uploadBytes: number;
  downloadBytes: number;
}

export interface AdminUser {
  id: string;
  username: string;
  displayName?: string;
  role: AdminRole;
  status: 'active' | 'disabled';
  lastLoginAt: string;
  loginIp: string;
  createdAt: string;
}

export interface AgentInfo {
  id: string;
  nodeName: string;
  version: string;
  status: 'online' | 'offline';
  latestVersion: string;
  lastHeartbeat: string;
  updateStatus: 'idle' | 'updating' | 'up-to-date' | 'failed';
  ip: string;
}

export interface CoreInfo {
  id: string;
  nodeName: string;
  coreType: 'Xray' | 'sing-box';
  currentVersion: string;
  runningStatus: 'running' | 'stopped' | 'reloading';
  availableVersion: string;
  protocols: ProtocolType[];
  locked: boolean;
}

export interface ConfigRevision {
  id: string;
  versionCode: string;
  author: string;
  summary: string;
  affectedNodes: string[];
  status: 'draft' | 'pending' | 'deploying' | 'success' | 'partial' | 'failed' | 'rollback';
  createdAt: string;
  changes: {
    users?: string;
    inbounds?: string;
    outbounds?: string;
    routing?: string;
    chains?: string;
  };
}

export interface BackupItem {
  id: string;
  name: string;
  timestamp: string;
  type: '自动' | '手动';
  fileSize: number; // in bytes
  dbStatus: '正常' | '异常';
  checksumStatus: '通过' | '失败';
  creator: string;
}

export interface OperationLog {
  id: string;
  timestamp: string;
  operator: string;
  action: string;
  target: string;
  result: '成功' | '失败';
  ip: string;
  beforeSummary?: string;
  afterSummary?: string;
  errorReason?: string;
}
