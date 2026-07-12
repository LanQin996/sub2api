<template>
  <BaseDialog
    :show="show"
    :title="t('admin.accounts.dataImportTitle')"
    width="normal"
    close-on-click-outside
    @close="handleClose"
  >
    <form id="import-data-form" class="space-y-4" @submit.prevent="handleImport">
      <div class="text-sm text-gray-600 dark:text-dark-300">
        {{ t('admin.accounts.dataImportHint') }}
      </div>
      <div
        class="rounded-lg border border-amber-200 bg-amber-50 p-3 text-xs text-amber-600 dark:border-amber-800 dark:bg-amber-900/20 dark:text-amber-400"
      >
        {{ t('admin.accounts.dataImportWarning') }}
      </div>

      <div>
        <label class="input-label">{{ t('admin.accounts.dataImportFile') }}</label>
        <div
          class="flex items-center justify-between gap-3 rounded-lg border border-dashed px-4 py-3 transition-colors"
          :class="dragActive
            ? 'border-primary-400 bg-primary-50/70 dark:border-primary-500 dark:bg-primary-900/20'
            : 'border-gray-300 bg-gray-50 dark:border-dark-600 dark:bg-dark-800'"
          @dragenter.prevent="handleDragEnter"
          @dragover.prevent
          @dragleave.prevent="handleDragLeave"
          @drop.prevent="handleDrop"
        >
          <div class="min-w-0">
            <div class="truncate text-sm text-gray-700 dark:text-dark-200" :title="fileListTitle">
              {{ selectedFilesLabel || t('admin.accounts.dataImportSelectFile') }}
            </div>
            <div class="text-xs text-gray-500 dark:text-dark-400">
              JSON (.json)
              <span v-if="files.length > 1"> · {{ fileListTitle }}</span>
            </div>
            <div class="text-xs text-gray-500 dark:text-dark-400">
              {{ t('admin.accounts.dataImportFileHint') }}
            </div>
          </div>
          <button type="button" class="btn btn-secondary shrink-0" @click="openFilePicker">
            {{ t('common.chooseFile') }}
          </button>
        </div>
        <input
          ref="fileInput"
          type="file"
          class="hidden"
          accept="application/json,.json"
          multiple
          @change="handleFileChange"
        />
      </div>

      <fieldset
        data-testid="import-groups"
        :disabled="importing"
        class="m-0 min-w-0 border-0 p-0 transition-opacity"
        :class="importing ? 'pointer-events-none opacity-60' : ''"
      >
        <GroupSelector v-model="groupIds" :groups="groups" />
      </fieldset>

      <div
        v-if="result"
        class="space-y-2 rounded-xl border border-gray-200 p-4 dark:border-dark-700"
      >
        <div class="text-sm font-medium text-gray-900 dark:text-white">
          {{ t('admin.accounts.dataImportResult') }}
        </div>
        <div class="text-sm text-gray-700 dark:text-dark-300">
          {{ t('admin.accounts.dataImportResultSummary', result) }}
        </div>

        <div v-if="errorItems.length" class="mt-2">
          <div class="text-sm font-medium text-red-600 dark:text-red-400">
            {{ t('admin.accounts.dataImportErrors') }}
          </div>
          <div
            class="mt-2 max-h-48 overflow-auto rounded-lg bg-gray-50 p-3 font-mono text-xs dark:bg-dark-800"
          >
            <div v-for="(item, idx) in errorItems" :key="idx" class="whitespace-pre-wrap">
              {{ item.kind }} {{ item.name || item.proxy_key || '-' }} — {{ item.message }}
            </div>
          </div>
        </div>
      </div>
    </form>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button class="btn btn-secondary" type="button" :disabled="importing" @click="handleClose">
          {{ t('common.cancel') }}
        </button>
        <button
          class="btn btn-primary"
          type="submit"
          form="import-data-form"
          :disabled="importing"
        >
          {{ importing ? t('admin.accounts.dataImporting') : t('admin.accounts.dataImportButton') }}
        </button>
      </div>
    </template>
  </BaseDialog>

  <ConfirmDialog
    :show="showMixedChannelConfirm"
    :title="t('admin.accounts.dataImportMixedChannelWarningTitle')"
    :message="t('admin.accounts.dataImportMixedChannelWarning')"
    :confirm-text="t('admin.accounts.dataImportMixedChannelConfirm')"
    :cancel-text="t('common.cancel')"
    :danger="true"
    @confirm="handleMixedChannelConfirm"
    @cancel="clearMixedChannelConfirmation"
  />
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import GroupSelector from '@/components/common/GroupSelector.vue'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import type { AdminDataImportResult, AdminDataPayload, AdminGroup } from '@/types'

