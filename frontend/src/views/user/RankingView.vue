<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
        <div>
          <h1 class="text-2xl font-bold text-gray-900 dark:text-white">{{ t('ranking.title') }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-300">{{ t('ranking.description') }}</p>
        </div>

        <div class="flex flex-col gap-3 sm:flex-row sm:items-center">
          <div class="inline-flex w-full overflow-x-auto rounded-lg border border-gray-200 bg-white p-1 shadow-sm dark:border-dark-700 dark:bg-dark-900 sm:w-auto">
            <button
              v-for="option in periodOptions"
              :key="option.value"
              type="button"
              class="flex-1 whitespace-nowrap rounded-md px-3 py-2 text-sm font-medium transition-colors sm:flex-none"
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
      </div>

      <div class="grid grid-cols-1 gap-4 md:grid-cols-3">
        <div class="card p-5">
          <div class="flex items-center gap-3">
            <div class="rounded-lg bg-amber-100 p-2 dark:bg-amber-900/30">
              <Icon name="fire" size="md" class="text-amber-600 dark:text-amber-400" />
            </div>
            <div class="min-w-0">
              <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('ranking.totalTokens') }}</p>
              <p class="text-xl font-bold text-gray-900 dark:text-white">{{ formatCompact(ranking?.total_tokens || 0) }}</p>
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
            <div class="rounded-lg bg-purple-100 p-2 dark:bg-purple-900/30">
              <Icon name="cube" size="md" class="text-purple-600 dark:text-purple-400" />
            </div>
            <div class="min-w-0">
              <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('ranking.totalModels') }}</p>
              <p class="text-xl font-bold text-gray-900 dark:text-white">{{ formatNumber(ranking?.total_models || 0) }}</p>
            </div>
          </div>
        </div>
      </div>

      <section class="card overflow-hidden">
        <div class="flex flex-col gap-2 border-b border-gray-100 px-6 py-4 dark:border-dark-700 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('ranking.modelRanking') }}</h2>
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
            :key="`${item.rank}-${item.model_name}`"
            class="flex flex-col gap-4 px-4 py-4 transition-colors hover:bg-gray-50 dark:hover:bg-dark-800/60 sm:px-6 lg:flex-row lg:items-center"
          >
            <div class="flex min-w-0 flex-1 items-center gap-4">
              <div :class="rankBadgeClass(item.rank)" class="flex h-10 w-10 flex-none items-center justify-center rounded-lg text-sm font-bold">
                #{{ item.rank }}
              </div>

              <div class="flex h-10 w-10 flex-none items-center justify-center rounded-lg bg-gray-100 dark:bg-dark-700">
                <ModelIcon :model="item.model_name || item.vendor" size="22px" />
              </div>

              <div class="min-w-0 flex-1">
                <div class="flex min-w-0 flex-wrap items-center gap-2">
                  <p class="truncate text-sm font-semibold text-gray-900 dark:text-white">{{ item.model_name }}</p>
                  <span class="rounded bg-gray-100 px-1.5 py-0.5 text-[11px] font-medium text-gray-600 dark:bg-dark-700 dark:text-dark-200">
                    {{ item.vendor || t('ranking.unknownVendor') }}
                  </span>
                </div>
                <div class="mt-2 h-2 overflow-hidden rounded-full bg-gray-100 dark:bg-dark-800">
                  <div
                    class="h-full rounded-full bg-primary-500 transition-all"
                    :style="{ width: `${Math.max(item.share * 100, 1)}%` }"
                  />
                </div>
                <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
                  {{ formatNumber(item.requests) }} {{ t('ranking.requests') }} · {{ formatPercent(item.share) }} {{ t('ranking.share') }}
                </p>
              </div>
            </div>

            <div class="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:w-[360px]">
              <div class="rounded-lg bg-gray-50 p-3 text-right dark:bg-dark-800">
                <p class="text-sm font-semibold text-gray-900 dark:text-white">{{ formatCompact(item.total_tokens) }}</p>
                <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ t('ranking.tokens') }}</p>
              </div>
              <div class="rounded-lg bg-gray-50 p-3 text-right dark:bg-dark-800">
                <p :class="growthClass(item.growth_pct)" class="text-sm font-semibold">{{ formatGrowth(item.growth_pct) }}</p>
                <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ t('ranking.growth') }}</p>
              </div>
              <div class="rounded-lg bg-gray-50 p-3 text-right dark:bg-dark-800 sm:block">
                <p :class="rankDeltaClass(item.rank_delta)" class="text-sm font-semibold">{{ formatRankDelta(item.rank_delta) }}</p>
                <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ t('ranking.rankChange') }}</p>
              </div>
            </div>
          </div>
        </div>
      </section>

      <div class="grid grid-cols-1 gap-6 xl:grid-cols-3">
        <section class="card overflow-hidden xl:col-span-2">
          <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
            <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('ranking.vendorShare') }}</h2>
            <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('ranking.vendorShareDesc') }}</p>
          </div>

          <div v-if="loading" class="flex items-center justify-center py-12">
            <LoadingSpinner />
          </div>
          <div v-else-if="vendorItems.length === 0" class="py-10">
            <EmptyState :message="t('ranking.noUsage')" />
          </div>
          <div v-else class="divide-y divide-gray-100 dark:divide-dark-700">
            <div v-for="vendor in vendorItems" :key="vendor.vendor" class="px-6 py-4">
              <div class="mb-2 flex items-center justify-between gap-4">
                <div class="min-w-0">
                  <p class="truncate text-sm font-semibold text-gray-900 dark:text-white">
                    #{{ vendor.rank }} {{ vendor.vendor || t('ranking.unknownVendor') }}
                  </p>
                  <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
                    {{ t('ranking.modelsCount', { count: vendor.models_count }) }} · {{ t('ranking.topModel') }} {{ vendor.top_model || '-' }}
                  </p>
                </div>
                <div class="text-right">
                  <p class="text-sm font-semibold text-gray-900 dark:text-white">{{ formatCompact(vendor.total_tokens) }}</p>
                  <p class="text-xs text-gray-500 dark:text-dark-400">{{ formatPercent(vendor.share) }}</p>
                </div>
              </div>
              <div class="h-2 overflow-hidden rounded-full bg-gray-100 dark:bg-dark-800">
                <div class="h-full rounded-full bg-indigo-500" :style="{ width: `${Math.max(vendor.share * 100, 1)}%` }" />
              </div>
            </div>
          </div>
        </section>

        <aside class="space-y-6">
          <section class="card overflow-hidden">
            <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
              <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('ranking.topMovers') }}</h2>
              <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('ranking.topMoversDesc') }}</p>
            </div>
            <RankingMoverList :items="ranking?.top_movers || []" type="up" />
          </section>

          <section class="card overflow-hidden">
            <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
              <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('ranking.topDroppers') }}</h2>
              <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('ranking.topDroppersDesc') }}</p>
            </div>
            <RankingMoverList :items="ranking?.top_droppers || []" type="down" />
          </section>
        </aside>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, onMounted, ref, watch } from 'vue'
