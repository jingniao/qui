<template>
  <section class="panel-card p-6 max-w-3xl space-y-5 text-sm">
    <div class="flex flex-wrap justify-between items-center gap-3"><h2 class="font-semibold">自动备份与保留轮换</h2><button :disabled="loading || saving" class="ios-btn-secondary" @click="load">刷新已保存策略</button></div>
    <ErrorState v-if="error" :message="error" @retry="load" /><LoadingSkeleton v-else-if="!saved" />
    <template v-else>
      <label class="flex items-center gap-2"><input v-model="form.enabled" type="checkbox" /> 启用自动加密备份</label>
      <label class="block">每日执行时间<input v-model="form.time" type="time" class="form-input-base mt-2" /></label>
      <p class="text-xs text-gray-500">按系统时区 {{ saved.timezone }} 执行。启动时会补做当前日期、当前周和当前月尚未完成的备份，不补造停机期间的历史快照。</p>
      <div class="grid md:grid-cols-3 gap-4"><label>每日备份保留（份）<input v-model.number="form.dailyKeep" type="number" min="1" max="365" step="1" class="form-input-base mt-2" /></label><label>每周备份保留（份）<input v-model.number="form.weeklyKeep" type="number" min="1" max="365" step="1" class="form-input-base mt-2" /></label><label>每月备份保留（份）<input v-model.number="form.monthlyKeep" type="number" min="1" max="365" step="1" class="form-input-base mt-2" /></label></div>
      <label class="block">{{ saved.passwordConfigured ? '更换自动备份密码（留空保留现有密码）' : '设置自动备份密码' }}<input v-model="password" type="password" autocomplete="new-password" maxlength="256" class="form-input-base mt-2" /></label>
      <label v-if="password" class="block">再次输入密码<input v-model="passwordConfirm" type="password" autocomplete="new-password" maxlength="256" class="form-input-base mt-2" /></label>
      <p class="text-xs text-gray-500">后台执行所需密码会加密保存，接口只显示是否已配置。请单独保管密码；更换密码只影响新备份，旧文件仍需原密码。手动备份继续使用各自创建时的密码。</p>
      <p class="text-xs text-gray-500">默认保留 7 份每日、4 份每周、3 份每月备份。成功创建并确认新文件后，才清理对应类别的超额自动备份；手动备份单独保留。</p>
      <p v-if="changedElsewhere" class="text-amber-700 text-xs">已收到备份设置或系统时区的更新提示。当前输入保留，请核对并刷新后再提交。</p>
      <div class="space-y-3"><button :disabled="loading || saving || previewLoading" class="ios-btn-secondary" @click="readPreview">{{ previewLoading ? '读取影响中…' : '预览保留影响' }}</button><p v-if="previewError" role="alert" class="text-red-600 text-xs">{{ previewError }}</p><div v-if="preview" class="rounded-xl border border-gray-200 p-4 space-y-3"><p class="font-semibold">{{ preview.policy.enabled ? `候选清理 ${preview.candidates} 份，登记大小合计 ${size(preview.candidateBytes)}` : '自动备份停用后，不执行自动轮换' }}</p><p class="text-xs text-gray-500">依据 {{ formatTime(preview.observedAt) }} 的登记清单计算，不读取或删除文件。实际轮换还会复核最新归档及配置密钥，手动和导入文件不在候选范围。</p><table class="w-full text-xs text-left"><thead><tr><th class="py-2">类别</th><th>现有</th><th>保留</th><th>候选清理</th><th>登记大小</th></tr></thead><tbody><tr v-for="category in preview.categories" :key="category.kind"><td class="py-2">{{ kindNames[category.kind] }}</td><td>{{ category.existing }}</td><td>{{ category.retained }}</td><td>{{ category.candidates }}</td><td>{{ size(category.candidateBytes) }}</td></tr></tbody></table><template v-for="category in preview.categories" :key="category.kind"><details v-if="category.sample.length" class="text-xs"><summary class="cursor-pointer">{{ kindNames[category.kind] }}候选文件{{ category.sampleTruncated ? '（仅展示前 10 份）' : '' }}</summary><ul class="mt-2 space-y-1"><li v-for="file in category.sample" :key="file.id"><span class="font-mono break-all">{{ file.id }}</span> · {{ formatTime(file.createdAt) }} · {{ size(file.fileBytes) }}</li></ul></details></template></div></div>
      <div class="flex flex-wrap justify-between items-center gap-3"><span class="text-xs text-gray-500">{{ saved.revision ? `策略版本 ${saved.revision} · ${saved.updatedBy} · ${formatTime(saved.updatedAt)}` : '当前使用默认策略，尚未启用' }}</span><button :disabled="loading || saving || previewLoading" class="ios-btn-primary" @click="confirmSave">{{ saving ? '保存中…' : '保存自动备份策略' }}</button></div>
      <router-link to="/system/backups" class="ios-action-btn-primary">查看备份文件与执行记录</router-link>
    </template>
    <ConfirmDialog v-model="confirmation" title="确认自动备份策略" :message="`${form.enabled ? '启用' : '停用'}自动备份，每日 ${form.time}，保留每日 ${form.dailyKeep} 份、每周 ${form.weeklyKeep} 份、每月 ${form.monthlyKeep} 份。当前登记候选 ${preview?.candidates || 0} 份、${size(preview?.candidateBytes || 0)}。`" warning-details="候选按登记清单计算，实际轮换会重新核对。更换密码后，旧备份仍需原密码。" confirm-text="确认保存" :is-danger="shortening" @confirm="save" />
  </section>
