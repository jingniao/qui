import type {
  User, Device, Subscription, Node, NodeGroup, Inbound, Outbound,
  OutboundGroup, ProxyChain, RoutingRule, ServiceRule, WebsiteControlRule,
  AccessRecord, AdminUser, AgentInfo, CoreInfo, ConfigRevision, BackupItem, OperationLog
} from '../types';

export const mockUsers: User[] = [
  {
    id: 'u-1',
    username: 'alex_vip',
    displayName: 'Alex Zhang',
    status: 'active',
    nodeGroups: ['香港节点组', '日本节点组', '美国高速组'],
    devicesCount: 3,
    maxDevices: 5,
    maxConnections: 16,
    allowedProtocols: ['VLESS', 'Trojan', 'Shadowsocks', 'WireGuard'],
    monthlyTrafficUsed: 148 * 1024 * 1024 * 1024, // 148 GB
    trafficLimit: 500 * 1024 * 1024 * 1024, // 500 GB
    todayTraffic: 4.8 * 1024 * 1024 * 1024,
    expireAt: '2026-12-31 23:59:59',
    lastOnlineAt: '2026-10-01 00:52:10',
    remark: 'VIP企业客户-研发团队',
    createdAt: '2025-01-10 14:20:00'
  },
  {
    id: 'u-2',
    username: 'chen_ops',
    displayName: 'Chen Liu',
    status: 'active',
    nodeGroups: ['香港节点组', '日本节点组'],
    devicesCount: 2,
    maxDevices: 3,
    maxConnections: 8,
    allowedProtocols: ['VLESS', 'Trojan'],
    monthlyTrafficUsed: 42 * 1024 * 1024 * 1024,
    trafficLimit: 200 * 1024 * 1024 * 1024,
    todayTraffic: 1.2 * 1024 * 1024 * 1024,
    expireAt: '2026-11-15 00:00:00',
    lastOnlineAt: '2026-10-01 00:45:00',
    remark: '运维部主力',
    createdAt: '2025-03-01 09:00:00'
  },
  {
    id: 'u-3',
    username: 'sarah_design',
    displayName: 'Sarah Wang',
    status: 'active',
    nodeGroups: ['美国高速组'],
    devicesCount: 1,
    maxDevices: 2,
    maxConnections: 4,
    allowedProtocols: ['Trojan'],
    monthlyTrafficUsed: 185 * 1024 * 1024 * 1024,
    trafficLimit: 200 * 1024 * 1024 * 1024,
    todayTraffic: 12.5 * 1024 * 1024 * 1024,
    expireAt: '2026-10-10 18:00:00',
    lastOnlineAt: '2026-09-30 23:10:00',
    remark: 'UI设计部素材下载',
    createdAt: '2025-04-12 11:30:00'
  },
  {
    id: 'u-4',
    username: 'guest_test',
    displayName: '测试访客',
    status: 'expired',
    nodeGroups: ['香港节点组'],
    devicesCount: 0,
    maxDevices: 1,
    maxConnections: 2,
    allowedProtocols: ['Shadowsocks'],
    monthlyTrafficUsed: 9.8 * 1024 * 1024 * 1024,
    trafficLimit: 10 * 1024 * 1024 * 1024,
    todayTraffic: 0,
    expireAt: '2026-09-28 00:00:00',
    lastOnlineAt: '2026-09-27 15:40:00',
    remark: '体验账户已过期',
    createdAt: '2026-09-21 10:00:00'
  },
  {
    id: 'u-5',
    username: 'bot_crawler',
    displayName: '自动化采集爬虫',
    status: 'disabled',
    nodeGroups: ['美国高速组'],
    devicesCount: 0,
    maxDevices: 1,
    maxConnections: 32,
    allowedProtocols: ['SOCKS5', 'HTTP'],
    monthlyTrafficUsed: 890 * 1024 * 1024 * 1024,
    trafficLimit: 1000 * 1024 * 1024 * 1024,
    todayTraffic: 0,
    expireAt: '2027-01-01 00:00:00',
    lastOnlineAt: '2026-09-29 11:22:00',
    remark: '疑似异常请求频发已暂时禁用',
    createdAt: '2025-06-01 08:30:00'
  }
];

export const mockDevices: Device[] = [
  {
    id: 'd-1',
    userId: 'u-1',
    userName: 'alex_vip',
    name: 'MacBook Pro M3 Max',
    status: 'online',
    protocol: 'VLESS',
    credentialStatus: 'active',
    lastOnlineAt: '刚刚',
    recentNode: 'HK-Edge-01 (香港入口)',
    monthlyTraffic: 86 * 1024 * 1024 * 1024,
    ip: '116.48.21.90',
    platform: 'macOS 15.0'
  },
  {
    id: 'd-2',
    userId: 'u-1',
    userName: 'alex_vip',
    name: 'iPhone 16 Pro',
    status: 'online',
    protocol: 'Trojan',
    credentialStatus: 'active',
    lastOnlineAt: '5分钟前',
    recentNode: 'HK-Edge-01 (香港入口)',
    monthlyTraffic: 32 * 1024 * 1024 * 1024,
    ip: '223.16.89.14',
    platform: 'iOS 18.1'
  },
  {
    id: 'd-3',
    userId: 'u-1',
    userName: 'alex_vip',
    name: 'Home Office ThinkPad',
    status: 'offline',
    protocol: 'WireGuard',
    credentialStatus: 'active',
    lastOnlineAt: '2天前',
    recentNode: 'JP-Tokyo-02 (日本中继)',
    monthlyTraffic: 30 * 1024 * 1024 * 1024,
    ip: '183.14.92.5',
    platform: 'Windows 11'
  },
  {
    id: 'd-4',
    userId: 'u-2',
    userName: 'chen_ops',
    name: 'Ops-Linux-DevBox',
    status: 'online',
    protocol: 'VLESS',
    credentialStatus: 'active',
    lastOnlineAt: '刚刚',
    recentNode: 'HK-Edge-02 (香港入口备用)',
    monthlyTraffic: 38 * 1024 * 1024 * 1024,
    ip: '14.153.64.12',
    platform: 'Ubuntu 24.04 LTS'
  },
  {
    id: 'd-5',
    userId: 'u-3',
    userName: 'sarah_design',
    name: 'Studio iMac 27',
    status: 'online',
    protocol: 'Trojan',
    credentialStatus: 'active',
    lastOnlineAt: '12分钟前',
    recentNode: 'US-LA-Direct-01 (洛杉矶直连)',
    monthlyTraffic: 185 * 1024 * 1024 * 1024,
    ip: '58.24.112.44',
    platform: 'macOS 14.6'
  }
];

