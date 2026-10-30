<template>
  <FormDrawer v-model="createOpen" title="新增服务器" description="① 填写服务器信息 → ② 在目标服务器执行一次性安装命令" :loading="creating" confirm-text="生成安装命令" @confirm="create">
    <label class="block text-xs text-ios-secondary">服务器名称<input v-model.trim="name" maxlength="40" class="form-input-base mt-1.5" placeholder="例如 东京 01" /></label>
    <details class="text-sm text-ios-secondary">
      <summary class="cursor-pointer">续费信息（可选）</summary>
      <div class="space-y-3 mt-3">
        <label class="block">续费到期日<input v-model="renewDate" type="date" class="form-input-base" /></label>
        <label class="block">币种<select v-model="currency" class="form-input-base"><option v-for="c in CURRENCY_LIST" :key="c.code" :value="c.code">{{ c.label }}</option></select></label>
        <label class="block">续费价格<input v-model="price" type="number" min="0" step="any" class="form-input-base" /></label>
        <label class="block">周期<select v-model="cycle" class="form-input-base"><option v-for="c in CYCLE_LIST" :key="c">{{ c }}</option></select></label>
      </div>
    </details>
    <p v-if="error" role="alert" class="text-sm text-ios-red">{{ error }}</p>
  </FormDrawer>
  <FormDrawer v-model="installOpen" title="一键安装命令" description="② 在服务器终端以 root 用户运行命令，完成 Agent 部署与证书信任配置。" :show-footer="false" width="2xl">
    <div v-if="installAvailable" class="space-y-3">
      <div class="grid gap-3 sm:grid-cols-3">
        <label class="text-xs text-ios-secondary">面板地址<input v-model.trim="host" maxlength="253" class="form-input-base mt-1.5" /></label>
        <label class="text-xs text-ios-secondary">HTTP 端口<input v-model.number="port" type="number" min="1" max="65535" class="form-input-base mt-1.5" /></label>
        <div class="text-xs text-ios-secondary">运行内核
          <div class="mt-1.5 flex flex-wrap items-center gap-3 rounded-xl bg-ios-bg px-3 py-2">
            <label v-for="option in CORE_OPTIONS" :key="option.name" class="inline-flex items-center gap-1.5 text-ios-label">
              <input v-model="cores" type="checkbox" :value="option.name" class="accent-ios-blue" />{{ option.label }}
            </label>
          </div>
        </div>
      </div>
      <p v-if="!cores.length" role="alert" class="text-xs text-ios-red">请至少选择一个运行内核（xray 或 sing-box）。</p>
      <p class="text-xs text-ios-tertiary">命令按所选内核写入重复的 <code class="font-mono">--core</code> 参数（例如 <code class="font-mono">--core xray --core sing-box</code>）；sing-box 仍使用默认的 qui 同版本来源。</p>
      <CodePreview v-if="command" :code="command" title="一键安装命令" />
      <p v-else class="text-sm text-ios-red">请填写有效的面板地址与端口</p>
    </div>
    <div v-else class="space-y-2">
      <CodePreview :code="enrollment?.enrollmentToken || ''" />
      <p class="text-xs text-ios-tertiary">本地 / 开发构建无法生成一键命令，请使用注册码手动登记。</p>
    </div>
    <p class="text-xs text-ios-tertiary">命令含一次性注册码，{{ formatTime(enrollment?.expiresAt || '') }} 前有效。重新生成会撤销旧注册码。</p>
    <p v-if="metaWarning" role="alert" class="text-sm text-ios-orange">{{ metaWarning }}</p>
    <button type="button" class="ios-dialog-btn-secondary" @click="installOpen = false">关闭</button>
  </FormDrawer>
</template>
<script setup lang="ts">
import { computed, ref } from 'vue';
import { nodeAPI, installCommand, type CoreName, type Enrollment, type NodeView } from '../api';
import { CURRENCY_LIST, CYCLE_LIST, saveNodeMeta } from '../nodeMeta';
import { formatTime } from '../../../app/auth';
import { showToast } from '../../../app/api';
import FormDrawer from '../../../components/FormDrawer.vue';
import CodePreview from '../../../components/CodePreview.vue';
const emit = defineEmits<{ changed: [] }>();
// A fresh node gets Xray by default; sing-box is optional. `--core` is repeatable
// (K3c), so the dialog keeps the selection as a list and never sends an empty one.
const CORE_OPTIONS: { name: CoreName; label: string }[] = [{ name: 'xray', label: 'Xray' }, { name: 'sing-box', label: 'sing-box' }];
const createOpen = ref(false), installOpen = ref(false), creating = ref(false);
const name = ref(''), renewDate = ref(''), currency = ref('CNY'), price = ref(''), cycle = ref('/年');
const error = ref(''), metaWarning = ref('');
const enrollment = ref<Enrollment | null>(null);
const host = ref(''), port = ref(443), cores = ref<CoreName[]>(['xray']);
const installAvailable = computed(() => Boolean(enrollment.value?.install?.version && enrollment.value?.install?.caSha256));
const command = computed(() => {
  const item = enrollment.value;
  if (!item || !installAvailable.value || !/^[A-Za-z0-9.-]+$/.test(host.value) || !Number.isInteger(port.value) || port.value < 1 || port.value > 65535) return '';
  if (!cores.value.length) return '';
  return installCommand({ version: item.install.version, host: host.value, httpPort: port.value,
    grpcPort: item.install.grpcPort, caSha256: item.install.caSha256, token: item.enrollmentToken, cores: [...cores.value] });
});
function showInstall(item: Enrollment) {
  enrollment.value = item;
  host.value = window.location.hostname;
  port.value = Number(window.location.port) || 443;
  cores.value = ['xray'];
  installOpen.value = true;
}
async function create() {
  if (creating.value) return;
  if (!name.value) { error.value = '请输入服务器名称'; return; }
  creating.value = true; error.value = ''; metaWarning.value = '';
  try {
    const item = await nodeAPI.enroll(name.value);
    // The enrollment succeeded: always show its token, even if optional metadata fails.
    showInstall(item); createOpen.value = false;
    try {
      await saveNodeMeta(item.node.id, { renewDate: renewDate.value, renewCurrency: currency.value,
        renewPrice: price.value.trim(), renewCycle: cycle.value });
    } catch (e) { metaWarning.value = `服务器已创建，续费信息未保存：${(e as Error).message}。请在详情设置中补填。`; }
    name.value = ''; renewDate.value = ''; currency.value = 'CNY'; price.value = ''; cycle.value = '/年';
    emit('changed');
  } catch (e) { error.value = (e as Error).message; }
  finally { creating.value = false; }
}
const reissuing = ref(false);
async function reissue(node: NodeView) {
  if (reissuing.value) return;
  reissuing.value = true; metaWarning.value = '';
  try { showInstall(await nodeAPI.reissueEnrollment(node.id)); }
  catch (e) { showToast((e as Error).message, 'error'); }
  finally { reissuing.value = false; }
}
function installCode(id: string) { return enrollment.value?.node.id === id ? installAvailable.value ? command.value : enrollment.value.enrollmentToken : ''; }
defineExpose({ installCode, open: () => { error.value = ''; createOpen.value = true; }, reissue });
</script>