</template>
<script setup lang="ts">
import { computed, reactive, ref, watch, onMounted, onUnmounted } from 'vue';
import { settingsAPI, type BackupSchedule, type BackupRetentionPreview } from '../settingsApi';
import { formatTime } from '../../../app/auth';
import { showToast } from '../../../app/api';
import ErrorState from '../../../components/ErrorState.vue';
import LoadingSkeleton from '../../../components/LoadingSkeleton.vue';
import ConfirmDialog from '../../../components/ConfirmDialog.vue';
const saved = ref<BackupSchedule | null>(null), loading = ref(false), saving = ref(false), error = ref(''), confirmation = ref(false), password = ref(''), passwordConfirm = ref(''), changedElsewhere = ref(false);
const form = reactive({ enabled: false, time: '03:00', dailyKeep: 7, weeklyKeep: 4, monthlyKeep: 3 }); let generation = 0;
const preview = ref<BackupRetentionPreview | null>(null), previewLoading = ref(false), previewError = ref(''); let previewGeneration = 0;
const kindNames = {daily:'每日',weekly:'每周',monthly:'每月'};
function size(bytes:number) { return bytes >= 1024*1024*1024 ? `${(bytes/(1024*1024*1024)).toFixed(2)} GiB` : bytes >= 1024*1024 ? `${(bytes/(1024*1024)).toFixed(2)} MiB` : `${bytes.toLocaleString()} B`; }
function clearPreview() { previewGeneration++; preview.value = null; previewLoading.value = false; previewError.value = ''; confirmation.value = false; }
function validPolicy() { if (!/^([01]\d|2[0-3]):[0-5]\d$/.test(form.time) || [form.dailyKeep,form.weeklyKeep,form.monthlyKeep].some(value=>!Number.isInteger(value)||value<1||value>365)) { showToast('请填写有效时间及 1～365 的保留数量','error'); return false; } return true; }
async function readPreview() {
  if (!saved.value || saving.value || previewLoading.value || !validPolicy()) return false;
  const serial = ++previewGeneration, revision = saved.value.revision, policy = {...form}, key = JSON.stringify(policy);
  previewLoading.value = true; preview.value = null; previewError.value = '';
  try {
    const result = await settingsAPI.backupRetentionPreview(policy,revision);
    if (serial!==previewGeneration || saved.value?.revision!==revision || JSON.stringify(form)!==key) return false;
    preview.value = result; return true;
  } catch (e) { if (serial===previewGeneration) previewError.value = (e as Error).message; return false; }
  finally { if (serial===previewGeneration) previewLoading.value = false; }
}
watch(form,clearPreview,{deep:true,flush:'sync'});
const dirty = computed(() => !!password.value || !!saved.value && JSON.stringify(form) !== JSON.stringify(saved.value.policy));
const shortening = computed(() => !!saved.value && (form.dailyKeep < saved.value.policy.dailyKeep || form.weeklyKeep < saved.value.policy.weeklyKeep || form.monthlyKeep < saved.value.policy.monthlyKeep));
async function load() { if (saving.value) return; clearPreview(); const serial = ++generation; loading.value = true; try { const result = await settingsAPI.backupSchedule(); if (serial !== generation) return; saved.value = result; Object.assign(form, result.policy); password.value = ''; passwordConfirm.value = ''; changedElsewhere.value = false; error.value = ''; } catch (e) { if (serial === generation) error.value = (e as Error).message; } finally { if (serial === generation) loading.value = false; } }
async function confirmSave() {
  if (!saved.value || saving.value) return;
  if (!validPolicy()) return;
  if (password.value && (Array.from(password.value).length < 12 || Array.from(password.value).length > 128 || password.value !== passwordConfirm.value)) { showToast('请输入 12～128 个字符的密码，并确认两次输入一致', 'error'); return; }
  if (form.enabled && !saved.value.passwordConfigured && !password.value) { showToast('启用前请设置自动备份密码', 'error'); return; }
  if (await readPreview()) confirmation.value = true;
}
async function save() { if (!saved.value || saving.value) return; saving.value = true; try { const result = await settingsAPI.saveBackupSchedule({ ...form }, saved.value.revision, password.value); saved.value = result; Object.assign(form, result.policy); clearPreview(); password.value = ''; passwordConfirm.value = ''; changedElsewhere.value = false; error.value = ''; showToast('自动备份策略已保存，后台将按实际执行结果更新记录'); } catch (e) { showToast((e as Error).message, 'error'); } finally { saving.value = false; } }
function refreshed() { clearPreview(); if (dirty.value || saving.value) { changedElsewhere.value = true; return; } void load(); }
onMounted(() => { void load(); window.addEventListener('proxy-backup-status', refreshed); window.addEventListener('proxy-system-settings', refreshed); });
onUnmounted(() => { generation++; clearPreview(); password.value = ''; passwordConfirm.value = ''; window.removeEventListener('proxy-backup-status', refreshed); window.removeEventListener('proxy-system-settings', refreshed); });
</script>
