<template>
  <span :class="['inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-[11px] font-semibold border tracking-tight', classes[tone]]">
    <span :class="['w-1.5 h-1.5 rounded-full shrink-0', dots[tone]]"></span><span>{{ label || displayLabel }}</span>
  </span>
</template>
<script setup lang="ts">
import { computed } from 'vue';
const props = withDefaults(defineProps<{ status: string; label?: string; kind?: 'generic' | 'node' }>(), { label: '', kind: 'generic' });
const nodeLabels: Record<string, string> = { pending: '待注册', online: '在线', degraded: '降级', offline: '离线', applying: '应用配置中', rollback: '回滚中', disabled: '已禁用', error: '异常' };
const genericLabels: Record<string, string> = { online: '在线', offline: '离线', active: '正常启用', disabled: '已禁用', maintenance: '维护中', expired: '已到期', healthy: '网络健康', warning: '异常波动', unhealthy: '节点故障', draft: '草稿', pending: '待发布', deploying: '发布中', success: '发布成功', partial: '部分成功', failed: '发布失败', rollback: '已回滚', up_to_date: '最新版本', 'up-to-date': '最新版本', running: '运行中', stopped: '已停止' };
const displayLabel = computed(() => (props.kind === 'node' ? nodeLabels : genericLabels)[props.status.toLowerCase()] || props.status);
type Tone = 'green' | 'yellow' | 'red' | 'gray' | 'blue';
const tone = computed<Tone>(() => {
  const status = props.status.toLowerCase();
  if (props.kind === 'node') return ({ online: 'green', degraded: 'yellow', error: 'red', applying: 'blue', rollback: 'yellow' } as Record<string, Tone>)[status] || 'gray';
  if (['online', 'active', 'healthy', 'success', 'up_to_date', 'up-to-date', 'running'].includes(status)) return 'green';
  if (['warning', 'maintenance', 'partial', 'pending', 'rollback'].includes(status)) return 'yellow';
  if (['deploying', 'applying', 'updating', 'reloading'].includes(status)) return 'blue';
  if (['expired', 'unhealthy', 'failed', 'error'].includes(status)) return 'red';
  return 'gray';
});
const classes: Record<Tone, string> = {
  green: 'bg-emerald-50 text-emerald-800 border-emerald-200',
  yellow: 'bg-amber-50 text-amber-800 border-amber-200',
  red: 'bg-rose-50 text-rose-800 border-rose-200',
  gray: 'bg-black/[0.05] text-[#48484A] border-black/[0.08]',
  blue: 'bg-blue-50 text-[#0071E3] border-blue-200'
};
const dots: Record<Tone, string> = {
  green: 'bg-emerald-500',
  yellow: 'bg-amber-500',
  red: 'bg-rose-500',
  gray: 'bg-[#6E6E73]',
  blue: 'bg-[#0071E3]'
};
</script>
