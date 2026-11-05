<script setup lang="ts">
import { onMounted } from 'vue'
import { useDeploymentCapabilitiesStore } from '@/stores/deploymentCapabilities'
import LegacyOrganizations from './OrganizationList.vue'
import UserSpaces from './UserSpaces.vue'
const capabilities = useDeploymentCapabilitiesStore()
onMounted(() => capabilities.ensureLoaded())
</script>
<template>
  <UserSpaces v-if="capabilities.userSpaces" />
  <LegacyOrganizations v-else-if="capabilities.loaded" />
  <div v-else role="status" style="padding: 32px">正在加载共享空间…</div>
</template>
