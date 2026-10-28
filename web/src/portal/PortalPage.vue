<template>
  <div class="space-y-6">
    <PageHeader :title="titles[section] || '概览'" description="一个账户一条订阅，可在多台设备上使用。" />
    <ErrorState v-if="error" :message="error" @retry="load" />
    <LoadingSkeleton v-else-if="loading" />
    <template v-else-if="data">
      <div v-if="section === 'overview'" class="space-y-6">
        <div class="grid grid-cols-1 sm:grid-cols-3 gap-4">
          <div class="panel-card p-5 space-y-2"><div class="text-xs text-ios-tertiary">账号状态</div><StatusBadge :status="data.me.status" /><p>{{ reasons[data.me.reason] || data.me.reason || '正常服务' }}</p></div>
          <div class="panel-card p-5 space-y-2"><div class="text-xs text-ios-tertiary">已用流量 / 额度</div><TrafficValue :bytes="data.me.usedTraffic" /> / {{ data.me.trafficLimit ? bytes(data.me.trafficLimit) : '不限额' }}<div class="h-1.5 bg-black/[0.05] rounded-full overflow-hidden"><div class="h-full bg-ios-blue" :style="{ width: quota + '%' }"></div></div></div>
          <div class="panel-card p-5 space-y-2"><div class="text-xs text-ios-tertiary">到期时间</div><p>{{ data.me.expiresAt ? new Date(data.me.expiresAt).toLocaleString() : '永不过期' }}</p></div>
        </div>
        <section class="panel-card p-6 space-y-3"><h2 class="font-semibold">在线设备 / 上限</h2><p>{{ data.me.onlineDevices ?? '未知' }} / {{ data.me.deviceLimit === 0 ? '不限' : data.me.deviceLimit ?? '—' }}</p>
          <p v-if="data.me.deviceLimit !== null && data.me.deviceLimit > 0 && data.me.onlineDevices !== null && data.me.onlineDevices >= data.me.deviceLimit" class="text-amber-700">设备数量已达上限</p>
          <p v-if="!data.me.deviceLimitEnforced" class="text-xs text-amber-700">部分执行：{{ data.me.deviceLimitUnsupportedNodes?.join('、') }}</p>
          <p v-for="(item, index) in onlineAddresses" :key="index" class="text-xs font-mono">{{ item.address }} · {{ item.allowed ? '允许' : '受限' }} · {{ item.active ? '活跃' : '等待释放' }}</p>
          <details class="text-xs text-ios-secondary"><summary>设备上限的限制与隐私</summary><p v-for="help in deviceLimitHelp" :key="help" class="mt-2">{{ help }}</p></details>
          <p v-if="data.me.wireGuardNote" class="text-xs text-ios-secondary">{{ data.me.wireGuardNote }}</p>
        </section>
      </div>
      <section v-else-if="section === 'subscription'" class="panel-card p-6 space-y-4">
        <div class="flex justify-between items-center"><h2 class="font-semibold">专属订阅与配置</h2><button :disabled="preview || busy" class="ios-btn-secondary" @click="resetVisible = true">重置订阅链接</button></div>
        <p v-if="data.subscription.state === 'not_recoverable'" class="text-ios-secondary">重置后可查看：旧订阅链接无法恢复，原链接仍可使用。</p>
        <p v-else-if="data.subscription.state === 'missing'" class="text-ios-secondary">尚无订阅链接，请重置以创建。</p>
        <div v-for="format in data.subscription.formats || []" :key="format.id" class="space-y-2">
          <label class="text-xs font-semibold">{{ format.id }}</label>
          <div class="flex gap-2"><input readonly :value="absolute(format.url)" class="form-input-base text-xs font-mono select-all" /><button class="ios-btn-secondary" @click="copy(format.url)">复制</button><button class="ios-btn-secondary" @click="showQR(format.url)">二维码</button></div>
        </div>
        <p class="text-xs text-ios-tertiary">订阅链接包含接入凭据，请妥善保存。</p>
        <p v-if="qrError" role="alert" class="text-ios-red">{{ qrError }}</p><div v-if="qr" class="p-4 bg-white w-fit rounded-xl border" v-html="qr"></div>
      </section>
      <section v-else-if="section === 'nodes'" class="panel-card p-6 space-y-4">
        <h2 class="font-semibold">可用节点</h2><p v-if="!data.nodes.items.length" class="text-ios-tertiary">暂无授权节点</p>
        <div v-for="node in data.nodes.items" :key="node.id" class="flex justify-between border-b border-black/[0.05] py-3"><span>{{ node.name }} <small class="text-ios-tertiary">{{ node.region || '—' }}</small></span><StatusBadge :status="node.online ? 'online' : 'offline'" /></div>
      </section>
      <section v-else-if="section === 'traffic'" class="panel-card p-6 space-y-4">
        <div class="flex justify-between"><h2 class="font-semibold">流量趋势</h2><select v-model="range" :disabled="preview" class="form-input-base max-w-40"><option value="24h">最近 24 小时</option><option value="7d">最近 7 天</option><option value="30d">最近 30 天</option></select></div>
        <p v-if="preview" class="text-ios-secondary">只读预览仅提供账号、订阅和节点；流量历史请在用户详情查看。</p>
        <p v-else-if="!traffic.items.length" class="text-ios-tertiary">暂无流量记录</p>
        <div v-else class="space-y-3" role="img" :aria-label="range === '24h' ? '每小时上传和下载流量' : '每日上传和下载流量'">
          <div v-for="item in traffic.items" :key="item.bucket" class="text-xs space-y-1">
            <div class="flex justify-between gap-2"><span>{{ item.bucket }}</span><span>上传 {{ bytes(item.uploadBytes) }} · 下载 {{ bytes(item.downloadBytes) }}</span></div>
            <div class="h-3 flex rounded bg-black/[0.04] overflow-hidden"><div class="bg-ios-blue" :style="{ width: item.uploadBytes / maxTraffic * 100 + '%' }"></div><div class="bg-emerald-500" :style="{ width: item.downloadBytes / maxTraffic * 100 + '%' }"></div></div>
          </div>
        </div>
      </section>
      <form v-else-if="section === 'account'" class="panel-card p-6 space-y-4" @submit.prevent="changePassword">
        <h2 class="font-semibold">修改登录密码</h2><p class="text-sm">{{ data.me.displayName || data.me.username }}</p>
        <label class="block text-xs">当前密码<input v-model="currentPassword" :disabled="preview" type="password" autocomplete="current-password" required class="form-input-base mt-2" /></label>
        <label class="block text-xs">新密码<input v-model="newPassword" :disabled="preview" type="password" autocomplete="new-password" required class="form-input-base mt-2" /></label>
        <label class="block text-xs">确认新密码<input v-model="confirmPassword" :disabled="preview" type="password" autocomplete="new-password" required class="form-input-base mt-2" /></label>
        <p class="text-xs text-ios-tertiary">修改后其他登录会话立即退出。</p><button :disabled="preview || busy" class="ios-btn-primary">保存密码</button>
      </form>
    </template>
    <p v-if="notice" role="status" class="text-ios-blue text-sm">{{ notice }}</p>
    <ConfirmDialog v-model="resetVisible" title="重置订阅链接" message="重置后旧链接立即失效，客户端需要重新配置。确定继续？" @confirm="reset" />
  </div>