export const mockSubscriptions: Subscription[] = [
  {
    id: 'sub-1',
    userId: 'u-1',
    userName: 'alex_vip',
    status: 'active',
    nodeGroups: ['香港节点组', '日本节点组', '美国高速组'],
    availableNodesCount: 8,
    allowedProtocols: ['VLESS', 'Trojan', 'Shadowsocks', 'WireGuard'],
    lastFetchedAt: '2026-10-01 00:30:15',
    tokenStatus: 'active',
    tokenMasked: 'sub_live_9f8a********************4b21',
    tokenFull: 'sub_live_9f8a374b8109efccba44883192004b21',
    createdAt: '2025-01-10 14:20:00'
  },
  {
    id: 'sub-2',
    userId: 'u-2',
    userName: 'chen_ops',
    status: 'active',
    nodeGroups: ['香港节点组', '日本节点组'],
    availableNodesCount: 5,
    allowedProtocols: ['VLESS', 'Trojan'],
    lastFetchedAt: '2026-09-30 22:15:00',
    tokenStatus: 'active',
    tokenMasked: 'sub_live_1a2b********************9c8d',
    tokenFull: 'sub_live_1a2b3c4d5e6f7a8b9c0d1e2f3a4b9c8d',
    createdAt: '2025-03-01 09:00:00'
  },
  {
    id: 'sub-3',
    userId: 'u-3',
    userName: 'sarah_design',
    status: 'active',
    nodeGroups: ['美国高速组'],
    availableNodesCount: 3,
    allowedProtocols: ['Trojan'],
    lastFetchedAt: '2026-09-30 18:40:22',
    tokenStatus: 'active',
    tokenMasked: 'sub_live_77e1********************fa33',
    tokenFull: 'sub_live_77e129aa4419cb1144002891bbfa33',
    createdAt: '2025-04-12 11:30:00'
  },
  {
    id: 'sub-4',
    userId: 'u-4',
    userName: 'guest_test',
    status: 'expired',
    nodeGroups: ['香港节点组'],
    availableNodesCount: 0,
    allowedProtocols: ['Shadowsocks'],
    lastFetchedAt: '2026-09-27 15:40:00',
    tokenStatus: 'revoked',
    tokenMasked: 'sub_revk_001a********************bb44',
    createdAt: '2026-09-21 10:00:00'
  }
];

