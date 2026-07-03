<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="grid gap-4 lg:grid-cols-[1.1fr_0.9fr]">
        <div class="card p-6">
          <div class="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
            <div>
              <h3 class="text-base font-semibold text-gray-900 dark:text-white">
                {{ t('accountContributions.contributeOpenAI.title') }}
              </h3>
              <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">
                {{ t('accountContributions.contributeOpenAI.description') }}
              </p>
            </div>
            <div class="flex flex-wrap gap-2">
              <button class="btn btn-secondary" @click="showImportDialog = true">
                <Icon name="upload" size="sm" />
                <span>{{ t('accountContributions.importJson.button') }}</span>
              </button>
              <button class="btn btn-primary" :disabled="startingOAuth" @click="startOpenAIOAuth">
                <Icon v-if="startingOAuth" name="refresh" size="sm" class="animate-spin" />
                <Icon v-else name="link" size="sm" />
                <span>{{ startingOAuth ? t('accountContributions.startingOAuth') : t('accountContributions.startOAuth') }}</span>
              </button>
            </div>
          </div>

          <div class="mt-5 rounded-xl border border-primary-200 bg-primary-50 p-4 dark:border-primary-900/40 dark:bg-primary-900/20">
            <p class="text-sm font-medium text-primary-800 dark:text-primary-200">
              {{ t('accountContributions.rules.title') }}
            </p>
            <ul class="mt-2 space-y-1 text-sm text-primary-700 dark:text-primary-300">
              <li>{{ t('accountContributions.rules.line1') }}</li>
              <li>{{ t('accountContributions.rules.line2') }}</li>
              <li>{{ t('accountContributions.rules.line3') }}</li>
            </ul>
          </div>
        </div>

        <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-1">
          <div class="card p-5">
            <p class="text-sm text-gray-500 dark:text-dark-400">
              {{ t('accountContributions.stats.totalAccounts') }}
            </p>
            <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">
              {{ accountsPagination.total }}
            </p>
          </div>
          <div class="card p-5">
            <p class="text-sm text-gray-500 dark:text-dark-400">
              {{ t('accountContributions.stats.totalRewards') }}
            </p>
            <p class="mt-2 text-2xl font-semibold text-emerald-600 dark:text-emerald-400">
              {{ formatCurrency(rewardSummary.total_reward) }}
            </p>
          </div>
          <div class="card p-5">
            <p class="text-sm text-gray-500 dark:text-dark-400">
              {{ t('accountContributions.stats.todayRewards') }}
            </p>
            <p class="mt-2 text-2xl font-semibold text-emerald-600 dark:text-emerald-400">
              {{ formatCurrency(rewardSummary.today_reward) }}
            </p>
          </div>
          <div class="card p-5">
            <p class="text-sm text-gray-500 dark:text-dark-400">
              {{ t('accountContributions.stats.last7dRewards') }}
            </p>
            <p class="mt-2 text-2xl font-semibold text-emerald-600 dark:text-emerald-400">
              {{ formatCurrency(rewardSummary.last_7d_reward) }}
            </p>
          </div>
        </div>
      </div>

      <div class="card overflow-hidden">
        <div class="flex flex-col gap-3 border-b border-gray-100 p-5 dark:border-dark-800 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h3 class="text-base font-semibold text-gray-900 dark:text-white">
              {{ t('accountContributions.accounts.title') }}
            </h3>
            <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">
              {{ t('accountContributions.accounts.description') }}
            </p>
          </div>
          <button class="btn btn-secondary" :disabled="accountsLoading" @click="loadAccounts">
            <Icon name="refresh" size="sm" :class="accountsLoading ? 'animate-spin' : ''" />
            <span>{{ t('common.refresh') }}</span>
          </button>
        </div>

        <DataTable :columns="accountColumns" :data="accounts" :loading="accountsLoading" row-key="id">
          <template #cell-id="{ value }">
            <span class="font-mono text-xs text-gray-500 dark:text-dark-400">#{{ value }}</span>
          </template>
          <template #cell-name="{ row }">
            <div>
              <p class="font-medium text-gray-900 dark:text-white">{{ row.name || '-' }}</p>
              <p class="text-xs text-gray-500 dark:text-dark-400">{{ row.platform }} / {{ row.type }}</p>
            </div>
          </template>
          <template #cell-status="{ row }">
            <div class="flex flex-wrap gap-1">
              <span :class="['badge', accountStatusClass(row.status)]">
                {{ accountStatusLabel(row.status) }}
              </span>
              <span :class="['badge', contributionStatusClass(row.contribution_status)]">
                {{ contributionStatusLabel(row.contribution_status) }}
              </span>
            </div>
          </template>
          <template #cell-submitted_at="{ row }">
            <div class="space-y-1 text-xs text-gray-500 dark:text-dark-400">
              <p>{{ t('accountContributions.accounts.submitted') }}：{{ formatDateTime(row.contribution_submitted_at) || '-' }}</p>
              <p>{{ t('accountContributions.accounts.approved') }}：{{ formatDateTime(row.contribution_approved_at) || '-' }}</p>
              <p>{{ t('accountContributions.accounts.revoked') }}：{{ formatDateTime(row.contribution_revoked_at) || '-' }}</p>
            </div>
          </template>
          <template #cell-actions="{ row }">
            <button
              v-if="canRevoke(row)"
              class="btn btn-secondary btn-sm text-red-600 hover:bg-red-50 hover:text-red-700 dark:text-red-400 dark:hover:bg-red-900/20"
              :disabled="revokingId === row.id"
              @click="revoke(row.id)"
            >
              <Icon v-if="revokingId === row.id" name="refresh" size="sm" class="animate-spin" />
              <Icon v-else name="x" size="sm" />
              <span>{{ t('accountContributions.revoke') }}</span>
            </button>
            <span v-else class="text-sm text-gray-400 dark:text-dark-500">-</span>
          </template>
        </DataTable>

        <Pagination
          v-if="accountsPagination.total > 0"
          :page="accountsPagination.page"
          :total="accountsPagination.total"
          :page-size="accountsPagination.page_size"
          @update:page="handleAccountsPageChange"
          @update:pageSize="handleAccountsPageSizeChange"
        />
      </div>

      <div class="card overflow-hidden">
        <div class="flex flex-col gap-3 border-b border-gray-100 p-5 dark:border-dark-800 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h3 class="text-base font-semibold text-gray-900 dark:text-white">
              {{ t('accountContributions.rewards.title') }}
            </h3>
            <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">
              {{ t('accountContributions.rewards.description') }}
            </p>
          </div>
          <button class="btn btn-secondary" :disabled="rewardsLoading" @click="loadRewardData">
            <Icon name="refresh" size="sm" :class="rewardsLoading ? 'animate-spin' : ''" />
            <span>{{ t('common.refresh') }}</span>
          </button>
        </div>

        <DataTable :columns="rewardColumns" :data="rewards" :loading="rewardsLoading" row-key="id">
          <template #cell-created_at="{ value }">
            <span class="text-sm text-gray-500 dark:text-dark-400">{{ formatDateTime(value) || '-' }}</span>
          </template>
          <template #cell-account_id="{ value }">
            <span class="font-mono text-xs text-gray-500 dark:text-dark-400">#{{ value }}</span>
          </template>
          <template #cell-group_id="{ value }">
            <span class="font-mono text-xs text-gray-500 dark:text-dark-400">#{{ value }}</span>
          </template>
          <template #cell-total_cost="{ value }">
            <span>{{ formatCurrency(value) }}</span>
          </template>
          <template #cell-actual_cost="{ value }">
            <span>{{ formatCurrency(value) }}</span>
          </template>
          <template #cell-reward_multiplier="{ value }">
            <span>x{{ formatMultiplier(value) }}</span>
          </template>
          <template #cell-reward_amount="{ value }">
            <span class="font-medium text-emerald-600 dark:text-emerald-400">+{{ formatCurrency(value) }}</span>
          </template>
          <template #cell-request_id="{ value }">
            <code class="block max-w-[220px] truncate text-xs text-gray-500 dark:text-dark-400">{{ value }}</code>
          </template>
        </DataTable>

        <Pagination
          v-if="rewardsPagination.total > 0"
          :page="rewardsPagination.page"
          :total="rewardsPagination.total"
          :page-size="rewardsPagination.page_size"
          @update:page="handleRewardsPageChange"
          @update:pageSize="handleRewardsPageSizeChange"
        />
      </div>
    </div>

    <BaseDialog
      :show="showImportDialog"
      :title="t('accountContributions.importJson.title')"
      width="normal"
      close-on-click-outside
      @close="closeImportDialog"
    >
      <form id="contribution-import-json-form" class="space-y-4" @submit.prevent="importJSON">
        <div class="text-sm text-gray-600 dark:text-dark-300">
          {{ t('accountContributions.importJson.hint') }}
        </div>
        <div class="rounded-xl border border-amber-200 bg-amber-50 p-3 text-xs text-amber-700 dark:border-amber-900/40 dark:bg-amber-900/20 dark:text-amber-300">
          {{ t('accountContributions.importJson.warning') }}
        </div>

        <div>
          <label class="input-label">{{ t('accountContributions.importJson.file') }}</label>
          <div class="flex items-center justify-between gap-3 rounded-lg border border-dashed border-gray-300 bg-gray-50 px-4 py-3 dark:border-dark-600 dark:bg-dark-800">
            <div class="min-w-0">
              <div class="truncate text-sm text-gray-700 dark:text-dark-200">
                {{ importFileName || t('accountContributions.importJson.selectFile') }}
              </div>
              <div class="text-xs text-gray-500 dark:text-dark-400">
                {{ t('accountContributions.importJson.fileHint') }}
              </div>
            </div>
            <button type="button" class="btn btn-secondary shrink-0" @click="openImportFilePicker">
              {{ t('common.chooseFile') }}
            </button>
          </div>
          <input
            ref="importFileInput"
            type="file"
            class="hidden"
            accept="application/json,.json"
            multiple
            @change="handleImportFileChange"
          />
        </div>

        <div v-if="importPreview" class="space-y-3 rounded-xl border border-blue-200 bg-blue-50 p-4 dark:border-blue-900/40 dark:bg-blue-900/20">
          <div class="flex items-center justify-between gap-3">
            <div class="text-sm font-medium text-blue-900 dark:text-blue-100">
              {{ t('accountContributions.importJson.preview') }}
            </div>
            <Icon v-if="previewingJSON" name="refresh" size="sm" class="animate-spin text-blue-600" />
          </div>
          <div class="grid grid-cols-2 gap-3 text-sm sm:grid-cols-4">
            <div>
              <p class="text-blue-700 dark:text-blue-300">{{ t('accountContributions.importJson.previewTotal') }}</p>
              <p class="font-semibold text-blue-950 dark:text-blue-100">{{ importPreview.total }}</p>
            </div>
            <div>
              <p class="text-blue-700 dark:text-blue-300">{{ t('accountContributions.importJson.previewValid') }}</p>
              <p class="font-semibold text-blue-950 dark:text-blue-100">{{ importPreview.valid }}</p>
            </div>
            <div>
              <p class="text-blue-700 dark:text-blue-300">{{ t('accountContributions.importJson.previewDuplicate') }}</p>
              <p class="font-semibold text-blue-950 dark:text-blue-100">{{ importPreview.duplicate }}</p>
            </div>
            <div>
              <p class="text-blue-700 dark:text-blue-300">{{ t('accountContributions.importJson.previewUnsupported') }}</p>
              <p class="font-semibold text-blue-950 dark:text-blue-100">{{ importPreview.unsupported + importPreview.invalid }}</p>
            </div>
          </div>
          <div v-if="importPreviewProblemItems.length" class="max-h-40 overflow-auto rounded-lg bg-white/70 p-3 font-mono text-xs text-blue-900 dark:bg-dark-900/70 dark:text-blue-100">
            <div v-for="item in importPreviewProblemItems" :key="`${item.index}-${item.name || ''}`" class="whitespace-pre-wrap">
              #{{ item.index + 1 }} {{ item.name || '-' }} — {{ item.message || previewItemFallbackMessage(item) }}
            </div>
          </div>
        </div>

        <div v-if="importResult" class="space-y-2 rounded-xl border border-gray-200 p-4 dark:border-dark-700">
          <div class="text-sm font-medium text-gray-900 dark:text-white">
            {{ t('accountContributions.importJson.result') }}
          </div>
          <div class="text-sm text-gray-700 dark:text-dark-300">
            {{ t('accountContributions.importJson.resultSummary', importResult) }}
          </div>
          <div v-if="importErrors.length" class="mt-2">
            <div class="text-sm font-medium text-red-600 dark:text-red-400">
              {{ t('accountContributions.importJson.errors') }}
            </div>
            <div class="mt-2 max-h-48 overflow-auto rounded-lg bg-gray-50 p-3 font-mono text-xs dark:bg-dark-800">
              <div v-for="item in importErrors" :key="`${item.index}-${item.name || ''}`" class="whitespace-pre-wrap">
                #{{ item.index + 1 }} {{ item.name || '-' }} — {{ item.message }}
              </div>
            </div>
          </div>
        </div>
      </form>

      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" class="btn btn-secondary" :disabled="importingJSON" @click="closeImportDialog">
            {{ t('common.cancel') }}
          </button>
          <button
            type="submit"
            form="contribution-import-json-form"
            class="btn btn-primary"
            :disabled="importingJSON || previewingJSON"
          >
            {{ importingJSON ? t('accountContributions.importJson.importing') : t('accountContributions.importJson.submit') }}
          </button>
        </div>
      </template>
    </BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import type { Column } from '@/components/common/types'
