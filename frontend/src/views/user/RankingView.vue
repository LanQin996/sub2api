<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
        <div class="inline-flex w-full rounded-lg border border-gray-200 bg-white p-1 shadow-sm dark:border-dark-700 dark:bg-dark-900 sm:w-auto">
          <button
            v-for="option in periodOptions"
            :key="option.value"
            type="button"
            class="flex-1 rounded-md px-4 py-2 text-sm font-medium transition-colors sm:flex-none"
            :class="activePeriod === option.value
              ? 'bg-primary-600 text-white shadow-sm'
              : 'text-gray-600 hover:bg-gray-100 dark:text-dark-300 dark:hover:bg-dark-800'"
            @click="activePeriod = option.value"
          >
            {{ option.label }}
          </button>
        </div>

        <button
          type="button"
          class="btn btn-secondary inline-flex items-center justify-center gap-2"
          :disabled="loading"
          @click="loadRanking"
        >
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
          {{ t('common.refresh') }}
        </button>
      </div>

      <div class="grid grid-cols-1 gap-4 md:grid-cols-3">
        <div class="card p-5">
          <div class="flex items-center gap-3">
            <div class="rounded-lg bg-green-100 p-2 dark:bg-green-900/30">
              <Icon name="dollar" size="md" class="text-green-600 dark:text-green-400" />
            </div>
            <div class="min-w-0">
              <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('ranking.totalSpend') }}</p>
              <p class="text-xl font-bold text-gray-900 dark:text-white">{{ formatMoney(ranking?.total_actual_cost || 0) }}</p>
            </div>
          </div>
        </div>

        <div class="card p-5">
          <div class="flex items-center gap-3">
            <div class="rounded-lg bg-blue-100 p-2 dark:bg-blue-900/30">
              <Icon name="document" size="md" class="text-blue-600 dark:text-blue-400" />
            </div>
            <div class="min-w-0">
              <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('ranking.totalRequests') }}</p>
              <p class="text-xl font-bold text-gray-900 dark:text-white">{{ formatNumber(ranking?.total_requests || 0) }}</p>
            </div>
          </div>
        </div>

        <div class="card p-5">
          <div class="flex items-center gap-3">
            <div class="rounded-lg bg-amber-100 p-2 dark:bg-amber-900/30">
              <Icon name="cube" size="md" class="text-amber-600 dark:text-amber-400" />
            </div>
            <div class="min-w-0">
              <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('ranking.totalTokens') }}</p>
              <p class="text-xl font-bold text-gray-900 dark:text-white">{{ formatCompact(ranking?.total_tokens || 0) }}</p>
            </div>
          </div>
        </div>
      </div>

      <div class="grid grid-cols-1 gap-6 xl:grid-cols-[1fr_360px]">
        <section class="card overflow-hidden">
          <div class="flex flex-col gap-2 border-b border-gray-100 px-6 py-4 dark:border-dark-700 sm:flex-row sm:items-center sm:justify-between">
            <div>
              <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('ranking.currentRanking') }}</h2>
              <p class="text-xs text-gray-500 dark:text-dark-400">{{ periodText }}</p>
            </div>
            <p v-if="ranking?.stats_updated_at" class="text-xs text-gray-500 dark:text-dark-400">
              {{ t('ranking.updatedAt', { time: formatDateTime(ranking.stats_updated_at) }) }}
            </p>
          </div>

          <div v-if="loading" class="flex items-center justify-center py-16">
            <LoadingSpinner />
          </div>

          <div v-else-if="rankingItems.length === 0" class="py-12">
            <EmptyState :message="t('ranking.noUsage')" />
          </div>

          <div v-else class="divide-y divide-gray-100 dark:divide-dark-700">
            <div
              v-for="item in rankingItems"
              :key="`${item.rank}-${item.user_id}`"
              class="flex items-center gap-4 px-4 py-4 transition-colors sm:px-6"
              :class="item.is_current_user ? 'bg-primary-50/70 dark:bg-primary-900/20' : 'hover:bg-gray-50 dark:hover:bg-dark-800/60'"
            >
              <div :class="rankBadgeClass(item.rank)" class="flex h-10 w-10 flex-none items-center justify-center rounded-lg text-sm font-bold">
                #{{ item.rank }}
              </div>

              <div class="flex min-w-0 flex-1 items-center gap-3">
                <div class="flex h-10 w-10 flex-none items-center justify-center overflow-hidden rounded-lg bg-gray-100 text-sm font-semibold text-gray-600 dark:bg-dark-700 dark:text-dark-200">
                  <img
                    v-if="item.avatar_url"
                    :src="item.avatar_url"
                    :alt="item.display_name"
                    class="h-full w-full object-cover"
                  >
                  <span v-else>{{ initials(item.display_name) }}</span>
                </div>
                <div class="min-w-0">
                  <div class="flex min-w-0 items-center gap-2">
                    <p class="truncate text-sm font-semibold text-gray-900 dark:text-white">{{ item.display_name }}</p>
                    <span
                      v-if="item.is_current_user"
                      class="inline-flex flex-none rounded bg-primary-100 px-1.5 py-0.5 text-[11px] font-medium text-primary-700 dark:bg-primary-500/20 dark:text-primary-300"
                    >
                      {{ t('ranking.currentUser') }}
                    </span>
                  </div>
                  <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
                    {{ formatNumber(item.requests) }} {{ t('ranking.requests') }} · {{ formatCompact(item.tokens) }} {{ t('ranking.tokens') }}
                  </p>
                </div>
              </div>

              <div class="text-right">
                <p class="text-sm font-semibold text-green-600 dark:text-green-400">{{ formatMoney(item.actual_cost) }}</p>
                <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ t('ranking.spend') }}</p>
              </div>
            </div>
          </div>
        </section>

        <aside class="card h-fit p-6">
          <div class="mb-5 flex items-center gap-3">
            <div class="rounded-lg bg-primary-100 p-2 dark:bg-primary-900/30">
              <Icon name="user" size="md" class="text-primary-600 dark:text-primary-400" />
            </div>
            <div>
              <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('ranking.myRanking') }}</h2>
              <p class="text-xs text-gray-500 dark:text-dark-400">{{ periodText }}</p>
            </div>
          </div>

          <div v-if="loading" class="flex items-center justify-center py-8">
            <LoadingSpinner />
          </div>

          <template v-else>
            <div class="mb-5 flex items-center gap-3">
              <div class="flex h-12 w-12 items-center justify-center overflow-hidden rounded-lg bg-primary-600 text-base font-semibold text-white">
                <img
                  v-if="currentUser?.avatar_url"
                  :src="currentUser.avatar_url"
                  :alt="currentUser.display_name"
                  class="h-full w-full object-cover"
                >
                <span v-else>{{ initials(currentUser?.display_name || '-') }}</span>
              </div>
              <div class="min-w-0">
                <p class="truncate text-sm font-semibold text-gray-900 dark:text-white">
                  {{ currentUser?.display_name || t('ranking.notRanked') }}
                </p>
                <p class="text-xs text-gray-500 dark:text-dark-400">
                  {{ currentRankLabel }}
                </p>
              </div>
            </div>

            <div class="grid grid-cols-3 gap-3">
              <div class="rounded-lg bg-gray-50 p-3 dark:bg-dark-800">
                <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('ranking.spend') }}</p>
                <p class="mt-1 text-sm font-semibold text-gray-900 dark:text-white">{{ formatMoney(currentUser?.actual_cost || 0) }}</p>
              </div>
              <div class="rounded-lg bg-gray-50 p-3 dark:bg-dark-800">
                <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('ranking.requests') }}</p>
                <p class="mt-1 text-sm font-semibold text-gray-900 dark:text-white">{{ formatNumber(currentUser?.requests || 0) }}</p>
              </div>
              <div class="rounded-lg bg-gray-50 p-3 dark:bg-dark-800">
                <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('ranking.tokens') }}</p>
                <p class="mt-1 text-sm font-semibold text-gray-900 dark:text-white">{{ formatCompact(currentUser?.tokens || 0) }}</p>
              </div>
            </div>
          </template>
        </aside>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { usageAPI } from '@/api'