export const mockNodes: Node[] = [
  {
    id: 'node-hk-01',
    name: 'HK-Edge-01 (香港入口01)',
    status: 'online',
    publicIp: '103.149.88.22',
    nodeGroup: '香港节点组',
    coreType: 'Xray',
    coreVersion: 'v1.8.16',
    agentVersion: 'v2.4.1',
    cpuUsage: 24,
    memUsage: 48,
    diskUsage: 32,
    activeConnections: 312,
    todayUpload: 18.4 * 1024 * 1024 * 1024,
    todayDownload: 92.6 * 1024 * 1024 * 1024,
    configVersion: 'cfg-rev-20261001-01',
    lastHeartbeat: '刚刚 (3秒前)',
    os: 'Debian 12 x86_64',
    uptime: '45天 18小时',
    allowSubscription: true,
    purpose: 'user_inbound',
    latency: 18,
    remark: 'BGP优质多线接入节点'
  },
  {
    id: 'node-hk-02',
    name: 'HK-Edge-02 (香港入口02备用)',
    status: 'online',
    publicIp: '103.149.88.23',
    nodeGroup: '香港节点组',
    coreType: 'Xray',
    coreVersion: 'v1.8.16',
    agentVersion: 'v2.4.1',
    cpuUsage: 12,
    memUsage: 35,
    diskUsage: 28,
    activeConnections: 98,
    todayUpload: 5.1 * 1024 * 1024 * 1024,
    todayDownload: 28.3 * 1024 * 1024 * 1024,
    configVersion: 'cfg-rev-20261001-01',
    lastHeartbeat: '刚刚 (5秒前)',
    os: 'Debian 12 x86_64',
    uptime: '45天 18小时',
    allowSubscription: true,
    purpose: 'user_inbound',
    latency: 22,
    remark: '自动热备入口节点'
  },
  {
    id: 'node-jp-01',
    name: 'JP-Tokyo-01 (东京中转核心)',
    status: 'online',
    publicIp: '139.162.77.104',
    nodeGroup: '日本节点组',
    coreType: 'Xray',
    coreVersion: 'v1.8.16',
    agentVersion: 'v2.4.1',
    cpuUsage: 45,
    memUsage: 62,
    diskUsage: 40,
    activeConnections: 540,
    todayUpload: 34.2 * 1024 * 1024 * 1024,
    todayDownload: 180.5 * 1024 * 1024 * 1024,
    configVersion: 'cfg-rev-20261001-01',
    lastHeartbeat: '刚刚 (2秒前)',
    os: 'Ubuntu 24.04 LTS',
    uptime: '28天 04小时',
    allowSubscription: false,
    purpose: 'relay',
    latency: 48,
    remark: '高速专线中转跳点'
  },
  {
    id: 'node-us-01',
    name: 'US-LA-Direct-01 (洛杉矶直连出口)',
    status: 'online',
    publicIp: '204.12.201.88',
    nodeGroup: '美国高速组',
    coreType: 'sing-box',
    coreVersion: 'v1.9.4',
    agentVersion: 'v2.4.1',
    cpuUsage: 19,
    memUsage: 39,
    diskUsage: 25,
    activeConnections: 185,
    todayUpload: 12.8 * 1024 * 1024 * 1024,
    todayDownload: 85.0 * 1024 * 1024 * 1024,
    configVersion: 'cfg-rev-20261001-01',
    lastHeartbeat: '刚刚 (6秒前)',
    os: 'AlmaLinux 9',
    uptime: '12天 11小时',
    allowSubscription: true,
    purpose: 'egress',
    latency: 142,
    remark: 'CN2 GIA原生出口IP'
  },
  {
    id: 'node-us-02',
    name: 'US-SJC-Egress-02 (硅谷原生出口)',
    status: 'maintenance',
    publicIp: '198.51.100.45',
    nodeGroup: '美国高速组',
    coreType: 'Xray',
    coreVersion: 'v1.8.15',
    agentVersion: 'v2.4.0',
    cpuUsage: 4,
    memUsage: 20,
    diskUsage: 18,
    activeConnections: 0,
    todayUpload: 0.2 * 1024 * 1024 * 1024,
    todayDownload: 1.1 * 1024 * 1024 * 1024,
    configVersion: 'cfg-rev-20260928-03',
    lastHeartbeat: '10秒前',
    os: 'Debian 11',
    uptime: '89天 01小时',
    allowSubscription: false,
    purpose: 'egress',
    latency: 155,
    remark: '正在进行系统内核升级维护'
  },
  {
    id: 'node-sg-01',
    name: 'SG-Direct-01 (新加坡边缘)',
    status: 'offline',
    publicIp: '128.199.200.12',
    nodeGroup: '混合节点组',
    coreType: 'Xray',
    coreVersion: 'v1.8.14',
    agentVersion: 'v2.3.9',
    cpuUsage: 0,
    memUsage: 0,
    diskUsage: 44,
    activeConnections: 0,
    todayUpload: 0,
    todayDownload: 0,
    configVersion: 'cfg-rev-20260920-01',
    lastHeartbeat: '离线已超过 2小时',
    os: 'Ubuntu 22.04',
    uptime: '0',
    allowSubscription: true,
    purpose: 'hybrid',
    latency: 999,
    remark: '上游机房故障排查中'
  }
];

export const mockNodeGroups: NodeGroup[] = [
  {
    id: 'ng-hk',
    name: '香港节点组',
    purpose: '用户订阅',
    nodesCount: 2,
    onlineNodesCount: 2,
    authorizedUsersCount: 14,
    subscriptionVisible: true,
    status: 'active',
    nodes: ['node-hk-01', 'node-hk-02'],
    remark: '面向亚太用户的高速低延迟入口集群',
    usedInChains: ['chain-hk-jp-us']
  },
  {
    id: 'ng-jp',
    name: '日本节点组',
    purpose: '中转',
    nodesCount: 1,
    onlineNodesCount: 1,
    authorizedUsersCount: 10,
    subscriptionVisible: false,
    status: 'active',
    nodes: ['node-jp-01'],
    remark: '专门用于跨太平洋流量专线中转',
    usedInChains: ['chain-hk-jp-us', 'chain-direct-jp']
  },
  {
    id: 'ng-us',
    name: '美国高速组',
    purpose: '出口',
    nodesCount: 2,
    onlineNodesCount: 1,
    authorizedUsersCount: 8,
    subscriptionVisible: true,
    status: 'active',
    nodes: ['node-us-01', 'node-us-02'],
    remark: '原生流媒体解锁及AI服务出口',
    usedInChains: ['chain-hk-jp-us']
  },
  {
    id: 'ng-hybrid',
    name: '混合节点组',
    purpose: '混合',
    nodesCount: 1,
    onlineNodesCount: 0,
    authorizedUsersCount: 2,
    subscriptionVisible: true,
    status: 'disabled',
    nodes: ['node-sg-01'],
    remark: '测试中的混合用途试验组'
  }
];

export const mockInbounds: Inbound[] = [
  {
    id: 'in-vless-reality-443',
    name: 'HK-VLESS-Reality-443',
    nodeId: 'node-hk-01',
    nodeName: 'HK-Edge-01 (香港入口01)',
    protocol: 'VLESS',
    listenAddress: '0.0.0.0',
    port: 443,
    usersCount: 12,
    subscriptionVisible: true,
    status: 'active',
    defaultOutbound: 'direct-out',
    transport: 'tcp',
    tls: 'reality',
    flow: 'xtls-rprx-vision',
    enableUdp: true
  },
  {
    id: 'in-trojan-8443',
    name: 'HK-Trojan-TLS-8443',
    nodeId: 'node-hk-01',
    nodeName: 'HK-Edge-01 (香港入口01)',
    protocol: 'Trojan',
    listenAddress: '0.0.0.0',
    port: 8443,
    usersCount: 10,
    subscriptionVisible: true,
    status: 'active',
    defaultOutbound: 'relay-to-jp',
    transport: 'tcp',
    tls: 'tls',
    enableUdp: true
  },
  {
    id: 'in-ss-2022',
    name: 'HK-SS2022-Blake3',
    nodeId: 'node-hk-02',
    nodeName: 'HK-Edge-02 (香港入口02备用)',
    protocol: 'Shadowsocks',
    listenAddress: '0.0.0.0',
    port: 52022,
    usersCount: 6,
    subscriptionVisible: true,
    status: 'active',
    defaultOutbound: 'direct-out',
    enableUdp: true
  },
  {
    id: 'in-wg-51820',
    name: 'JP-Relay-WireGuard-51820',
    nodeId: 'node-jp-01',
    nodeName: 'JP-Tokyo-01 (东京中转核心)',
    protocol: 'WireGuard',
    listenAddress: '0.0.0.0',
    port: 51820,
    usersCount: 4,
    subscriptionVisible: false,
    status: 'active',
    defaultOutbound: 'egress-to-us',
    enableUdp: true
  }
];

