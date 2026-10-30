<template>
  <form class="space-y-3" @submit.prevent="handleSubmit">
    <div :class="inline ? 'grid sm:grid-cols-2 gap-3' : 'space-y-3'">
      <label class="block text-xs text-ios-secondary">
        标签
        <input
          v-model.trim="form.label"
          class="form-input-base mt-1"
          required
          maxlength="120"
          placeholder="例如：电信回源 / 联通测速"
        />
      </label>
      <label class="block text-xs text-ios-secondary">
        主机 / IP
        <input
          v-model.trim="form.host"
          class="form-input-base mt-1"
          required
          placeholder="域名或 IPv4/IPv6"
        />
      </label>
      <label class="block text-xs text-ios-secondary">
        端口
        <input
          v-model.number="form.port"
          type="number"
          min="1"
          max="65535"
          class="form-input-base mt-1"
          required
        />
      </label>
      <div class="flex flex-col justify-center space-y-2 pt-2">
        <label class="flex items-center gap-2 text-xs text-ios-secondary cursor-pointer">
          <input v-model="form.allowNonPublic" type="checkbox" />
          明确允许非公网地址
        </label>
        <label v-if="showScheduled" class="flex items-center gap-2 text-xs text-ios-secondary cursor-pointer">
          <input v-model="form.scheduled" type="checkbox" />
          设为系统默认目标（最多 5 个）
        </label>
      </div>
    </div>
    <p v-if="localError" class="text-xs text-ios-red" role="alert">{{ localError }}</p>
    <div class="flex items-center gap-2 pt-1">
      <button type="submit" class="ios-btn-primary" :disabled="busy">
        {{ submitText || '添加目标' }}
      </button>
      <button v-if="showCancel" type="button" class="ios-btn-secondary" :disabled="busy" @click="$emit('cancel')">
        {{ cancelText || '取消' }}
      </button>
    </div>
  </form>
</template>
<script setup lang="ts">
import { reactive, ref, watch } from 'vue';
import type { ProbeTarget } from '../api';

const props = withDefaults(defineProps<{
  modelValue?: Partial<ProbeTarget> | null;
  busy?: boolean;
  submitText?: string;
  showCancel?: boolean;
  cancelText?: string;
  showScheduled?: boolean;
  inline?: boolean;
}>(), {
  modelValue: null,
  busy: false,
  submitText: '添加目标',
  showCancel: false,
  cancelText: '取消',
  showScheduled: false,
  inline: false,
});

const emit = defineEmits<{
  submit: [target: { label: string; host: string; port: number; allowNonPublic: boolean; scheduled: boolean }];
  cancel: [];
}>();

const form = reactive({
  label: '',
  host: '',
  port: 443,
  allowNonPublic: false,
  scheduled: false,
});
const localError = ref('');

watch(() => props.modelValue, (val) => {
  if (val) {
    form.label = val.label ?? '';
    form.host = val.host ?? '';
    form.port = typeof val.port === 'number' && val.port >= 1 ? val.port : 443;
    form.allowNonPublic = !!val.allowNonPublic;
    form.scheduled = !!val.scheduled;
  } else {
    reset();
  }
}, { immediate: true });

function reset() {
  form.label = '';
  form.host = '';
  form.port = 443;
  form.allowNonPublic = false;
  form.scheduled = false;
  localError.value = '';
}

function handleSubmit() {
  localError.value = '';
  const label = form.label.trim();
  const host = form.host.trim();
  const port = form.port;

  if (!label) {
    localError.value = '请填写目标标签';
    return;
  }
  if (label.length > 120) {
    localError.value = '目标标签长度不能超过 120 个字符';
    return;
  }
  if (!host) {
    localError.value = '请填写主机名或 IP 地址';
    return;
  }
  if (/[\/\\@?#%\s]/.test(host)) {
    localError.value = '主机名或 IP 地址包含非法字符';
    return;
  }
  if (!Number.isInteger(port) || port < 1 || port > 65535) {
    localError.value = '端口需为 1～65535 的整数';
    return;
  }

  emit('submit', {
    label,
    host,
    port,
    allowNonPublic: form.allowNonPublic,
    scheduled: form.scheduled,
  });
}
</script>
