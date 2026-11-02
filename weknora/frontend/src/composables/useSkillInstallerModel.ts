import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import { getAgentById, updateAgent, type CustomAgent } from '@/api/agent'
import { useDeploymentCapabilitiesStore } from '@/stores/deploymentCapabilities'

const INSTALLER_AGENT_ID = 'builtin-skill-installer'
const LAST_CHAT_MODEL_KEY = 'weknora_last_chat_model_id'

function readLastChatModelID(): string {
  try {
    return localStorage.getItem(LAST_CHAT_MODEL_KEY) || ''
  } catch {
    return ''
  }
}

/** Each panel keeps its own draft while persisting to the same installer agent. */
export function useSkillInstallerModel() {
  const { t } = useI18n()
  const deployment = useDeploymentCapabilitiesStore()
  const managedInstaller = computed(() => deployment.managedRuntime)
  const installerAgent = ref<Partial<CustomAgent> | null>(null)
  const installerModelId = ref('')
  const savingInstallerModel = ref(false)

  async function loadInstallerModel() {
    try {
      await deployment.ensureLoaded()
      const res = await getAgentById(INSTALLER_AGENT_ID)
      installerAgent.value = res?.data || null
      if (managedInstaller.value) { installerModelId.value = ''; return }
      const configured = installerAgent.value?.config?.model_id?.trim() || ''
      installerModelId.value = configured || readLastChatModelID()
    } catch {
      installerAgent.value = null
      installerModelId.value = managedInstaller.value ? '' : readLastChatModelID()
    }
  }

  async function persistInstallerModel(modelId: string) {
    await deployment.ensureLoaded()
    if (deployment.loadError) throw new Error(t('systemAgents.loadFailed'))
    if (managedInstaller.value) {
      // Internal installers inherit the same system model; never write a
      // tenant override or recreate a removed global workflow.
      const res = await getAgentById(INSTALLER_AGENT_ID)
      if (!res?.data) throw new Error(t('systemAgents.removedNotice'))
      installerAgent.value = res.data
      installerModelId.value = ''
      return
    }
    const id = modelId.trim()
    if (!id) throw new Error(t('settings.sandbox.skillInstallerModelRequired'))
    const current = installerAgent.value
    const config = { ...(current?.config || {}), model_id: id }
    const res = await updateAgent(INSTALLER_AGENT_ID, {
      name: current?.name || '',
      description: current?.description || '',
      avatar: current?.avatar || '',
      config,
    })
    installerAgent.value = res?.data || { ...current, config }
    installerModelId.value = id
  }

  async function onInstallerModelChange(modelId: string) {
    if (!modelId || modelId === '__add_model__') return
    installerModelId.value = modelId
    savingInstallerModel.value = true
    try {
      await persistInstallerModel(modelId)
    } catch (error: any) {
      MessagePlugin.error(error?.message || t('settings.sandbox.skillInstallerModelSaveFailed'))
    } finally {
      savingInstallerModel.value = false
    }
  }

  function resetInstallerModel() {
    installerAgent.value = null
    installerModelId.value = ''
  }

  return {
    installerModelId, managedInstaller, savingInstallerModel, loadInstallerModel, persistInstallerModel,
    onInstallerModelChange, resetInstallerModel,
  }
}