export const mockOutbounds: Outbound[] = [
  {
    id: 'out-direct',
    name: 'Direct-DirectOut',
    nodeId: 'node-hk-01',
    nodeName: 'HK-Edge-01 (香港入口01)',
    type: 'Direct',
    protocol: 'Direct',
    address: '0.0.0.0',
    port: 0,
    supportTcp: true,
    supportUdp: true,
    healthStatus: 'healthy',
    latency: 2,
    status: 'active'
  },
  {
    id: 'out-block',
    name: 'Block-BlackHole',
    nodeId: 'node-hk-01',
    nodeName: 'HK-Edge-01 (香港入口01)',
    type: 'Block',
    protocol: 'Block',
    address: '0.0.0.0',
    port: 0,
    supportTcp: true,
    supportUdp: true,
    healthStatus: 'healthy',
    latency: 0,
    status: 'active'
  },
  {
    id: 'out-relay-jp',
    name: 'Relay-To-Tokyo-VLESS',
    nodeId: 'node-hk-01',
    nodeName: 'HK-Edge-01 (香港入口01)',
    type: 'RemoteNode',
    protocol: 'VLESS',
    targetNodeId: 'node-jp-01',
    targetNodeName: 'JP-Tokyo-01 (东京中转核心)',
    address: '139.162.77.104',
    port: 443,
    supportTcp: true,
    supportUdp: true,
    healthStatus: 'healthy',
    latency: 32,
    status: 'active',
    failoverPolicy: 'fallback'
  },
  {
    id: 'out-egress-la',
    name: 'Egress-To-LA-WireGuard',
    nodeId: 'node-jp-01',
    nodeName: 'JP-Tokyo-01 (东京中转核心)',
    type: 'WireGuard',
    protocol: 'WireGuard',
    targetNodeId: 'node-us-01',
    targetNodeName: 'US-LA-Direct-01 (洛杉矶直连出口)',
    address: '204.12.201.88',
    port: 51820,
    supportTcp: true,
    supportUdp: true,
    healthStatus: 'healthy',
    latency: 98,
    status: 'active',
    failoverPolicy: 'fallback'
  },
  {
    id: 'out-egress-sjc',
    name: 'Egress-To-SJC-Trojan',
    nodeId: 'node-jp-01',
    nodeName: 'JP-Tokyo-01 (东京中转核心)',
    type: 'Trojan',
    protocol: 'Trojan',
    targetNodeId: 'node-us-02',
    targetNodeName: 'US-SJC-Egress-02 (硅谷原生出口)',
    address: '198.51.100.45',
    port: 8443,
    supportTcp: true,
    supportUdp: true,
    healthStatus: 'unhealthy',
    latency: 999,
    status: 'disabled'
  }
];

export const mockOutboundGroups: OutboundGroup[] = [
  {
    id: 'og-us-egress',
    name: '美西多线出口组',
    strategy: '主备',
    membersCount: 2,
    availableMembersCount: 1,
    usedByRulesCount: 4,
    status: 'active',
    healthCheckInterval: 30,
    healthCheckUrl: 'https://cp.cloudflare.com/generate_204',
    members: [
      {
        outboundId: 'out-egress-la',
        name: 'Egress-To-LA-WireGuard (洛杉矶CN2)',
        weight: 100,
        priority: 1,
        status: 'healthy',
        latency: 142
      },
      {
        outboundId: 'out-egress-sjc',
        name: 'Egress-To-SJC-Trojan (硅谷备用)',
        weight: 50,
        priority: 2,
        status: 'unhealthy',
        latency: 999
      }
    ]
  },
  {
    id: 'og-hk-inbounds',
    name: '香港双活均衡组',
    strategy: '延迟优先',
    membersCount: 2,
    availableMembersCount: 2,
    usedByRulesCount: 2,
    status: 'active',
    healthCheckInterval: 15,
    healthCheckUrl: 'https://www.gstatic.com/generate_204',
    members: [
      {
        outboundId: 'out-relay-jp',
        name: 'HK-01 直连中继',
        weight: 100,
        priority: 1,
        status: 'healthy',
        latency: 18
      }
    ]
  }
];