import accountContributionsAPI, { type ContributionImportPreview, type ContributionImportPreviewItem, type ContributionImportResult } from '@/api/accountContributions'
import type { Account, AdminDataPayload, ContributorRewardLog, ContributorRewardSummary } from '@/types'
import { useAppStore } from '@/stores/app'
import { getPersistedPageSize } from '@/composables/usePersistedPageSize'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatCurrency, formatDateTime } from '@/utils/format'

const { t } = useI18n()
const appStore = useAppStore()

const accounts = ref<Account[]>([])
const rewards = ref<ContributorRewardLog[]>([])
const accountsLoading = ref(false)
const rewardsLoading = ref(false)
const rewardSummaryLoading = ref(false)
const startingOAuth = ref(false)
const revokingId = ref<number | null>(null)
const showImportDialog = ref(false)
const importingJSON = ref(false)
const previewingJSON = ref(false)
const importFiles = ref<File[]>([])
const importResult = ref<ContributionImportResult | null>(null)
const importPreview = ref<ContributionImportPreview | null>(null)
const importFileInput = ref<HTMLInputElement | null>(null)

const accountsPagination = reactive({ page: 1, page_size: getPersistedPageSize(), total: 0 })
const rewardsPagination = reactive({ page: 1, page_size: getPersistedPageSize(), total: 0 })
const rewardSummary = reactive<ContributorRewardSummary>({ total_reward: 0, today_reward: 0, last_7d_reward: 0 })

