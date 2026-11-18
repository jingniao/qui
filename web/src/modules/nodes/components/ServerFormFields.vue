<template>
  <div class="space-y-4 text-sm">
    <label class="block">
      <span class="text-xs font-medium text-neutral-600">服务器名称</span>
      <input v-model="form.name" maxlength="40" class="form-input-base mt-1" />
    </label>
    <div class="grid grid-cols-1 sm:grid-cols-2 gap-3 items-start">
      <label class="block min-w-0">
        <span class="text-xs font-medium text-neutral-600" title="优先采用此地区展示国旗，留空则根据 IP 归属自动识别">地区</span>
        <input v-model="form.region" placeholder="例如：香港" class="form-input-base mt-1" />
      </label>
      <div class="min-w-0">
        <span class="text-xs font-medium text-neutral-600" title="用于服务器列表筛选，最多 10 个">服务器分组</span>
        <input v-model="tagInput" list="server-tag-suggestions" placeholder="输入分组后回车" class="form-input-base mt-1" @keydown.enter.prevent="commitTag" />
        <div v-if="form.tags.length" class="flex flex-wrap gap-1.5 mt-1.5">
          <span v-for="item in form.tags" :key="item" class="inline-flex items-center gap-1 rounded-lg bg-ios-blue/[0.08] px-2 py-0.5 text-xs font-semibold text-ios-blue">
            {{ item }}
            <button type="button" class="text-ios-blue/70 hover:text-ios-blue" :aria-label="`删除标签 ${item}`" @click="form.tags = form.tags.filter(tag => tag !== item)">×</button>
          </span>
        </div>
        <datalist id="server-tag-suggestions">
          <option v-for="item in knownTags.filter(tag => !form.tags.includes(tag))" :key="item" :value="item" />
        </datalist>
        <p v-if="tagError" class="text-xs text-ios-red mt-1">{{ tagError }}</p>
      </div>
    </div>
    <label class="block">
      <span class="text-xs font-medium text-neutral-600">服务器备注</span>
      <textarea v-model="form.remark" rows="2" placeholder="填写服务器规格等备注信息" class="form-input-base mt-1"></textarea>
    </label>

    <div class="grid grid-cols-1 sm:grid-cols-2 gap-3 items-start">
      <label class="block min-w-0">
        <span class="text-xs font-medium text-ios-secondary">客户端连接地址</span>
        <input v-model.trim="connection.connectHost" class="form-input-base mt-1" placeholder="域名或 IP，留空自动" />
      </label>
      <label class="block min-w-0">
        <span class="text-xs font-medium text-ios-secondary" title="默认端口分配范围（1～65535），留空清除">默认端口范围</span>
        <input v-model.trim="connection.portRange" class="form-input-base mt-1" placeholder="例如：35000-40000" />
      </label>
    </div>

    <div class="space-y-3">
      <div class="grid grid-cols-1 sm:grid-cols-3 gap-3 items-start">
        <label class="block min-w-0">
          <span class="text-xs font-medium text-neutral-600">续费到期日</span>
          <input v-model="form.renewDate" type="date" class="form-input-base mt-1 h-10 min-w-0 font-sans font-normal" />
        </label>

        <div class="min-w-0 sm:col-span-2">
          <span class="text-xs font-medium text-neutral-600">续费价格与周期</span>
          <div class="flex items-center h-10 mt-1 rounded-xl border border-black/15 bg-white shadow-2xs focus-within:ring-2 focus-within:ring-ios-blue/20 focus-within:border-ios-blue transition-all">
            <AppSelect
              v-model="form.renewCurrency"
              :options="currencyOptions"
              teleport
              :max-height="360"
              class="h-full w-24 shrink-0 border-r border-black/10"
              title="续费币种"
              trigger-class="h-full !px-2 !py-0 !border-0 !rounded-r-none !bg-neutral-50 !shadow-none !ring-0 [&>div]:!pr-0 [&>svg]:!ml-0 [&_span]:!font-normal [&_span]:!text-ios-label"
              menu-class="!min-w-36 !text-sm [&_span]:!font-normal"
            />
            <input
              v-model="form.renewPrice"
              type="number"
              step="any"
              min="0"
              placeholder="金额"
              class="flex-1 min-w-0 px-2 py-2 font-sans font-normal text-ios-label placeholder:text-ios-quaternary focus:outline-none bg-transparent text-sm"
            />
            <AppSelect
              v-model="form.renewCycle"
              :options="cycleOptions"
              teleport
              :max-height="360"
              align="right"
              class="h-full w-20 shrink-0 border-l border-black/10"
              title="续费周期"
              trigger-class="h-full !px-2 !py-0 !border-0 !rounded-l-none !bg-neutral-50 !shadow-none !ring-0 [&>div]:!pr-0 [&>svg]:!ml-0 [&_span]:!font-normal [&_span]:!text-ios-label"
              menu-class="!min-w-36 !text-sm [&_span]:!font-normal"
            />
          </div>
        </div>
      </div>
    </div>

    <div v-if="traffic" class="space-y-3">
      <div class="grid grid-cols-1 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_5rem] gap-3 items-start">
        <div class="min-w-0">
          <label for="server-traffic-allowance" class="text-xs font-medium text-ios-secondary" title="1 GB = 1,000,000,000 字节">流量限制（GB）</label>
          <input id="server-traffic-allowance" v-model.trim="traffic.allowanceValue" inputmode="decimal" class="form-input-base min-w-0 mt-1" placeholder="不设置" />
        </div>
        <div class="min-w-0">
          <span class="text-xs font-medium text-ios-secondary">月流量计算类型</span>
          <AppSelect
            v-model="traffic.method"
            :options="MACHINE_TRAFFIC_METHODS"
            teleport
            class="mt-1"
            title="月流量计算类型"
            trigger-class="h-10 !font-normal [&>div]:!pr-0 [&_span]:!font-normal [&_span]:!text-ios-label"
            menu-class="!min-w-56 !text-sm [&_span]:!font-normal"
          />
        </div>
        <label class="block min-w-0">
          <span class="text-xs font-medium text-ios-secondary" title="流量重置日（1～31）">流量重置日</span>
          <input v-model.number="traffic.resetDay" type="number" min="1" max="31" step="1" class="form-input-base mt-1" />
        </label>
      </div>
    </div>

    <div class="border-t border-black/[0.06] pt-3 space-y-2">
      <label class="flex items-center gap-2 cursor-pointer text-xs font-medium text-neutral-800">
        <input v-model="form.maintenance" type="checkbox" class="rounded border-black/20 text-ios-blue" />
        <span>设为维护模式（暂时排除在订阅之外）</span>
      </label>
    </div>

  </div>