export const mockProxyChains: ProxyChain[] = [
  {
    id: 'chain-hk-jp-us',
    name: '极速游戏/AI双跳专线 (HK → JP → US)',
    hops: [
      { nodeId: 'node-hk-01', nodeName: 'HK-Edge-01 (香港入口)', protocol: 'VLESS', port: 443, latency: 18 },
      { nodeId: 'node-jp-01', nodeName: 'JP-Tokyo-01 (东京中继)', protocol: 'Trojan', port: 8443, latency: 45 },
      { nodeId: 'node-us-01', nodeName: 'US-LA-Direct-01 (美西出口)', protocol: 'WireGuard', port: 51820, latency: 138 }
    ],
    hopsCount: 3,
    protocolPath: 'VLESS → Trojan → WireGuard',
    healthStatus: 'healthy',
    usedByRulesCount: 6,
    status: 'active',
    finalEgress: 'US-LA-Direct-01 (洛杉矶直连出口)',
    failoverPolicy: 'fallback_direct',
    remark: '针对 OpenAI / Claude / Netflix 的专用低延迟多跳'
  },
  {
    id: 'chain-direct-jp',
    name: '东亚低延迟单跳 (HK → JP)',
    hops: [
      { nodeId: 'node-hk-01', nodeName: 'HK-Edge-01 (香港入口)', protocol: 'Trojan', port: 8443, latency: 18 },
      { nodeId: 'node-jp-01', nodeName: 'JP-Tokyo-01 (日本出口)', protocol: 'SOCKS5', port: 10808, latency: 42 }
    ],
    hopsCount: 2,
    protocolPath: 'Trojan → SOCKS5',
    healthStatus: 'healthy',
    usedByRulesCount: 3,
    status: 'active',
    finalEgress: 'JP-Tokyo-01 (日本出口)',
    failoverPolicy: 'block',
    remark: '针对日本游戏和学术站点优化'
  },
  {
    id: 'chain-failover-backup',
    name: '备用容灾链路 (HK → US)',
    hops: [
      { nodeId: 'node-hk-02', nodeName: 'HK-Edge-02 (香港备用)', protocol: 'Shadowsocks', port: 52022, latency: 24 },
      { nodeId: 'node-us-02', nodeName: 'US-SJC-Egress-02 (硅谷出口)', protocol: 'Trojan', port: 8443, latency: 160 }
    ],
    hopsCount: 2,
    protocolPath: 'Shadowsocks → Trojan',
    healthStatus: 'warning',
    usedByRulesCount: 1,
    status: 'disabled',
    finalEgress: 'US-SJC-Egress-02 (节点维护中)',
    failoverPolicy: 'fallback_direct',
    remark: '硅谷节点维护中，链路处于待命状态'
  }
];

export const mockRoutingRules: RoutingRule[] = [
  {
    id: 'rule-openai-ai',
    priority: 10,
    name: 'OpenAI 与生成式 AI 服务分流',
    scope: {
      users: 'all',
      devices: 'all',
      inboundNodes: 'all',
      nodeGroups: 'all'
    },
    conditions: {
      serviceRules: ['OpenAI', 'Claude-Anthropic'],
      domains: ['chatgpt.com', 'oaistatic.com', 'claude.ai'],
      domainSuffixes: ['openai.com', 'anthropic.com'],
      network: 'all'
    },
    action: 'ProxyChain',
    targetId: 'chain-hk-jp-us',
    targetName: '极速游戏/AI双跳专线 (HK → JP → US)',
    failoverPolicy: 'BackupOutboundGroup',
    backupTargetId: 'og-us-egress',
    logging: {
      logDomain: true,
      logTraffic: true,
      instantAlertOnBlock: false
    },
    status: 'active',
    remark: '保障 AI 服务的原生解锁与稳定会话'
  },
  {
    id: 'rule-netflix-streaming',
    priority: 20,
    name: 'Netflix 国际流媒体专属策略',
    scope: {
      users: 'all',
      devices: 'all',
      inboundNodes: 'all',
      nodeGroups: 'all'
    },
    conditions: {
      serviceRules: ['Netflix'],
      domainSuffixes: ['netflix.com', 'nflxvideo.net', 'nflximg.net'],
      network: 'all'
    },
    action: 'ProxyChain',
    targetId: 'chain-hk-jp-us',
    targetName: '极速游戏/AI双跳专线 (HK → JP → US)',
    failoverPolicy: 'Block',
    logging: {
      logDomain: true,
      logTraffic: true,
      instantAlertOnBlock: false
    },
    status: 'active',
    remark: '防止非原生 IP 触发地区封锁'
  },
  {
    id: 'rule-security-block',
    priority: 30,
    name: '全网恶意与挖矿域名阻断',
    scope: {
      users: 'all',
      devices: 'all',
      inboundNodes: 'all',
      nodeGroups: 'all'
    },
    conditions: {
      domains: ['coinhive.com', 'pool.supportxmr.com', 'malware-phishing-test.cn'],
      ipCidr: ['198.18.0.0/15']
    },
    action: 'Block',
    failoverPolicy: 'Block',
    logging: {
      logDomain: true,
      logTraffic: false,
      instantAlertOnBlock: true
    },
    status: 'active',
    remark: '安全防护策略拦截恶意通信'
  },
  {
    id: 'rule-cn-direct',
    priority: 100,
    name: '国内直连与局域网直通',
    scope: {
      users: 'all',
      devices: 'all',
      inboundNodes: 'all',
      nodeGroups: 'all'
    },
    conditions: {
      domainSuffixes: ['cn', 'qq.com', 'baidu.com', 'aliyun.com', 'taobao.com', 'bilibili.com'],
      ipCidr: ['10.0.0.0/8', '172.16.0.0/12', '192.168.0.0/16', '127.0.0.0/8']
    },
    action: 'Direct',
    failoverPolicy: 'Direct',
    logging: {
      logDomain: false,
      logTraffic: true,
      instantAlertOnBlock: false
    },
    status: 'active',
    remark: '本地私网和合规大陆站点走直接出站 Direct'
  }
];