const accountColumns = computed<Column[]>(() => [
  { key: 'id', label: t('accountContributions.accounts.columns.id') },
  { key: 'name', label: t('accountContributions.accounts.columns.account') },
  { key: 'status', label: t('accountContributions.accounts.columns.status') },
  { key: 'submitted_at', label: t('accountContributions.accounts.columns.timeline') },
  { key: 'actions', label: t('common.actions') }
])

const rewardColumns = computed<Column[]>(() => [
  { key: 'created_at', label: t('accountContributions.rewards.columns.createdAt') },
  { key: 'account_id', label: t('accountContributions.rewards.columns.account') },
  { key: 'group_id', label: t('accountContributions.rewards.columns.group') },
  { key: 'total_cost', label: t('accountContributions.rewards.columns.totalCost') },
  { key: 'actual_cost', label: t('accountContributions.rewards.columns.actualCost') },
  { key: 'reward_multiplier', label: t('accountContributions.rewards.columns.multiplier') },
  { key: 'reward_amount', label: t('accountContributions.rewards.columns.reward') },
  { key: 'request_id', label: t('accountContributions.rewards.columns.request') }
])

const importErrors = computed(() => importResult.value?.errors || [])
const importPreviewProblemItems = computed(() =>
  (importPreview.value?.items || []).filter(item => !item.valid)
)
const importFileName = computed(() => {
  if (importFiles.value.length === 0) return ''
  if (importFiles.value.length === 1) return importFiles.value[0].name
  return t('accountContributions.importJson.selectedFiles', { count: importFiles.value.length })
})