import type { PropType } from 'vue'
import { useI18n } from 'vue-i18n'
import { usageAPI } from '@/api'
import { useAppStore } from '@/stores/app'
import AppLayout from '@/components/layout/AppLayout.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import ModelIcon from '@/components/common/ModelIcon.vue'
import Icon from '@/components/icons/Icon.vue'
import type {
  ModelRankingMover,
  ModelUsageRankingResponse,
  RankingPeriod
} from '@/types'

const { t } = useI18n()
const appStore = useAppStore()

const activePeriod = ref<RankingPeriod>('weekly')
const ranking = ref<ModelUsageRankingResponse | null>(null)
const loading = ref(false)
let loadSeq = 0

const periodOptions = computed(() => [
  { value: 'daily' as RankingPeriod, label: t('ranking.daily') },
  { value: 'weekly' as RankingPeriod, label: t('ranking.weekly') },
  { value: 'monthly' as RankingPeriod, label: t('ranking.monthly') },
  { value: 'yearly' as RankingPeriod, label: t('ranking.yearly') },
  { value: 'all' as RankingPeriod, label: t('ranking.allTime') }
])

const rankingItems = computed(() => ranking.value?.models || [])
const vendorItems = computed(() => ranking.value?.vendors || [])

const periodText = computed(() => {
  if (!ranking.value?.start_date || !ranking.value?.end_date) return ''
  return t('ranking.periodRange', {
    start: ranking.value.start_date,
    end: ranking.value.end_date
  })
})

