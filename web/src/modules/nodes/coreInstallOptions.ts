import { ref } from 'vue';
import type { CoreName } from './api';

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
    hint: '仅 linux/amd64，只做端口转发与隧道，需同时选择 Xray 或 sing-box'
  }
];

export const CORE_INSTALL_ORDER: readonly CoreName[] = ['xray', 'sing-box', 'gost'];

/**
 * 校验选中的安装内核列表是否有效：
 * 1. 不能为空
 * 2. 不能只勾选 GOST（GOST 仅作为辅助转发核心，不能单独安装，需同时选择 Xray 或 sing-box）
 */
export function validateInstallCores(cores: CoreName[]): { valid: boolean; error?: string } {
  if (!cores.length) {
    return { valid: false, error: '请至少选择一个运行内核（Xray 或 sing-box）。' };
  }
  const hasPrimary = cores.some(c => c === 'xray' || c === 'sing-box');
  if (cores.includes('gost') && !hasPrimary) {
    return {
      valid: false,
      error: 'GOST 仅做端口转发与隧道，需同时选择 Xray 或 sing-box，不能单独作为运行内核。'
    };
  }
  return { valid: true };
}

/**
 * 将内核按安装规范排序：Xray/sing-box 在前，GOST 永远排在最后（安装脚本要求 GOST 不能是首项）。
 */
export function sortInstallCores(cores: Iterable<CoreName>): CoreName[] {
  const set = new Set(cores);
  return CORE_INSTALL_ORDER.filter(name => set.has(name));
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