function contributionStatusLabel(status: Account['contribution_status']): string {
  const normalized = status || ''
  if (normalized === 'pending') return t('accountContributions.status.pending')
  if (normalized === 'approved') return t('accountContributions.status.approved')
  if (normalized === 'rejected') return t('accountContributions.status.rejected')
  if (normalized === 'revoked') return t('accountContributions.status.revoked')
  return '-'
}

function contributionStatusClass(status: Account['contribution_status']): string {
  if (status === 'approved') return 'badge-success'
  if (status === 'pending') return 'badge-warning'
  if (status === 'rejected') return 'badge-danger'
  if (status === 'revoked') return 'badge-gray'
  return 'badge-gray'
}

function accountStatusLabel(status: Account['status']): string {
  if (status === 'active') return t('common.active')
  if (status === 'inactive') return t('common.inactive')
  return status || '-'
}

function accountStatusClass(status: Account['status']): string {
  if (status === 'active') return 'badge-success'
  if (status === 'inactive') return 'badge-gray'
  return 'badge-danger'
}

function formatMultiplier(value: number): string {
  return Number(value || 0).toFixed(2).replace(/\.?0+$/, '')
}

function canRevoke(account: Account): boolean {
  return account.contribution_status === 'pending' || account.contribution_status === 'approved'
}

