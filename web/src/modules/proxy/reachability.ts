import { request } from '../../app/apiClient';
import type { ProxyNodeItem } from './proxyNodes';

interface Reachability {
  supported: boolean;
  reachable: boolean | null;
  code: string;
  transport: 'tcp' | 'tls' | 'udp';
}

export async function checkNodeReachability(node: ProxyNodeItem): Promise<{ message: string; kind: 'info' | 'success' | 'error' }> {
  if (node.sourceType !== 'self_hosted' || !node.inboundId) {
    return { message: '当前仅支持自建节点的面板 TCP/TLS 连通性检查', kind: 'info' };
  }
  const result = await request<Reachability>(`/inbounds/${encodeURIComponent(node.inboundId)}/reachability`, 'POST');
  if (result.code === 'udp_probe_unsupported') {
    return { message: `${node.name}：未检测，面板暂不支持 UDP/QUIC 协议探测，请用客户端验证连接`, kind: 'info' };
  }
  if (!result.supported || result.reachable === null) throw new Error('面板返回了无法识别的连通性检查结果');
  const scope = result.transport === 'tls' ? 'TCP/TLS' : 'TCP';
  return {
    message: `${node.name}：面板 ${scope} ${result.reachable ? '检查通过，尚未验证代理认证和转发' : '检查失败，请检查监听和网络'}`,
    kind: result.reachable ? 'success' : 'error',
  };
}