interface Props {
  show: boolean
  groups: AdminGroup[]
}

interface Emits {
  (e: 'close'): void
  (e: 'imported'): void
}

const props = defineProps<Props>()
const emit = defineEmits<Emits>()

const { t } = useI18n()
const appStore = useAppStore()

const importing = ref(false)
const files = ref<File[]>([])
const groupIds = ref<number[]>([])
const dragDepth = ref(0)
const dragActive = computed(() => dragDepth.value > 0)
const hasCreatedData = ref(false)
const result = ref<AdminDataImportResult | null>(null)
const pendingMixedChannelImport = ref<{
  data: AdminDataPayload
  groupIDs: number[]
} | null>(null)
const showMixedChannelConfirm = computed(() => pendingMixedChannelImport.value !== null)

const fileInput = ref<HTMLInputElement | null>(null)
const selectedFilesLabel = computed(() => {
  if (files.value.length === 0) return ''
  if (files.value.length === 1) return files.value[0]?.name || ''
  return t('admin.accounts.selectedCount', { count: files.value.length })
})
const fileListTitle = computed(() => files.value.map((item) => item.name).join(', '))

const errorItems = computed(() => result.value?.errors || [])

const clearMixedChannelConfirmation = () => {
  pendingMixedChannelImport.value = null
}

watch(
  () => props.show,
  (open) => {
    clearMixedChannelConfirmation()
    if (open) {
      files.value = []
      groupIds.value = []
      dragDepth.value = 0
      hasCreatedData.value = false
      result.value = null
      if (fileInput.value) {
        fileInput.value.value = ''
      }
    }
  }
)

watch(groupIds, clearMixedChannelConfirmation, { deep: true })
watch(() => props.groups, clearMixedChannelConfirmation)

const openFilePicker = () => {
  fileInput.value?.click()
}

const handleFileChange = (event: Event) => {
  const target = event.target as HTMLInputElement
  setSelectedFiles(target.files)
  target.value = ''
}

const handleClose = () => {
  if (importing.value) return
  clearMixedChannelConfirmation()
  if (hasCreatedData.value) {
    hasCreatedData.value = false
    emit('imported')
  }
  emit('close')
}

const isJsonFile = (sourceFile: File) => {
  const name = sourceFile.name.toLowerCase()
  return name.endsWith('.json') || sourceFile.type === 'application/json'
}

const setSelectedFiles = (sourceFiles: FileList | File[] | null | undefined) => {
  if (importing.value) return
  const incoming = Array.from(sourceFiles || [])
  const picked = incoming.filter(isJsonFile)
  if (!picked.length) {
    appStore.showError(t('admin.accounts.dataImportSelectFile'))
    return
  }
  if (picked.length < incoming.length) {
    appStore.showWarning(
      t('admin.accounts.dataImportIgnoredFiles', { count: incoming.length - picked.length })
    )
  }
  files.value = picked
  result.value = null
  clearMixedChannelConfirmation()
}

const handleDragEnter = () => {
  if (importing.value) return
  dragDepth.value += 1
}

const handleDragLeave = () => {
  dragDepth.value = Math.max(0, dragDepth.value - 1)
}

const handleDrop = (event: DragEvent) => {
  dragDepth.value = 0
  if (importing.value) return
  setSelectedFiles(event.dataTransfer?.files)
}

const readFileAsText = async (sourceFile: File): Promise<string> => {
  if (typeof sourceFile.text === 'function') {
    return sourceFile.text()
  }

  if (typeof sourceFile.arrayBuffer === 'function') {
    const buffer = await sourceFile.arrayBuffer()
    return new TextDecoder().decode(buffer)
  }

  return await new Promise<string>((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result ?? ''))
    reader.onerror = () => reject(reader.error || new Error('Failed to read file'))
    reader.readAsText(sourceFile)
  })
}