function resolveRedirectURI(): string {
  return `${window.location.origin}/account-contributions/callback`
}

function openImportFilePicker(): void {
  importFileInput.value?.click()
}

function handleImportFileChange(event: Event): void {
  const target = event.target as HTMLInputElement
  importFiles.value = Array.from(target.files || [])
  importResult.value = null
  importPreview.value = null
  void previewImportJSON()
}

function closeImportDialog(): void {
  if (importingJSON.value) return
  showImportDialog.value = false
}

async function readFileAsText(sourceFile: File): Promise<string> {
  if (typeof sourceFile.text === 'function') {
    return sourceFile.text()
  }
  const buffer = await sourceFile.arrayBuffer()
  return new TextDecoder().decode(buffer)
}

async function parseContributionPayloadFile(sourceFile: File): Promise<AdminDataPayload> {
  const text = await readFileAsText(sourceFile)
  const payload = JSON.parse(text) as AdminDataPayload
  if (!payload || typeof payload !== 'object' || !Array.isArray(payload.accounts)) {
    throw new Error(t('accountContributions.importJson.invalidFile', { file: sourceFile.name }))
  }
  if (payload.proxies && payload.proxies.length > 0) {
    throw new Error(t('accountContributions.importJson.proxyNotSupported', { file: sourceFile.name }))
  }
  return {
    ...payload,
    proxies: payload.proxies || []
  }
}

function mergeContributionPayloads(payloads: AdminDataPayload[]): AdminDataPayload {
  if (payloads.length === 1) return payloads[0]
  return {
    exported_at: new Date().toISOString(),
    proxies: [],
    accounts: payloads.flatMap(payload => payload.accounts)
  }
}


function previewItemFallbackMessage(item: ContributionImportPreviewItem): string {
  if (item.duplicate) return t('accountContributions.importJson.previewDuplicateMessage')
  if (item.unsupported) return t('accountContributions.importJson.previewUnsupportedMessage')
  if (item.invalid) return t('accountContributions.importJson.previewInvalidMessage')
  return '-'
}

async function previewImportJSON(): Promise<void> {
  if (importFiles.value.length === 0) return
  previewingJSON.value = true
  try {
    const payloads = await Promise.all(importFiles.value.map(parseContributionPayloadFile))
    const payload = mergeContributionPayloads(payloads)
    importPreview.value = await accountContributionsAPI.previewOpenAIJSON({ data: payload })
  } catch (error) {
    importPreview.value = null
    if (error instanceof SyntaxError) {
      appStore.showError(t('accountContributions.importJson.parseFailed'))
    } else {
      appStore.showError(extractApiErrorMessage(error, t('accountContributions.importJson.previewFailed')))
    }
  } finally {
    previewingJSON.value = false
  }
}