const loadRanking = async () => {
  const currentSeq = ++loadSeq
  loading.value = true
  try {
    const data = await usageAPI.getModelRanking({
      period: activePeriod.value,
      limit: 20
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

const formatNumber = (value: number): string => {
  return value.toLocaleString()
}

const formatCompact = (value: number): string => {
  if (value >= 1_000_000_000) return `${(value / 1_000_000_000).toFixed(2)}B`
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(2)}M`
  if (value >= 1_000) return `${(value / 1_000).toFixed(2)}K`
  return value.toLocaleString()
}

const formatPercent = (value: number): string => `${(value * 100).toFixed(value >= 0.1 ? 1 : 2)}%`

const formatGrowth = (value: number): string => {
  if (!Number.isFinite(value)) return '-'
  if (value > 0) return `+${value.toFixed(1)}%`
  if (value < 0) return `${value.toFixed(1)}%`
  return '0.0%'
}

const formatRankDelta = (value: number): string => {
  if (value > 0) return `↑ ${value}`
  if (value < 0) return `↓ ${Math.abs(value)}`
  return '—'
}

const formatDateTime = (value: string): string => {
  if (!value) return ''
  return new Date(value).toLocaleString()
}

const rankBadgeClass = (rank: number): string => {
  if (rank === 1) return 'bg-amber-100 text-amber-700 dark:bg-amber-500/20 dark:text-amber-300'
  if (rank === 2) return 'bg-slate-100 text-slate-700 dark:bg-slate-500/20 dark:text-slate-300'
  if (rank === 3) return 'bg-orange-100 text-orange-700 dark:bg-orange-500/20 dark:text-orange-300'
  return 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-dark-200'
}

const growthClass = (value: number): string => {
  if (value > 0) return 'text-green-600 dark:text-green-400'
  if (value < 0) return 'text-red-600 dark:text-red-400'
  return 'text-gray-600 dark:text-dark-300'
}

const rankDeltaClass = (value: number): string => {
  if (value > 0) return 'text-green-600 dark:text-green-400'
  if (value < 0) return 'text-red-600 dark:text-red-400'
  return 'text-gray-600 dark:text-dark-300'
}

const RankingMoverList = defineComponent({
  name: 'RankingMoverList',
  props: {
    items: {
      type: Array as PropType<ModelRankingMover[]>,
      required: true
    },
    type: {
      type: String as () => 'up' | 'down',
      required: true
    }
  },
  setup(props) {
    return () => {
      if (loading.value) {
        return h('div', { class: 'flex items-center justify-center py-10' }, [h(LoadingSpinner)])
      }
      if (!props.items.length) {
        return h('div', { class: 'px-6 py-8 text-center text-sm text-gray-500 dark:text-dark-300' }, t('ranking.noTrend'))
      }
      return h('div', { class: 'divide-y divide-gray-100 dark:divide-dark-700' }, props.items.map((item: ModelRankingMover) => h('div', {
        key: `${item.model_name}-${item.rank_delta}`,
        class: 'flex items-center gap-3 px-6 py-3'
      }, [
        h('div', { class: 'flex h-8 w-8 flex-none items-center justify-center rounded-lg bg-gray-100 dark:bg-dark-700' }, [
          h(ModelIcon, { model: item.model_name, size: '18px' })
        ]),
        h('div', { class: 'min-w-0 flex-1' }, [
          h('p', { class: 'truncate text-sm font-medium text-gray-900 dark:text-white' }, item.model_name),
          h('p', { class: 'text-xs text-gray-500 dark:text-dark-400' }, `#${item.current_rank} · ${item.vendor || t('ranking.unknownVendor')}`)
        ]),
        h('div', { class: props.type === 'up' ? 'text-sm font-semibold text-green-600 dark:text-green-400' : 'text-sm font-semibold text-red-600 dark:text-red-400' },
          props.type === 'up' ? `↑ ${item.rank_delta}` : `↓ ${Math.abs(item.rank_delta)}`)
      ])))
    }
  }
})

watch(activePeriod, () => {
  loadRanking()
})

onMounted(() => {
  loadRanking()
})
</script>