export const mockServiceRules: ServiceRule[] = [
  {
    id: 'svc-netflix',
    name: 'Netflix',
    type: '内置',
    domainCount: 24,
    ipRulesCount: 6,
    version: '2026.09.28',
    updatedAt: '2026-09-28 14:00:00',
    usedByRulesCount: 3,
    domains: ['netflix.com', 'netflix.net', 'nflximg.com', 'nflxvideo.net', 'nflxext.com'],
    domainSuffixes: ['netflix.com', 'nflxvideo.net', 'nflximg.net'],
    ipCidr: ['23.246.0.0/18', '37.77.184.0/21', '45.57.0.0/17'],
    sourceDescription: '官方 GeoIP/Domain 规则库自动同步'
  },
  {
    id: 'svc-openai',
    name: 'OpenAI',
    type: '内置',
    domainCount: 18,
    ipRulesCount: 2,
    version: '2026.09.25',
    updatedAt: '2026-09-25 10:20:00',
    usedByRulesCount: 4,
    domains: ['openai.com', 'chatgpt.com', 'oaistatic.com', 'oaiusercontent.com', 'sentry.io'],
    domainSuffixes: ['openai.com', 'chatgpt.com'],
    ipCidr: ['104.18.0.0/16', '172.64.0.0/16'],
    sourceDescription: 'ChatGPT 网页与 API 全域白名单集合'
  },
  {
    id: 'svc-disney',
    name: 'Disney+',
    type: '内置',
    domainCount: 16,
    ipRulesCount: 0,
    version: '2026.09.10',
    updatedAt: '2026-09-10 11:00:00',
    usedByRulesCount: 1,
    domains: ['disneyplus.com', 'disney-plus.net', 'dssott.com'],
    domainSuffixes: ['disneyplus.com', 'dssott.com'],
    ipCidr: [],
    sourceDescription: 'Disney+ 流媒体集合'
  },
  {
    id: 'svc-youtube',
    name: 'YouTube',
    type: '内置',
    domainCount: 32,
    ipRulesCount: 12,
    version: '2026.09.30',
    updatedAt: '2026-09-30 08:00:00',
    usedByRulesCount: 2,
    domains: ['youtube.com', 'googlevideo.com', 'ytimg.com', 'youtu.be'],
    domainSuffixes: ['youtube.com', 'googlevideo.com'],
    ipCidr: ['172.217.0.0/16'],
    sourceDescription: 'YouTube 视频与静态 CDN 规则'
  },
  {
    id: 'svc-github',
    name: 'GitHub',
    type: '内置',
    domainCount: 14,
    ipRulesCount: 4,
    version: '2026.09.18',
    updatedAt: '2026-09-18 16:30:00',
    usedByRulesCount: 2,
    domains: ['github.com', 'githubusercontent.com', 'githubassets.com'],
    domainSuffixes: ['github.com', 'githubusercontent.com'],
    ipCidr: ['140.82.112.0/20'],
    sourceDescription: '研发代码托管平台加速规则'
  },
  {
    id: 'svc-custom-corp',
    name: '企业内部 SaaS 集合',
    type: '自定义',
    domainCount: 8,
    ipRulesCount: 1,
    version: '1.2.0',
    updatedAt: '2026-09-29 17:15:00',
    usedByRulesCount: 1,
    domains: ['corp-erp.internal.tech', 'jira.internal.tech', 'wiki.internal.tech'],
    domainSuffixes: ['internal.tech'],
    ipCidr: ['192.168.100.0/24'],
    sourceDescription: '管理员根据企业内部系统手动录入'
  }
];

export const mockWebsiteControls: WebsiteControlRule[] = [
  {
    id: 'web-1',
    name: '禁止未成年/涉赌站点',
    category: '禁止访问',
    scope: '所有用户',
    targetDomain: 'bet365.com, poker*.com, *.casino',
    action: 'Block',
    status: 'active',
    updatedAt: '2026-09-20 10:00:00',
    logRecord: true,
    remark: '合规审计禁止名单'
  },
  {
    id: 'web-2',
    name: '谷歌学术无阻通道',
    category: '允许访问',
    scope: '所有用户',
    targetDomain: 'scholar.google.com, arxiv.org',
    action: 'Proxy',
    assignedEgress: 'HK-Edge-01 (香港入口01)',
    status: 'active',
    updatedAt: '2026-09-22 14:30:00',
    logRecord: false,
    remark: '学术科研极速放行'
  },
  {
    id: 'web-3',
    name: '企业指定美西出口',
    category: '指定出口',
    scope: '研发团队 (alex_vip)',
    targetDomain: 'huggingface.co, wandb.ai',
    action: 'Proxy',
    assignedEgress: '极速游戏/AI双跳专线 (HK → JP → US)',
    status: 'active',
    updatedAt: '2026-09-25 16:00:00',
    logRecord: true,
    remark: '大模型训练与平台访问'
  }
];

