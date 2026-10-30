<template>
  <div :class="selected.length ? 'sticky bottom-4 z-20' : ''">
    <p v-if="result" role="status" class="text-sm text-ios-secondary panel-card p-4">{{ result }}</p>
    <div v-if="selected.length" class="panel-card p-4 flex flex-wrap items-center gap-3 shadow-ios-lg">
      <span class="text-sm text-ios-label">已选 {{ selected.length }} 台服务器</span>
      <button class="ios-btn-secondary" :disabled="busy" @click="start('add')">加入节点分组</button>
      <button class="ios-btn-secondary" :disabled="busy" @click="start('remove')">移出节点分组</button>
      <button class="ios-btn-secondary" :disabled="busy" @click="start('maintenance-on')">开启维护</button>
      <button class="ios-btn-secondary" :disabled="busy" @click="start('maintenance-off')">关闭维护</button>
      <button class="ios-btn-secondary" :disabled="busy" @click="$emit('clear')">取消选择</button>
    </div>
    <FormDrawer v-model="open" :title="titles[mode]" :loading="busy" :confirm-text="reviewing ? '确认执行' : '预览变更'" @confirm="reviewing ? execute() : prepare()">
      <div class="space-y-3 text-sm text-ios-secondary">
        <p>服务器：{{ targets.map(node => node.name).join('、') }}</p>
        <div v-if="!reviewing && isGroup" class="space-y-2">
          <p v-if="!serverGroups.length">暂无支持显式服务器成员的中转 / 出口分组。用户接入请在节点分组页选择具体入站。</p>
          <label v-for="group in serverGroups" :key="group.id" class="flex items-center gap-2 cursor-pointer">
            <input v-model="picked" type="checkbox" :value="group.id" />{{ group.name }}{{ group.enabled ? '' : '（已禁用）' }}
          </label>
        </div>
        <ul v-if="reviewing" class="space-y-2">
          <li v-for="line in changes" :key="line">{{ line }}</li>
        </ul>
        <p v-if="isGroup">仅修改中转 / 出口分组的显式服务器成员，不授予用户接入权限，不发布配置。</p>
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
const titles = { add: '加入节点分组', remove: '移出节点分组', 'maintenance-on': '开启维护模式', 'maintenance-off': '关闭维护模式' };
const mode = ref<Mode>('add'), open = ref(false), busy = ref(false), reviewing = ref(false);
const picked = ref<string[]>([]), error = ref(''), result = ref(''), changes = ref<string[]>([]);
const targets = ref<NodeView[]>([]);
const plans = ref<{ id: string; name: string; before: GroupInput; after: GroupInput }[]>([]);
const serverGroups = computed(() => props.groups.filter(g => (g.purpose === 'relay' || g.purpose === 'egress') && g.serverIds !== undefined));
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
      if (!picked.value.length) throw new Error('请选择至少一个节点分组');
      const fresh = await choices(groupAPI.list);
      const ids = targets.value.map(node => node.id);
      plans.value = picked.value.map(id => {
        const group = fresh.find(item => item.id === id);
        if (!group || group.serverIds === undefined || !['relay', 'egress'].includes(group.purpose)) throw new Error('分组不支持显式服务器成员，请刷新重试');
        const serverIds = mode.value === 'add' ? [...new Set([...group.serverIds, ...ids])] : group.serverIds.filter(id => !ids.includes(id));
        return { id, name: group.name, before: groupInput(group), after: { ...groupInput(group), serverIds } };
      }).filter(plan => JSON.stringify(plan.before) !== JSON.stringify(plan.after));
      changes.value = plans.value.map(plan => {
        return `${plan.name}：显式服务器成员 ${plan.before.serverIds?.length || 0} → ${plan.after.serverIds?.length || 0} 台（不授予用户接入）`; 
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
          if (!group || JSON.stringify(groupInput(group)) !== JSON.stringify(plan.before)) throw new Error('节点分组已变化，请刷新后重试');
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
