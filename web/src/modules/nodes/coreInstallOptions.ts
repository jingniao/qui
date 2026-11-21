import { ref } from 'vue';
import type { CoreName, NodeView } from './api';

export interface CoreInstallOption {
  name: CoreName;
  label: string;
  hint?: string;
}

export const CORE_INSTALL_OPTIONS: readonly CoreInstallOption[] = [
  { name: 'xray', label: 'Xray' },
  { name: 'sing-box', label: 'sing-box' },
  {
    name: 'gost',
    label: 'GOST',
    hint: '仅 linux/amd64，端口转发与隧道'
  }
];

export const CORE_INSTALL_ORDER: readonly CoreName[] = ['xray', 'sing-box', 'gost'];

export function installCoresDescription(cores: CoreName[]): string {
  if (!cores.length) return '仅安装 Agent，登记后在 内核管理 添加';
  if (cores.length === 1 && cores[0] === 'gost') return '纯转发节点：只做端口转发与隧道，不承载用户（仅 linux/amd64）';
  return '';
}

// 用户内核在前，GOST 在后；空列表与仅 GOST 均为有效安装选项。
export function sortInstallCores(cores: Iterable<CoreName>, primary?: string): CoreName[] {
  const set = new Set(cores);
  const ordered = CORE_INSTALL_ORDER.filter(name => set.has(name));
  if ((primary === 'sing-box' || primary === 'xray') && set.has(primary)) {
    return [primary, ...ordered.filter(name => name !== primary)];
  }
  return ordered;
}

// The recovery response carries the token. The server list/detail carries the
// last actual capability report, including the scalar core on older Agents.
export function reportedInstallCores(capabilities: NodeView['capabilities']): CoreName[] {
  const names = (capabilities?.cores ?? []).map(core => core.name);
  if (!names.length && capabilities?.core_name) names.push(capabilities.core_name as CoreName);
  return sortInstallCores(names, capabilities?.core_name);
}

// 记录当前正在重新签发注册码的待登记节点 ID
export const reissuingNodeId = ref<string | null>(null);

export function setReissuingNodeId(id: string | null) {
  reissuingNodeId.value = id;
}

export function isNodeReissuing(nodeId?: string | null): boolean {
  if (!nodeId) return false;
  return reissuingNodeId.value === nodeId;
}
