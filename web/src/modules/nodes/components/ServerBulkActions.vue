<template>
  <div :class="selected.length ? 'sticky bottom-4 z-20' : ''">
    <p v-if="result" role="status" class="text-sm text-ios-secondary panel-card p-4">{{ result }}</p>
    <div v-if="selected.length" class="panel-card p-4 flex flex-wrap items-center gap-3 shadow-ios-lg">
      <span class="text-sm text-ios-label">已选 {{ selected.length }} 台服务器</span>
      <button class="ios-btn-secondary" :disabled="busy" @click="start('add')">加入线路组</button>
      <button class="ios-btn-secondary" :disabled="busy" @click="start('remove')">移出线路组</button>
      <button class="ios-btn-secondary" :disabled="busy" @click="start('maintenance-on')">开启维护</button>
      <button class="ios-btn-secondary" :disabled="busy" @click="start('maintenance-off')">关闭维护</button>
      <button class="ios-btn-secondary" :disabled="busy" @click="$emit('clear')">取消选择</button>
    </div>
    <FormDrawer v-model="open" :title="titles[mode]" :loading="busy" :confirm-text="reviewing ? '确认执行' : '预览变更'" @confirm="reviewing ? execute() : prepare()">
      <div class="space-y-3 text-sm text-ios-secondary">
        <p>服务器：{{ targets.map(node => node.name).join('、') }}</p>
        <div v-if="!reviewing && isGroup" class="space-y-2">
          <p v-if="!groups.length">暂无线路组，请先在线路组页面创建。</p>
          <label v-for="group in groups" :key="group.id" class="flex items-center gap-2 cursor-pointer">
            <input v-model="picked" type="checkbox" :value="group.id" />{{ group.name }}{{ group.enabled ? '' : '（已禁用）' }}
          </label>
        </div>
        <ul v-if="reviewing" class="space-y-2">
          <li v-for="line in changes" :key="line">{{ line }}</li>
        </ul>
        <p v-if="isGroup">仅修改线路组成员，不发布配置；订阅在用户下次刷新时生效，仍按授权、线路组用途与可见性过滤。</p>
        <p v-else>维护模式会将服务器排除在后续订阅之外；既有客户端配置与连接不会自动撤销。</p>
        <p v-if="error" role="alert" class="text-ios-red">{{ error }}</p>
      </div>
    </FormDrawer>
  </div>
</template>
<script setup lang="ts">
import { computed, ref } from 'vue';
import { nodeAPI, type NodeView } from '../api';
import { groupAPI, type NodeGroup, type GroupInput } from '../groupsApi';
import { choices } from '../../proxy/api';
import { groupInput } from '../../proxy/lineGroups';
import FormDrawer from '../../../components/FormDrawer.vue';
const props = defineProps<{ selected: NodeView[]; groups: NodeGroup[] }>();
const emit = defineEmits<{ changed: []; clear: [] }>();
type Mode = 'add' | 'remove' | 'maintenance-on' | 'maintenance-off';
const titles = { add: '加入线路组', remove: '移出线路组', 'maintenance-on': '开启维护模式', 'maintenance-off': '关闭维护模式' };
const mode = ref<Mode>('add'), open = ref(false), busy = ref(false), reviewing = ref(false);
const picked = ref<string[]>([]), error = ref(''), result = ref(''), changes = ref<string[]>([]);
const targets = ref<NodeView[]>([]);
const plans = ref<{ id: string; name: string; before: GroupInput; after: GroupInput }[]>([]);
const isGroup = computed(() => mode.value === 'add' || mode.value === 'remove');
function start(value: Mode) {
  mode.value = value; targets.value = props.selected.filter(node => !node.deletedAt);
  picked.value = []; error.value = ''; reviewing.value = false; plans.value = []; changes.value = []; open.value = true;
}
async function prepare() {
  if (busy.value) return;
  error.value = ''; busy.value = true;
  try {
    if (isGroup.value) {
      if (!picked.value.length) throw new Error('请选择至少一个线路组');
      const fresh = await choices(groupAPI.list);
      const ids = targets.value.map(node => node.id);
      plans.value = picked.value.map(id => {
        const group = fresh.find(item => item.id === id);
        if (!group) throw new Error('线路组已发生变化，请关闭后刷新重试');
        const nodeIds = mode.value === 'add' ? [...new Set([...group.nodeIds, ...ids])] : group.nodeIds.filter(id => !ids.includes(id));
        return { id, name: group.name, before: groupInput(group), after: groupInput(group, nodeIds) };
      }).filter(plan => JSON.stringify(plan.before) !== JSON.stringify(plan.after));
      changes.value = plans.value.map(plan => {
        const affected = targets.value.filter(node => plan.before.nodeIds.includes(node.id) !== plan.after.nodeIds.includes(node.id));
        return `${plan.name}：${mode.value === 'add' ? '加入' : '移出'} ${affected.map(node => node.name).join('、')}（${plan.before.nodeIds.length} → ${plan.after.nodeIds.length} 台）`;
      });
    } else {
      const enabled = mode.value === 'maintenance-on';
      changes.value = targets.value.map(node => `${node.name}：${node.maintenance ? '维护中' : '非维护'} → ${enabled ? '维护中' : '非维护'}`);
    }
    if (!changes.value.length) throw new Error('所选服务器无需变更');
    reviewing.value = true;
  } catch (e) { error.value = (e as Error).message; }
  finally { busy.value = false; }
}
async function execute() {
  if (busy.value || !reviewing.value) return;
  busy.value = true; error.value = '';
  const failed: string[] = []; let succeeded = 0;
  try {
    if (isGroup.value) {
      // Recheck before full-record PUT; the API has no revision/ETag contract.
      const fresh = await choices(groupAPI.list);
      for (const plan of plans.value) {
        try {
          const group = fresh.find(item => item.id === plan.id);
          if (!group || JSON.stringify(groupInput(group)) !== JSON.stringify(plan.before)) throw new Error('线路组已变化，请刷新后重试');
          await groupAPI.save(plan.id, plan.after); succeeded++;
        } catch (e) { failed.push(`${plan.name}：${(e as Error).message}`); }
      }
    } else {
      for (const target of targets.value) {
        try {
          // Fetch the latest complete settings so unrelated fields are preserved.
          const node = await nodeAPI.get(target.id);
          if (node.deletedAt) throw new Error('服务器已删除');
          await nodeAPI.save({ ...node, maintenance: mode.value === 'maintenance-on' }); succeeded++;
        } catch (e) { failed.push(`${target.name}：${(e as Error).message}`); }
      }
    }
    result.value = `已完成 ${succeeded} 项。${failed.length ? `失败 ${failed.length} 项：${failed.join('；')}` : '全部成功。'}`;
    open.value = false; emit('clear');
  } catch (e) { error.value = (e as Error).message; }
  finally { busy.value = false; emit('changed'); }
}
</script>
