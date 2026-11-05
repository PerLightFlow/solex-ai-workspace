import { onMounted, onUnmounted, ref, watch, type Ref } from 'vue'
import { get } from '@/utils/request'
import { useDeploymentCapabilitiesStore } from '@/stores/deploymentCapabilities'
import { useAuthStore } from '@/stores/auth'

interface SpacePermission {
  read: boolean
  edit: boolean
  manage: boolean
  download: boolean
}

// Physical tenant/creator identity is not ownership after a team handover.
export function useSpacePermission(kbId: Readonly<Ref<string>>) {
  const deployment = useDeploymentCapabilitiesStore()
  const auth = useAuthStore()
  const permission = ref<SpacePermission | null>(null)
  let generation = 0
  const refresh = async () => {
    const current = ++generation
    permission.value = null
    if (!deployment.userSpaces || !kbId.value || !auth.user?.id) return
    try {
      const response = await get(`/api/v1/spaces/knowledge-bases/${encodeURIComponent(kbId.value)}/permission`)
      if (current === generation) permission.value = response.data
    } catch {
      // API authorization remains authoritative; unavailable rights fail closed.
    }
  }
  watch([kbId, () => deployment.userSpaces, () => auth.user?.id], refresh, { immediate: true })
  onMounted(() => window.addEventListener('focus', refresh))
  onUnmounted(() => { generation++; window.removeEventListener('focus', refresh) })
  return permission
}