export const mockAccessRecords: AccessRecord[] = [
  {
    id: 'rec-1',
    timestamp: '2026-10-01 00:52:18',
    userId: 'u-1',
    userName: 'alex_vip',
    deviceId: 'd-1',
    inboundNode: 'HK-Edge-01',
    inboundProtocol: 'VLESS',
    domain: 'api.openai.com',
    targetIp: '104.18.32.7',
    targetPort: 443,
    matchedRule: 'OpenAI 与生成式 AI 服务分流',
    action: 'Allow',
    outboundProtocol: 'WireGuard',
    proxyChain: '极速游戏/AI双跳专线 (HK → JP → US)',
    finalEgress: 'US-LA-Direct-01',
    connections: 4,
    uploadBytes: 15420,
    downloadBytes: 184520
  },
  {
    id: 'rec-2',
    timestamp: '2026-10-01 00:51:40',
    userId: 'u-1',
    userName: 'alex_vip',
    deviceId: 'd-1',
    inboundNode: 'HK-Edge-01',
    inboundProtocol: 'VLESS',
    domain: 'chatgpt.com',
    targetIp: '172.64.155.20',
    targetPort: 443,
    matchedRule: 'OpenAI 与生成式 AI 服务分流',
    action: 'Allow',
    outboundProtocol: 'WireGuard',
    proxyChain: '极速游戏/AI双跳专线 (HK → JP → US)',
    finalEgress: 'US-LA-Direct-01',
    connections: 8,
    uploadBytes: 42100,
    downloadBytes: 894000
  },
  {
    id: 'rec-3',
    timestamp: '2026-10-01 00:50:05',
    userId: 'u-3',
    userName: 'sarah_design',
    deviceId: 'd-5',
    inboundNode: 'US-LA-Direct-01',
    inboundProtocol: 'Trojan',
    domain: 'assets.figma.com',
    targetIp: '151.101.65.140',
    targetPort: 443,
    matchedRule: '默认国外放行规则',
    action: 'Allow',
    outboundProtocol: 'Direct',
    finalEgress: 'US-LA-Direct-01',
    connections: 12,
    uploadBytes: 98000,
    downloadBytes: 14205000
  },
  {
    id: 'rec-4',
    timestamp: '2026-10-01 00:48:33',
    userId: 'u-2',
    userName: 'chen_ops',
    deviceId: 'd-4',
    inboundNode: 'HK-Edge-02',
    inboundProtocol: 'VLESS',
    domain: 'pool.supportxmr.com',
    targetIp: '198.18.1.5',
    targetPort: 3333,
    matchedRule: '全网恶意与挖矿域名阻断',
    action: 'Block',
    outboundProtocol: 'Block',
    finalEgress: 'BlackHole',
    connections: 1,
    uploadBytes: 520,
    downloadBytes: 0
  },
  {
    id: 'rec-5',
    timestamp: '2026-10-01 00:45:10',
    userId: 'u-1',
    userName: 'alex_vip',
    deviceId: 'd-2',
    inboundNode: 'HK-Edge-01',
    inboundProtocol: 'Trojan',
    domain: 'www.netflix.com',
    targetIp: '45.57.90.1',
    targetPort: 443,
    matchedRule: 'Netflix 国际流媒体专属策略',
    action: 'Allow',
    outboundProtocol: 'WireGuard',
    proxyChain: '极速游戏/AI双跳专线 (HK → JP → US)',
    finalEgress: 'US-LA-Direct-01',
    connections: 6,
    uploadBytes: 31000,
    downloadBytes: 25400000
  },
  {
    id: 'rec-6',
    timestamp: '2026-10-01 00:42:00',
    userId: 'u-2',
    userName: 'chen_ops',
    deviceId: 'd-4',
    inboundNode: 'HK-Edge-02',
    inboundProtocol: 'VLESS',
    domain: 'github.com',
    targetIp: '140.82.112.4',
    targetPort: 443,
    matchedRule: '研发代码平台加速',
    action: 'Allow',
    outboundProtocol: 'Direct',
    finalEgress: 'HK-Edge-02',
    connections: 3,
    uploadBytes: 12400,
    downloadBytes: 450000
  }
];

export const mockAdmins: AdminUser[] = [
  {
    id: 'admin-owner',
    username: 'root_owner',
    role: 'Owner',
    status: 'active',
    lastLoginAt: '2026-10-01 00:54:00',
    loginIp: '127.0.0.1 (本地控制台)',
    createdAt: '2025-01-01 00:00:00'
  },
  {
    id: 'admin-ops',
    username: 'ops_david',
    role: 'Viewer',
    status: 'active',
    lastLoginAt: '2026-09-30 21:15:32',
    loginIp: '116.23.44.18',
    createdAt: '2025-02-15 10:00:00'
  },
  {
    id: 'admin-auditor',
    username: 'security_auditor',
    role: 'Viewer',
    status: 'active',
    lastLoginAt: '2026-09-29 14:02:11',
    loginIp: '14.153.20.88',
    createdAt: '2025-05-10 16:30:00'
  }
];

export const mockAgents: AgentInfo[] = [
  {
    id: 'ag-hk-01',
    nodeName: 'HK-Edge-01 (香港入口01)',
    version: 'v2.4.1',
    status: 'online',
    latestVersion: 'v2.4.1',
    lastHeartbeat: '3秒前',
    updateStatus: 'up-to-date',
    ip: '103.149.88.22'
  },
  {
    id: 'ag-hk-02',
    nodeName: 'HK-Edge-02 (香港入口02备用)',
    version: 'v2.4.1',
    status: 'online',
    latestVersion: 'v2.4.1',
    lastHeartbeat: '5秒前',
    updateStatus: 'up-to-date',
    ip: '103.149.88.23'
  },
  {
    id: 'ag-jp-01',
    nodeName: 'JP-Tokyo-01 (东京中转核心)',
    version: 'v2.4.1',
    status: 'online',
    latestVersion: 'v2.4.1',
    lastHeartbeat: '2秒前',
    updateStatus: 'up-to-date',
    ip: '139.162.77.104'
  },
  {
    id: 'ag-us-01',
    nodeName: 'US-LA-Direct-01 (洛杉矶直连出口)',
    version: 'v2.4.1',
    status: 'online',
    latestVersion: 'v2.4.1',
    lastHeartbeat: '6秒前',
    updateStatus: 'up-to-date',
    ip: '204.12.201.88'
  },
  {
    id: 'ag-us-02',
    nodeName: 'US-SJC-Egress-02 (硅谷原生出口)',
    version: 'v2.4.0',
    status: 'online',
    latestVersion: 'v2.4.1',
    lastHeartbeat: '10秒前',
    updateStatus: 'idle',
    ip: '198.51.100.45'
  },
  {
    id: 'ag-sg-01',
    nodeName: 'SG-Direct-01 (新加坡边缘)',
    version: 'v2.3.9',
    status: 'offline',
    latestVersion: 'v2.4.1',
    lastHeartbeat: '2小时前',
    updateStatus: 'failed',
    ip: '128.199.200.12'
  }
];