</template>
<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { deviceLimitHelp } from '../app/deviceLimit';
const onlineAddresses = ref<{ address: string; allowed: boolean; active: boolean }[]>([]);
import { portalAPI, type Snapshot, type Traffic } from './api';
import { generateQRCodeSVG } from '../app/qrcode';
import PageHeader from '../components/PageHeader.vue';
import ErrorState from '../components/ErrorState.vue';
import LoadingSkeleton from '../components/LoadingSkeleton.vue';
import TrafficValue from '../components/TrafficValue.vue';
import StatusBadge from '../components/StatusBadge.vue';
import ConfirmDialog from '../components/ConfirmDialog.vue';
const props = defineProps<{ section: string; preview?: boolean; snapshot?: Snapshot }>();
const data = ref<Snapshot>(), loading = ref(false), error = ref(''), notice = ref(''), busy = ref(false);
const range = ref<'24h' | '7d' | '30d'>('7d'), traffic = ref<Traffic>({ items: [], granularity: 'daily' });
const currentPassword = ref(''), newPassword = ref(''), confirmPassword = ref(''), resetVisible = ref(false), qr = ref(''), qrError = ref('');
const titles: Record<string, string> = { overview: '概览', subscription: '我的订阅', nodes: '我的节点', traffic: '流量统计', account: '我的账号' };
const reasons: Record<string, string> = { disabled: '账号已禁用', expired: '账号已到期', over_quota: '流量额度已用完', quota_exceeded: '流量额度已用完', active: '正常服务' };
const quota = computed(() => data.value?.me.trafficLimit ? Math.min(100, data.value.me.usedTraffic / data.value.me.trafficLimit * 100) : 0);
const maxTraffic = computed(() => Math.max(1, ...traffic.value.items.map(i => i.uploadBytes + i.downloadBytes)));
function absolute(path: string) { return new URL(path, window.location.origin).href; }
function bytes(value: number) { return (value / 1073741824).toFixed(3) + ' GiB'; }
async function load() {
  error.value = ''; notice.value = ''; qr.value = ''; qrError.value = ''; loading.value = true;
  currentPassword.value = ''; newPassword.value = ''; confirmPassword.value = '';
  try {
    if (props.preview) { data.value = props.snapshot; return; }
    const [me, nodes, subscription] = await Promise.all([portalAPI.me(), portalAPI.nodes(), portalAPI.subscription()]);
    data.value = { me, nodes, subscription };
    onlineAddresses.value = props.section === "overview" ? (await portalAPI.devicesOnline()).items : [];
    if (props.section === 'traffic') traffic.value = await portalAPI.traffic(range.value);
  } catch (e) { error.value = (e as Error).message; }
  finally { loading.value = false; }
}
async function copy(path: string) { try { await navigator.clipboard.writeText(absolute(path)); notice.value = '已复制到剪贴板'; } catch { notice.value = '复制失败，请手动选择链接复制'; } }
function showQR(path: string) { qr.value = ''; qrError.value = ''; try { qr.value = generateQRCodeSVG(absolute(path), 200); } catch { qrError.value = '链接过长，无法生成二维码，请复制链接。'; } }
async function reset() {
  if (props.preview || busy.value) return;
  busy.value = true;
  try { const subscription = await portalAPI.reset(); if (data.value) data.value.subscription = subscription; qr.value = ''; resetVisible.value = false; notice.value = '订阅链接已重置，旧链接已失效'; }
  catch (e) { notice.value = (e as Error).message; } finally { busy.value = false; }
}
async function changePassword() {
  if (props.preview || busy.value) return;
  if (newPassword.value !== confirmPassword.value) { notice.value = '两次输入的新密码不一致'; return; }
  busy.value = true;
  try { await portalAPI.password(currentPassword.value, newPassword.value); currentPassword.value = ''; newPassword.value = ''; confirmPassword.value = ''; notice.value = '密码已修改，其他会话已退出'; }
  catch (e) { notice.value = (e as Error).message; } finally { busy.value = false; }
}
watch(() => [props.section, props.snapshot, range.value], () => void load(), { immediate: true });
</script>
