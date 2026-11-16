<template>
  <section class="panel-card p-6 max-w-3xl space-y-5 text-sm">
    <div class="flex flex-wrap justify-between items-center gap-3">
      <h2 class="text-base font-semibold leading-normal">Reality 预设</h2>
      <button class="ios-btn-secondary" :disabled="loading || saving" @click="load(true)">刷新已保存预设</button>
    </div>
    <ErrorState v-if="error" :message="error" @retry="load(true)" /><LoadingSkeleton v-else-if="!saved" />
    <template v-else>
      <p class="text-xs text-gray-500">创建节点与中转入口时填入握手目标和服务器名称；每个入站仍独立生成密钥与 Short ID。新预设保存后可设为默认。</p>
      <fieldset :disabled="saving || loading" class="space-y-5">
        <p v-if="!items.length" class="text-xs text-gray-500">尚无系统预设，编辑器使用内置的 5 个常用目标。</p>
        <div v-for="(item, index) in items" :key="index" class="space-y-3 border-t border-ios-separator pt-4">
          <div class="flex items-center justify-between gap-3">
            <label v-if="item.id" class="flex items-center gap-2"><input v-model="defaultId" type="radio" name="reality-default" :value="item.id" />设为默认</label>
            <span v-else class="text-xs text-gray-500">新预设</span>
            <button type="button" class="ios-btn-secondary" @click="remove(index)">删除</button>
          </div>
          <label class="block">名称<input v-model="item.name" maxlength="64" class="form-input-base mt-2" /></label>
          <label class="block">握手目标 (host:port)<input v-model="item.handshakeTarget" placeholder="www.amd.com:443" class="form-input-base mt-2 font-mono" /></label>
          <label class="block">服务器名称（每行一个，留空取握手目标主机）<textarea :value="item.serverNames.join('\n')" rows="2" class="form-input-base mt-2 font-mono" @input="item.serverNames = ($event.target as HTMLTextAreaElement).value.split(/\r?\n/).filter(name => name !== '')" /></label>
          <label class="block">默认传输与 Flow（可选）
            <select :value="item.transport ? item.flow ? 'vision' : item.transport : ''" class="form-input-base mt-2" @change="setTransport(item, ($event.target as HTMLSelectElement).value)">
              <option value="">保留编辑器选择</option><option value="tcp">TCP</option><option value="vision">TCP（Vision）</option><option value="xhttp">xhttp</option>
            </select>
          </label>
        </div>
        <div class="flex flex-wrap gap-3">
          <button v-if="items.length < 20" type="button" class="ios-btn-secondary" @click="add">添加预设</button>
          <button v-if="defaultId" type="button" class="ios-btn-secondary" @click="defaultId = ''">取消默认</button>
        </div>
      </fieldset>
      <p v-if="formError" role="alert" class="text-ios-red">{{ formError }}</p>
      <div class="flex flex-wrap justify-between items-center gap-3">
        <span class="text-xs text-gray-500">{{ saved.revision ? `版本 ${saved.revision} · ${saved.updatedBy} · ${formatTime(saved.updatedAt)}` : '当前未设置' }}</span>
        <button class="ios-btn-primary" :disabled="loading || saving" @click="save">{{ saving ? '保存中…' : '保存 Reality 预设' }}</button>
      </div>
    </template>
  </section>
</template>
<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { APIError } from '../../../app/apiClient';
import { showToast } from '../../../app/api';
import { formatTime } from '../../../app/auth';
import ErrorState from '../../../components/ErrorState.vue';
import LoadingSkeleton from '../../../components/LoadingSkeleton.vue';
import { settingsAPI, type RealityPreset, type RealityPresets } from '../settingsApi';
import { loadRealityPresets, rememberRealityPresets } from '../realityPresets';

const saved = ref<RealityPresets | null>(null), items = ref<RealityPreset[]>([]), defaultId = ref('');
const loading = ref(false), saving = ref(false), error = ref(''), formError = ref('');
function fill(value: RealityPresets) {
  saved.value = value;
  items.value = value.items.map(item => ({ ...item, serverNames: [...item.serverNames] }));
  defaultId.value = value.defaultId;
  rememberRealityPresets(value);
}
async function load(refresh = false) {
  if (loading.value || saving.value) return;
  loading.value = true;
  try {
    const value = refresh ? await settingsAPI.realityPresets() : await loadRealityPresets();
    if (!value) throw new Error('读取 Reality 预设失败，请刷新重试');
    fill(value); error.value = ''; formError.value = '';
  } catch (e) { error.value = (e as Error).message; }
  finally { loading.value = false; }
}
function add() { items.value.push({ id: '', name: '', handshakeTarget: '', serverNames: [], transport: '', flow: '' }); }
function remove(index: number) {
  if (items.value[index]!.id === defaultId.value) defaultId.value = '';
  items.value.splice(index, 1);
}
function setTransport(item: RealityPreset, value: string) {
  item.transport = value === 'vision' ? 'tcp' : value as RealityPreset['transport'];
  item.flow = value === 'vision' ? 'xtls-rprx-vision' : '';
}
async function save() {
  if (!saved.value || saving.value) return;
  saving.value = true;
  try { fill(await settingsAPI.saveRealityPresets(items.value, defaultId.value, saved.value.revision)); formError.value = ''; showToast('Reality 预设已保存'); }
  catch (e) { formError.value = e instanceof APIError && e.code === 'settings_conflict' ? '设置已被其他会话修改，请刷新后重试' : (e as Error).message; }
  finally { saving.value = false; }
}
onMounted(() => load());
</script>