</template>
<script setup lang="ts">
import { ref } from 'vue';
import AppSelect from '../../../components/AppSelect.vue';
import { CURRENCY_LIST, CYCLE_LIST } from '../nodeMeta';
import { MACHINE_TRAFFIC_METHODS, type MachineTrafficForm } from '../machineTrafficModel';
import { addTag } from '../serverOrder';
import { validConnectHost } from '../nodeAddress';
import type { ServerEditFormState } from './serverListModel';

const props = withDefaults(defineProps<{
  form: ServerEditFormState;
  connection: { connectHost: string; portRange: string };
  traffic: MachineTrafficForm | null;
  knownTags?: string[];
}>(), { knownTags: () => [] });
const currencyOptions = CURRENCY_LIST.map(item => ({ value: item.code, label: item.label }));
const cycleOptions = CYCLE_LIST.map(value => ({ value, label: value.replace(/^\//, '') }));
const tagInput = ref(''), tagError = ref('');
function commitTag() {
  const result = addTag(props.form.tags, tagInput.value);
  tagError.value = result.error || '';
  if (!result.error) { props.form.tags = result.tags; tagInput.value = ''; }
}
function connectionSettings() {
  const values = props.connection;
  if (!validConnectHost(values.connectHost)) {
    throw new Error('客户端连接地址需为域名或 IP，不能包含协议、端口或方括号');
  }
  const range = values.portRange.trim();
  const rangeMatch = range.match(/^(\d{1,5})\s*-\s*(\d{1,5})$/);
  if (range && !rangeMatch) {
    throw new Error('端口范围格式为 35000-40000，留空表示不设置');
  }
  const portRangeStart = rangeMatch ? Number(rangeMatch[1]) : null;
  const portRangeEnd = rangeMatch ? Number(rangeMatch[2]) : null;
  if (portRangeStart !== null && portRangeEnd !== null &&
      (portRangeStart < 1 || portRangeEnd > 65535 || portRangeStart > portRangeEnd)) {
    throw new Error('端口范围需在 1～65535 之间，起始端口不能大于结束端口');
  }
  return { portRangeStart, portRangeEnd };
}
defineExpose({ connectionSettings });
</script>
