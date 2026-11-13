<template>
  <div class="space-y-6">
    <section class="panel-card p-6 space-y-4">
      <div class="flex flex-wrap justify-between items-center gap-2">
        <h2 class="text-base font-semibold leading-normal">我的订阅</h2>
        <button v-if="!preview" :disabled="busy" class="ios-btn-secondary" @click="resetVisible = true">重置订阅链接</button>
      </div>
      <p v-if="subscription.state === 'not_recoverable'" class="text-ios-secondary">重置后可查看：旧订阅链接无法恢复，原链接仍可使用。</p>
      <p v-else-if="subscription.state === 'missing'" class="text-ios-secondary">尚无订阅链接，请重置以创建。</p>
      <template v-else>
        <div v-if="subscription.path" class="space-y-2 border-b border-black/[0.05] pb-4">
          <div class="text-sm font-semibold">{{ AUTO_SUBSCRIPTION_LABEL }}</div>
          <p class="text-xs text-ios-secondary">按客户端自动选择格式；无法识别的客户端会得到系统默认格式，可能无法导入，此时请改用下面指定格式的链接。</p>
          <div class="flex flex-wrap gap-2">
            <input readonly :value="absolute(subscription.path)" class="form-input-base font-mono select-all flex-1 min-w-0" />
            <button class="ios-btn-secondary" @click="copy(subscription.path)">复制</button>
            <button class="ios-btn-secondary" @click="showQR(AUTO_SUBSCRIPTION_LABEL, subscription.path)">二维码</button>
          </div>
        </div>
        <div v-for="client in clients" :key="client.format" class="space-y-2">
          <div class="text-sm font-semibold">{{ client.label }}<span v-if="client.format === subscription.defaultFormat" class="ml-2 text-xs text-ios-blue">系统默认</span></div>
          <p v-if="client.clients" class="text-xs text-ios-secondary">{{ client.clients }}</p>
          <div class="flex flex-wrap gap-2">
            <input readonly :value="absolute(urlOf(client.format))" class="form-input-base font-mono select-all flex-1 min-w-0" />
            <button class="ios-btn-secondary" @click="copy(urlOf(client.format))">复制</button>
            <button class="ios-btn-secondary" @click="showQR(client.label, urlOf(client.format))">二维码</button>
          </div>
        </div>
        <p class="text-xs text-ios-tertiary">订阅链接包含接入凭据，请妥善保存。</p>
      </template>
      <p v-if="notice" role="status" class="text-ios-blue text-sm">{{ notice }}</p>
    </section>

    <section class="panel-card p-6 space-y-2">
      <h2 class="text-base font-semibold leading-normal">导入步骤</h2>
      <ol class="list-decimal pl-5 text-sm space-y-1">
        <li>复制上面与你的客户端对应的链接。</li>
        <li>在客户端中添加订阅或配置，粘贴链接。</li>
        <li>更新订阅，节点和规则即可使用。</li>
      </ol>
    </section>

    <Teleport to="body">
      <div v-if="qrVisible" class="fixed inset-0 z-50 bg-black/40 flex items-center justify-center p-4" @click.self="qrVisible = false">
        <div class="w-full max-w-sm bg-white rounded-3xl border border-black/[0.08] p-6 space-y-4" role="dialog" aria-modal="true" :aria-label="qrTitle">
          <h3 class="text-lg font-semibold leading-snug">{{ qrTitle }}</h3>
          <p v-if="qrError" role="alert" class="text-ios-red text-sm">{{ qrError }}</p>
          <div v-else class="p-4 bg-white w-fit mx-auto rounded-xl border" v-html="qr"></div>
          <div class="flex justify-end"><button class="ios-btn-secondary" @click="qrVisible = false">关闭</button></div>
        </div>
      </div>
    </Teleport>
    <ConfirmDialog v-model="resetVisible" title="重置订阅链接" message="重置后旧链接立即失效，客户端需要重新配置。确定继续？" @confirm="reset" />
  </div>
</template>
<script setup lang="ts">
import { computed, ref } from 'vue';
import { orderedSubscriptionClients, AUTO_SUBSCRIPTION_LABEL } from '../app/subscriptionClients';
import { generateQRCodeSVG } from '../app/qrcode';
import ConfirmDialog from '../components/ConfirmDialog.vue';
import { portalAPI, type Subscription } from './api';
const props = defineProps<{ subscription: Subscription; preview?: boolean }>();
const emit = defineEmits<{ (e: 'update', value: Subscription): void }>();
const notice = ref(''), busy = ref(false), resetVisible = ref(false);
const qr = ref(''), qrError = ref(''), qrTitle = ref(''), qrVisible = ref(false);
const clients = computed(() => orderedSubscriptionClients((props.subscription.formats ?? []).map(f => f.id), props.subscription.defaultFormat));
function urlOf(format: string) { return props.subscription.formats?.find(f => f.id === format)?.url ?? ''; }
function absolute(path: string) { return new URL(path, window.location.origin).href; }
async function copy(path: string) { try { await navigator.clipboard.writeText(absolute(path)); notice.value = '已复制到剪贴板'; } catch { notice.value = '复制失败，请手动选择链接复制'; } }
function showQR(title: string, path: string) {
  qr.value = ''; qrError.value = ''; qrTitle.value = title + ' 二维码'; qrVisible.value = true;
  try { qr.value = generateQRCodeSVG(absolute(path), 200); } catch { qrError.value = '链接过长，无法生成二维码，请复制链接。'; }
}
async function reset() {
  if (props.preview || busy.value) return;
  busy.value = true;
  try { emit('update', await portalAPI.reset()); qrVisible.value = false; notice.value = '订阅链接已重置，旧链接已失效'; }
  catch (e) { notice.value = (e as Error).message; } finally { busy.value = false; }
}
</script>
