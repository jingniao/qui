<template>
  <section class="panel-card p-5 space-y-3">
    <div class="flex justify-between gap-3"><h2 class="font-semibold text-ios-label">节点</h2><div class="flex gap-3"><button class="ios-action-btn-secondary" @click="$emit('manage')">管理</button><button v-if="isOwner && !node.deletedAt" class="ios-action-btn-primary" @click="$emit('create')">+ 新建节点</button></div></div>
    <p class="text-sm text-ios-secondary">{{ error || (isOwner ? summary : '当前权限无法读取节点配置') }}</p>
  </section>
</template>
<script setup lang="ts">
import { computed, ref, watch, onUnmounted } from 'vue';
import type { NodeView } from '../../api';
import { proxyAPI, choices, type Inbound } from '../../../proxy/api';
import { isOwner } from '../../../../app/auth';
const props = defineProps<{ node: NodeView }>();
defineEmits<{ manage: []; create: [] }>();
const items = ref<Inbound[]>([]), error = ref(''), loading = ref(true);
const summary = computed(() => loading.value ? '正在读取节点…' : items.value.length + ' 个节点' + items.value.map(item => ' · ' + item.protocol.toUpperCase() + ':' + item.port).join(''));
let generation = 0;
watch(() => props.node.id, async id => { const current = ++generation; if (!isOwner.value) return; try { const result = await choices((p, s) => proxyAPI.inbounds(id, p, s)); if (generation === current) items.value = result; } catch (e) { if (generation === current) error.value = (e as Error).message; } finally { if (generation === current) loading.value = false; } }, { immediate: true });
onUnmounted(() => { generation++; });
</script>