async function importJSON(): Promise<void> {
  if (importFiles.value.length === 0) {
    appStore.showError(t('accountContributions.importJson.selectFile'))
    return
  }
  importingJSON.value = true
  try {
    const payloads = await Promise.all(importFiles.value.map(parseContributionPayloadFile))
    const payload = mergeContributionPayloads(payloads)
    const result = await accountContributionsAPI.submitOpenAIJSON({ data: payload })
    importResult.value = result
    const resultParams: Record<string, unknown> = {
      total: result.total,
      created: result.created,
      failed: result.failed
    }
    if (result.failed > 0) {
      appStore.showError(t('accountContributions.importJson.completedWithErrors', resultParams))
    } else {
      appStore.showSuccess(t('accountContributions.importJson.success', resultParams))
      await loadAccounts()
    }
  } catch (error) {
    if (error instanceof SyntaxError) {
      appStore.showError(t('accountContributions.importJson.parseFailed'))
    } else {
      appStore.showError(extractApiErrorMessage(error, t('accountContributions.importJson.failed')))
    }
  } finally {
    importingJSON.value = false
  }
}

async function startOpenAIOAuth(): Promise<void> {
  if (startingOAuth.value) return
  startingOAuth.value = true
  try {
    const redirectURI = resolveRedirectURI()
    const result = await accountContributionsAPI.generateOpenAIAuthURL({ redirect_uri: redirectURI })
    const parsed = new URL(result.auth_url)
    const state = parsed.searchParams.get('state') || ''
    sessionStorage.setItem('openai_contribution_session_id', result.session_id)
    sessionStorage.setItem('openai_contribution_state', state)
    sessionStorage.setItem('openai_contribution_redirect_uri', redirectURI)
    window.location.href = result.auth_url
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('accountContributions.errors.startOAuthFailed')))
  } finally {
    startingOAuth.value = false
  }
}

async function loadAccounts(): Promise<void> {
  accountsLoading.value = true
  try {
    const response = await accountContributionsAPI.listMine(
      accountsPagination.page,
      accountsPagination.page_size
    )
    accounts.value = response.items
    accountsPagination.total = response.total
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('accountContributions.errors.loadAccountsFailed')))
  } finally {
    accountsLoading.value = false
  }
}

async function loadRewards(): Promise<void> {
  rewardsLoading.value = true
  try {
    const response = await accountContributionsAPI.listRewards(
      rewardsPagination.page,
      rewardsPagination.page_size
    )
    rewards.value = response.items
    rewardsPagination.total = response.total
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('accountContributions.errors.loadRewardsFailed')))
  } finally {
    rewardsLoading.value = false
  }
}

async function loadRewardSummary(): Promise<void> {
  rewardSummaryLoading.value = true
  try {
    const summary = await accountContributionsAPI.getRewardSummary()
    rewardSummary.total_reward = summary.total_reward
    rewardSummary.today_reward = summary.today_reward
    rewardSummary.last_7d_reward = summary.last_7d_reward
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('accountContributions.errors.loadRewardSummaryFailed')))
  } finally {
    rewardSummaryLoading.value = false
  }
}

async function loadRewardData(): Promise<void> {
  await Promise.all([loadRewards(), loadRewardSummary()])
}

async function revoke(id: number): Promise<void> {
  if (revokingId.value !== null) return
  revokingId.value = id
  try {
    await accountContributionsAPI.revoke(id)
    appStore.showSuccess(t('accountContributions.revoked'))
    await loadAccounts()
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('accountContributions.errors.revokeFailed')))
  } finally {
    revokingId.value = null
  }
}

function handleAccountsPageChange(page: number): void {
  accountsPagination.page = page
  void loadAccounts()
}

function handleAccountsPageSizeChange(pageSize: number): void {
  accountsPagination.page_size = pageSize
  accountsPagination.page = 1
  void loadAccounts()
}

function handleRewardsPageChange(page: number): void {
  rewardsPagination.page = page
  void loadRewards()
}

function handleRewardsPageSizeChange(pageSize: number): void {
  rewardsPagination.page_size = pageSize
  rewardsPagination.page = 1
  void loadRewards()
}

onMounted(() => {
  void Promise.all([loadAccounts(), loadRewardData()])
})
</script>