export const mockCores: CoreInfo[] = [
  {
    id: 'core-1',
    nodeName: 'HK-Edge-01',
    coreType: 'Xray',
    currentVersion: 'v1.8.16',
    runningStatus: 'running',
    availableVersion: 'v1.8.16',
    protocols: ['VLESS', 'Trojan', 'Shadowsocks', 'SOCKS5', 'HTTP'],
    locked: false
  },
  {
    id: 'core-2',
    nodeName: 'HK-Edge-02',
    coreType: 'Xray',
    currentVersion: 'v1.8.16',
    runningStatus: 'running',
    availableVersion: 'v1.8.16',
    protocols: ['VLESS', 'Trojan', 'Shadowsocks'],
    locked: false
  },
  {
    id: 'core-3',
    nodeName: 'JP-Tokyo-01',
    coreType: 'Xray',
    currentVersion: 'v1.8.16',
    runningStatus: 'running',
    availableVersion: 'v1.8.16',
    protocols: ['VLESS', 'Trojan', 'WireGuard', 'SOCKS5'],
    locked: true
  },
  {
    id: 'core-4',
    nodeName: 'US-LA-Direct-01',
    coreType: 'sing-box',
    currentVersion: 'v1.9.4',
    runningStatus: 'running',
    availableVersion: 'v1.9.6',
    protocols: ['VLESS', 'Trojan', 'WireGuard', 'Shadowsocks'],
    locked: false
  }
];

export const mockRevisions: ConfigRevision[] = [
  {
    id: 'rev-01',
    versionCode: 'cfg-rev-20261001-01',
    author: 'root_owner',
    summary: '新增 OpenAI 规则与双跳代理链链路优化',
    affectedNodes: ['HK-Edge-01', 'HK-Edge-02', 'JP-Tokyo-01', 'US-LA-Direct-01'],
    status: 'success',
    createdAt: '2026-10-01 00:30:00',
    changes: {
      users: '无变化',
      inbounds: '更新 HK-VLESS-Reality 握手参数',
      outbounds: '新增 Relay-To-Tokyo 出站',
      routing: '新增 rule-openai-ai 策略',
      chains: '新增 chain-hk-jp-us 双跳配置'
    }
  },
  {
    id: 'rev-02',
    versionCode: 'cfg-rev-20260929-02',
    author: 'ops_david',
    summary: '调整恶意挖矿域名过滤黑名单',
    affectedNodes: ['HK-Edge-01', 'HK-Edge-02'],
    status: 'success',
    createdAt: '2026-09-29 18:20:00',
    changes: {
      routing: '更新 security-block 规则 CIDR'
    }
  },
  {
    id: 'rev-03',
    versionCode: 'cfg-rev-20260928-01',
    author: 'ops_david',
    summary: '硅谷节点配置更新测试',
    affectedNodes: ['US-SJC-Egress-02'],
    status: 'rollback',
    createdAt: '2026-09-28 10:15:00',
    changes: {
      outbounds: '修改 Trojan 加密参数导致校验未通过自动回滚'
    }
  }
];

export const mockBackups: BackupItem[] = [
  {
    id: 'bak-1',
    name: 'auto_backup_daily_20261001.bak',
    timestamp: '2026-10-01 00:00:00',
    type: '自动',
    fileSize: 42 * 1024 * 1024, // 42MB
    dbStatus: '正常',
    checksumStatus: '通过',
    creator: 'System Cron'
  },
  {
    id: 'bak-2',
    name: 'manual_backup_before_revision_01.bak',
    timestamp: '2026-10-01 00:25:00',
    type: '手动',
    fileSize: 41.8 * 1024 * 1024,
    dbStatus: '正常',
    checksumStatus: '通过',
    creator: 'root_owner'
  },
  {
    id: 'bak-3',
    name: 'auto_backup_daily_20260930.bak',
    timestamp: '2026-09-30 00:00:00',
    type: '自动',
    fileSize: 40.5 * 1024 * 1024,
    dbStatus: '正常',
    checksumStatus: '通过',
    creator: 'System Cron'
  }
];

export const mockOperationLogs: OperationLog[] = [
  {
    id: 'op-1',
    timestamp: '2026-10-01 00:30:10',
    operator: 'root_owner',
    action: '发布配置版本',
    target: 'cfg-rev-20261001-01',
    result: '成功',
    ip: '127.0.0.1',
    beforeSummary: '配置版本 20260929-02',
    afterSummary: '配置版本 20261001-01 (4个节点完成更新)'
  },
  {
    id: 'op-2',
    timestamp: '2026-10-01 00:28:44',
    operator: 'root_owner',
    action: '创建代理链',
    target: 'chain-hk-jp-us',
    result: '成功',
    ip: '127.0.0.1',
    afterSummary: 'HK-Edge-01 -> JP-Tokyo-01 -> US-LA-Direct-01'
  },
  {
    id: 'op-3',
    timestamp: '2026-09-30 22:10:00',
    operator: 'ops_david',
    action: '设置节点维护',
    target: 'US-SJC-Egress-02',
    result: '成功',
    ip: '116.23.44.18',
    beforeSummary: '在线',
    afterSummary: '维护中 (排空流量)'
  },
  {
    id: 'op-4',
    timestamp: '2026-09-30 19:45:12',
    operator: 'root_owner',
    action: '禁用异常用户',
    target: 'bot_crawler',
    result: '成功',
    ip: '127.0.0.1',
    beforeSummary: '用户状态: active',
    afterSummary: '用户状态: disabled (切断当前连接)'
  }
];

export const mockDashboardStats = {
  onlineUsers: 4,
  totalUsers: 5,
  onlineDevices: 4,
  totalDevices: 5,
  onlineNodes: 4,
  totalNodes: 6,
  errorNodes: 1,
  maintenanceNodes: 1,
  currentConnections: 1065,
  todayUploadBytes: 70.8 * 1024 * 1024 * 1024, // 70.8 GB
  todayDownloadBytes: 387.5 * 1024 * 1024 * 1024, // 387.5 GB
  todayBlockedCount: 148,
  sseConnected: true
};
