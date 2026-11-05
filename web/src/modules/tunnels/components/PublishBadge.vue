<template><StatusBadge :status="publishStatuses[status].status" :label="publishStatuses[status].label" /></template>
<script setup lang="ts">
import StatusBadge from '../../../components/StatusBadge.vue';
import type { PublishStatus } from '../api';
import { publishStatuses } from '../model';
defineProps<{ status: PublishStatus }>();
</script>