const SUPPORTED_DATA_TYPES = ['sub2api-data', 'sub2api-bundle']
const SUPPORTED_DATA_VERSION = 1

// 与后端 validateDataHeader 对齐:合并前逐文件校验,避免坏文件混入合并 payload 后
// 报错无法定位来源,或绕过后端本会对单文件做的 type/version 检查。
const isValidDataPayload = (payload: unknown): payload is AdminDataPayload => {
  if (!payload || typeof payload !== 'object' || Array.isArray(payload)) return false
  const candidate = payload as Record<string, unknown>
  if (
    candidate.type !== undefined &&
    candidate.type !== '' &&
    !SUPPORTED_DATA_TYPES.includes(candidate.type as string)
  ) {
    return false
  }
  if (
    candidate.version !== undefined &&
    candidate.version !== 0 &&
    candidate.version !== SUPPORTED_DATA_VERSION
  ) {
    return false
  }
  return Array.isArray(candidate.proxies) && Array.isArray(candidate.accounts)
}

const mergeDataPayloads = (payloads: AdminDataPayload[]): AdminDataPayload => {
  const [firstPayload] = payloads
  if (payloads.length === 1 && firstPayload) return firstPayload

  return {
    type: payloads.find((item) => typeof item.type === 'string')?.type,
    version: payloads.find((item) => typeof item.version === 'number')?.version,
    exported_at: new Date().toISOString(),
    proxies: payloads.flatMap((item) => item.proxies),
    accounts: payloads.flatMap((item) => item.accounts),
    skipped_shadows: payloads.reduce((sum, item) => {
      const count = Number(item.skipped_shadows || 0)
      return Number.isFinite(count) ? sum + count : sum
    }, 0)
  }
}

const normalizePlatform = (platform: unknown) => {
  const normalized = typeof platform === 'string' ? platform.trim().toLowerCase() : ''
  return normalized === 'claude' ? 'anthropic' : normalized
}

// 与后端 validateDataAccount 的覆盖检查口径保持一致。无效账号应进入逐项失败结果，
// 不能因为选择了群组就被前端提前拦掉整个批次。
const isValidAccountForGroupCoverage = (account: AdminDataPayload['accounts'][number]) => {
  const item = account as unknown as Record<string, unknown>
  if (typeof item.name !== 'string' || item.name.trim() === '') return false
  if (normalizePlatform(item.platform) === '') return false
  if (
    typeof item.type !== 'string' ||
    !['oauth', 'setup-token', 'apikey', 'upstream'].includes(item.type)
  ) {
    return false
  }
  if (
    !item.credentials ||
    typeof item.credentials !== 'object' ||
    Array.isArray(item.credentials) ||
    Object.keys(item.credentials).length === 0
  ) {
    return false
  }
  if (typeof item.rate_multiplier === 'number' && item.rate_multiplier < 0) return false
  if (typeof item.concurrency === 'number' && item.concurrency < 0) return false
  if (typeof item.priority === 'number' && item.priority < 0) return false
  return true
}

const getCompatibleGroupPlatforms = (account: AdminDataPayload['accounts'][number]) => {
  const platform = normalizePlatform(account.platform)
  const platforms = new Set([platform])
  if (platform === 'antigravity' && account.extra?.mixed_scheduling === true) {
    platforms.add('anthropic')
    platforms.add('gemini')
  }
  return platforms
}

const findUncoveredPlatforms = (payload: AdminDataPayload, selectedGroupIDs: number[]) => {
  if (selectedGroupIDs.length === 0) return []

  const selectedIDs = new Set(selectedGroupIDs)
  const selectedPlatforms = new Set(
    props.groups
      .filter((group) => selectedIDs.has(group.id))
      .map((group) => normalizePlatform(group.platform))
  )
  const uncoveredPlatforms = new Set<string>()
  for (const account of payload.accounts) {
    if (!isValidAccountForGroupCoverage(account)) continue
    const compatiblePlatforms = getCompatibleGroupPlatforms(account)
    if (Array.from(compatiblePlatforms).some((platform) => selectedPlatforms.has(platform))) {
      continue
    }
    uncoveredPlatforms.add(normalizePlatform(account.platform) || '<empty>')
  }
  return Array.from(uncoveredPlatforms)
}

