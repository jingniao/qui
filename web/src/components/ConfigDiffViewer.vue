<template>
  <div class="space-y-4">
    <p class="text-sm text-gray-600">{{ diff.baseRevision ? '#' + diff.baseRevision : '空配置' }} → #{{ diff.revision }} · {{ diff.changes.length }} 项字段变化</p>
    <p class="text-xs text-gray-500">比较已保存的业务配置。凭据和密钥只显示变更标记；发布时仍会执行节点能力、依赖和核心校验。</p>
    <p v-if="!diff.changes.length" class="p-4 bg-gray-50 rounded-xl text-sm text-gray-600">这两个版本没有业务配置变化。</p>
    <div v-else class="space-y-3">
      <section v-for="change in diff.changes" :key="change.category + '/' + change.id + '/' + change.field" class="border rounded-xl p-4 space-y-2">
        <div class="flex justify-between gap-3 text-sm"><span class="font-semibold">{{ categoryLabel(change.category) }} · {{ fieldLabel(change.field) }}</span><span :class="change.kind === 'removed' ? 'text-red-600' : change.kind === 'added' ? 'text-green-700' : 'text-blue-600'">{{ kindLabel(change.kind) }}</span></div>
        <p class="text-xs text-gray-500 break-all">{{ change.id }}</p>
        <div class="grid md:grid-cols-2 gap-3 text-xs"><div class="bg-red-50 rounded-lg p-3"><p class="text-gray-500 mb-2">比较版本</p><pre class="whitespace-pre-wrap break-words">{{ valueLabel(change.before) }}</pre></div><div class="bg-green-50 rounded-lg p-3"><p class="text-gray-500 mb-2">目标版本</p><pre class="whitespace-pre-wrap break-words">{{ valueLabel(change.after) }}</pre></div></div>
        <p v-if="change.hidden" class="text-xs text-gray-500">敏感字段已变化，具体内容保持隐藏。</p>
      </section>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { RevisionDiff } from '../modules/system/deploymentApi';
defineProps<{ diff: RevisionDiff }>();

function categoryLabel(cat: string) {
  const map: Record<string, string> = {
    users: '用户', credentials: '凭据', inbounds: '入站', outbounds: '出站',
    outbound_groups: '出口组', proxy_chains: '代理链', policies: '分流规则', logging: '记录设置', network: '出站网络'
  };
  return map[cat] || cat;
}
function kindLabel(kind: string) { return ({ added: '新增', removed: '移除', modified: '修改' } as Record<string, string>)[kind] || kind; }
function fieldLabel(field: string) {
  return ({ enabled: '启用状态', expires_at: '到期时间', budget: '节点额度', user_id: '所属用户', device_id: '所属设备', protocol: '协议', internal: '内部中转', username: '账号', secret: '协议凭据', public_key: '公钥', listen: '监听地址', port: '端口', credential_ids: '允许凭据', credential_id: '使用凭据', default_outbound: '默认出口', subscription_visible: '订阅可见性', address: '目标地址', target_node_id: '目标节点', failure_policy: '失败策略', backup_target_id: '备用目标', support_tcp: 'TCP 支持', support_udp: 'UDP 支持', probe_url: '探测地址', probe_interval: '探测间隔', strategy: '选择方式', members: '成员', failover: '失败策略', node_ids: '节点顺序', outbound_ids: '每跳出站', final_outbound_id: '最终出站', priority: '优先级', scope: '生效范围', match: '匹配条件', action: '执行动作', name: '名称', timezone: '时区', logging: '记录选项', domain_only: '仅记录域名', 'settings.transport': '传输方式', 'settings.security': '安全方式', 'settings.server_name': '服务器名称', 'settings.method': '加密方式', 'settings.flow': '流控', 'settings.path': '传输路径', 'settings.service_name': '服务名称', 'settings.certificate_pem': 'TLS 证书', 'settings.private_key_pem': 'TLS 私钥', 'settings.trusted_ca_pem': '信任证书', 'settings.wireguard.secret_key': 'WireGuard 私钥', 'settings.wireguard.addresses': '隧道地址', 'settings.wireguard.mtu': '隧道 MTU', outbound_source_ip: '核心出站源 IP', outbound_interface: '核心出站网卡', host_source_ipv4: '服务器强制出站 IPv4', host_source_ipv6: '服务器强制出站 IPv6' } as Record<string, string>)[field] || field;
}
function valueLabel(value: string) {
  if (!value) return '未设置';
  try { const parsed = JSON.parse(value); return typeof parsed === 'string' ? parsed || '空值' : typeof parsed === 'boolean' ? parsed ? '是' : '否' : JSON.stringify(parsed, null, 2); }
  catch { return value; }
}
</script>