import { useAppStore } from '@/stores/app'
import AppLayout from '@/components/layout/AppLayout.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Icon from '@/components/icons/Icon.vue'
import type {
  PublicUserSpendingRankingResponse,
  RankingPeriod
} from '@/types'

const { t } = useI18n()
const appStore = useAppStore()

const activePeriod = ref<RankingPeriod>('daily')
const ranking = ref<PublicUserSpendingRankingResponse | null>(null)
const loading = ref(false)
let loadSeq = 0

const periodOptions = computed(() => [
  { value: 'daily' as RankingPeriod, label: t('ranking.daily') },
  { value: 'weekly' as RankingPeriod, label: t('ranking.weekly') },
  { value: 'monthly' as RankingPeriod, label: t('ranking.monthly') }
])

const rankingItems = computed(() => ranking.value?.ranking || [])
const currentUser = computed(() => ranking.value?.current_user || null)

const periodText = computed(() => {
  if (!ranking.value?.start_date || !ranking.value?.end_date) return ''
  return t('ranking.periodRange', {
    start: ranking.value.start_date,
    end: ranking.value.end_date
  })
})

const currentRankLabel = computed(() => {
  const user = currentUser.value
  if (!user?.rank) return t('ranking.notRanked')
  const inTopList = rankingItems.value.some((item) => item.user_id === user.user_id)
  return inTopList ? `#${user.rank}` : `#${user.rank} · ${t('ranking.outsideTop')}`
})

const loadRanking = async () => {
  const currentSeq = ++loadSeq
  loading.value = true
  try {
    const data = await usageAPI.getRanking({
      period: activePeriod.value,
      limit: 50
    })
    if (currentSeq !== loadSeq) return
    ranking.value = data
  } catch (error) {
    if (currentSeq !== loadSeq) return
    console.error('Failed to load ranking:', error)
    appStore.showError(t('ranking.loadFailed'))
  } finally {
    if (currentSeq === loadSeq) {
      loading.value = false
    }
  }
}

const formatMoney = (value: number): string => {
  const digits = value >= 1 ? 2 : 4
  return `$${value.toFixed(digits)}`
}

const formatNumber = (value: number): string => {
  return value.toLocaleString()
}

const formatCompact = (value: number): string => {
  if (value >= 1_000_000_000) return `${(value / 1_000_000_000).toFixed(2)}B`
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(2)}M`
  if (value >= 1_000) return `${(value / 1_000).toFixed(2)}K`
  return value.toLocaleString()
}

const formatDateTime = (value: string): string => {
  if (!value) return ''
  return new Date(value).toLocaleString()
}

const initials = (name: string): string => {
  const trimmed = name.trim()
  if (!trimmed) return '-'
  return Array.from(trimmed)[0].toUpperCase()
}

const rankBadgeClass = (rank: number): string => {
  if (rank === 1) return 'bg-amber-100 text-amber-700 dark:bg-amber-500/20 dark:text-amber-300'
  if (rank === 2) return 'bg-slate-100 text-slate-700 dark:bg-slate-500/20 dark:text-slate-300'
  if (rank === 3) return 'bg-orange-100 text-orange-700 dark:bg-orange-500/20 dark:text-orange-300'
  return 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-dark-200'
}

watch(activePeriod, () => {
  loadRanking()
})

onMounted(() => {
  loadRanking()
})
</script>