const isMixedChannelWarning = (error: unknown) => {
  if (!error || typeof error !== 'object') return false
  const candidate = error as Record<string, unknown>
  return (
    candidate.status === 409 &&
    (candidate.reason === 'MIXED_CHANNEL_WARNING' || candidate.code === 'MIXED_CHANNEL_WARNING')
  )
}

const applyImportResult = (res: AdminDataImportResult) => {
  result.value = res

  const msgParams: Record<string, unknown> = {
    account_created: res.account_created,
    account_failed: res.account_failed,
    proxy_created: res.proxy_created,
    proxy_reused: res.proxy_reused,
    proxy_failed: res.proxy_failed
  }
  if (res.account_failed > 0 || res.proxy_failed > 0) {
    // 部分成功也创建了数据;弹窗关闭时通过 imported 通知父组件刷新列表
    if (res.account_created > 0 || res.proxy_created > 0) {
      hasCreatedData.value = true
    }
    appStore.showError(t('admin.accounts.dataImportCompletedWithErrors', msgParams))
  } else {
    appStore.showSuccess(t('admin.accounts.dataImportSuccess', msgParams))
    emit('imported')
  }
}

const submitImport = async (
  dataPayload: AdminDataPayload,
  groupIDs: number[],
  confirmMixedChannelRisk: boolean
) => {
  try {
    const res = await adminAPI.accounts.importData({
      data: dataPayload,
      skip_default_group_bind: true,
      ...(groupIDs.length > 0 ? { group_ids: groupIDs } : {}),
      ...(confirmMixedChannelRisk ? { confirm_mixed_channel_risk: true } : {})
    })
    applyImportResult(res)
  } catch (error: unknown) {
    if (!confirmMixedChannelRisk && props.show && isMixedChannelWarning(error)) {
      pendingMixedChannelImport.value = {
        data: dataPayload,
        groupIDs: [...groupIDs]
      }
      return
    }
    const message =
      error && typeof error === 'object' && 'message' in error
        ? String((error as { message?: unknown }).message || '')
        : ''
    appStore.showError(message || t('admin.accounts.dataImportFailed'))
  }
}

const handleMixedChannelConfirm = async () => {
  const pending = pendingMixedChannelImport.value
  if (!pending || importing.value) return

  clearMixedChannelConfirmation()
  importing.value = true
  try {
    await submitImport(pending.data, pending.groupIDs, true)
  } finally {
    importing.value = false
  }
}

const handleImport = async () => {
  if (files.value.length === 0) {
    appStore.showError(t('admin.accounts.dataImportSelectFile'))
    return
  }

  clearMixedChannelConfirmation()
  importing.value = true
  try {
    const dataPayloads: AdminDataPayload[] = []
    for (const sourceFile of files.value) {
      let parsed: unknown
      try {
        parsed = JSON.parse(await readFileAsText(sourceFile))
      } catch {
        appStore.showError(
          t('admin.accounts.dataImportParseFailedFile', { name: sourceFile.name })
        )
        return
      }
      if (!isValidDataPayload(parsed)) {
        appStore.showError(t('admin.accounts.dataImportInvalidFile', { name: sourceFile.name }))
        return
      }
      dataPayloads.push(parsed)
    }
    const dataPayload = mergeDataPayloads(dataPayloads)

    const groupIDs = [...groupIds.value]
    const uncoveredPlatforms = findUncoveredPlatforms(dataPayload, groupIDs)
    if (uncoveredPlatforms.length > 0) {
      appStore.showError(
        t('admin.accounts.dataImportGroupPlatformMismatch', {
          platforms: uncoveredPlatforms.join(', ')
        })
      )
      return
    }
    await submitImport(dataPayload, groupIDs, false)
  } finally {
    importing.value = false
  }
}
</script>
