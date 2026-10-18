<template>
  <div class="space-y-4 text-sm">
    <p class="text-xs text-gray-500">校验时间：{{ formatTime(result.checkedAt) }}。这是当前观测结果；执行恢复前仍须重新校验并停写。</p>
    <p class="p-3 rounded-xl bg-amber-50 text-amber-800">当前为只读预览，没有替换数据库或节点配置。托管服务可另行确认执行；执行前会重新停写并核对账本。</p>
    <div class="overflow-x-auto"><table class="w-full text-sm"><thead><tr class="text-left text-gray-500"><th class="py-2">数据范围</th><th>当前系统</th><th>备份快照</th></tr></thead><tbody><tr v-for="item in totals" :key="item.key" class="border-t"><td class="py-2">{{ item.label }}</td><td>{{ result.current[item.key] ?? 0 }}</td><td>{{ result.snapshot[item.key] ?? 0 }}</td></tr></tbody></table></div>
    <dl class="grid md:grid-cols-2 gap-3"><div><dt class="text-gray-500">配置密钥材料</dt><dd>{{ result.sameKeyMaterial ? '与当前文件匹配' : '与当前文件不同，需要匹配替换' }}</dd></div><div><dt class="text-gray-500">节点 CA</dt><dd>{{ result.sameNodeCA ? '与当前文件匹配' : '与当前文件不同，需要重新核对节点信任' }}</dd></div></dl>
    <section class="space-y-2"><h3 class="font-semibold">已观测的恢复影响</h3><p v-if="!changes.length" class="text-gray-500 text-xs">这些检查项未观测到差异；不代表草稿、凭据或所有业务字段相同，也不代表节点账本已核对。</p><div v-for="item in changes" :key="item.key" class="flex justify-between gap-4 border-b py-2"><span>{{ item.label }}</span><strong>{{ result.impact[item.key] }}</strong></div></section>
    <section class="space-y-2"><h3 class="font-semibold">恢复流程需要处理</h3><ul class="list-disc pl-5 space-y-2 text-xs text-gray-600"><li v-for="issue in result.issues" :key="issue">{{ issueLabel(issue) }}</li></ul></section>
  </div>
</template>
<script setup lang="ts">
import { computed } from 'vue';
import { formatTime } from '../../../app/auth';
import type { RestorePreview, RestoreTotals, RestoreImpact } from '../backupApi';
const props = defineProps<{ result: RestorePreview }>();
const totals: { key: keyof RestoreTotals; label: string }[] = [{ key: 'admins', label: '管理员' }, { key: 'activeOwners', label: '启用的 Owner' }, { key: 'sessions', label: '会话记录' }, { key: 'users', label: '代理用户' }, { key: 'devices', label: '设备' }, { key: 'nodes', label: '节点' }, { key: 'nodeGroups', label: '节点组' }, { key: 'policies', label: '分流策略' }, { key: 'revisions', label: '配置版本' }, { key: 'pendingOperations', label: '待处理发布/回滚记录' }, { key: 'backupFileOperations', label: '备份文件准备记录' }, { key: 'runningBackupJobs', label: '备份任务执行中记录' }];
const impactLabels: { key: keyof RestoreImpact; label: string }[] = [{ key: 'usersRemoved', label: '当前用户在备份中不存在' }, { key: 'nodesRemoved', label: '当前节点在备份中不存在' }, { key: 'nodesAdded', label: '备份节点在当前系统中不存在' }, { key: 'sequenceRegressions', label: '节点消息序号回退' }, { key: 'runtimeDifferences', label: '节点确认运行版本不同' }, { key: 'certificateDifferences', label: '节点证书身份或吊销状态不同' }, { key: 'revocationRegressions', label: '已吊销证书在备份中未吊销' }, { key: 'usageRegressions', label: '用户已确认流量回退或丢失' }, { key: 'userReactivations', label: '当前停用用户在备份中启用' }];
const changes = computed(() => impactLabels.filter(item => props.result.impact[item.key] > 0));
function issueLabel(code: string) { return ({ restore_executor_pending: '停写、替换、服务重建和失败回滚流程仍待完成。', backup_file_journal_reconciliation: '备份中可能包含创建归档时的文件准备记录，恢复时不能直接按旧日志删除当前文件。', backup_job_reconciliation: '备份中包含未完成备份任务记录，恢复时需要协调，不能直接续跑旧任务。', active_owner_required: '备份中必须包含一个启用的 Owner，当前快照不满足。', session_revocation_required: '恢复时需要撤销旧会话，避免恢复过去的登录授权。', snapshot_pending_deployments: '备份中存在未完成发布或回滚，不能直接重新执行旧命令。', current_pending_deployments: '当前系统存在未完成发布或回滚，需要协调后停写。', node_reconciliation_required: '需要核对各节点当前版本、消息确认及凭据，中央备份不能证明节点本地状态。', message_cursor_regression: '备份的节点消息序号低于当前记录，需要防止重复执行或丢失补传。', certificate_revocation_regression: '备份可能恢复已撤销的证书身份，需要保留安全撤销状态。', observed_usage_regression: '备份缺少已确认流量，需要核对并保持真实已消费量。', user_reactivation: '备份可能重新启用当前停用用户，需要核对访问授权。', configuration_key_change: '归档使用不同配置密钥，数据库必须与匹配的原始材料一起恢复。', node_ca_change: '归档使用不同节点 CA，需要核对节点信任与证书。' } as Record<string, string>)[code] || code; }
</script>
