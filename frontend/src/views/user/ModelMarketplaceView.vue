<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="grid grid-cols-1 gap-4 sm:grid-cols-3">
        <div class="card p-4">
          <div class="flex items-center gap-3">
            <div class="flex h-10 w-10 items-center justify-center rounded-xl bg-primary-50 text-primary-600 dark:bg-primary-900/20 dark:text-primary-400">
              <Icon name="brain" size="md" />
            </div>
            <div class="min-w-0">
              <div class="text-2xl font-semibold text-gray-900 dark:text-white">{{ marketplaceStats.models }}</div>
              <div class="text-xs text-gray-500 dark:text-gray-400">{{ t('modelMarketplace.stats.models') }}</div>
            </div>
          </div>
        </div>
        <div class="card p-4">
          <div class="flex items-center gap-3">
            <div class="flex h-10 w-10 items-center justify-center rounded-xl bg-emerald-50 text-emerald-600 dark:bg-emerald-900/20 dark:text-emerald-400">
              <Icon name="server" size="md" />
            </div>
            <div class="min-w-0">
              <div class="text-2xl font-semibold text-gray-900 dark:text-white">{{ marketplaceStats.channels }}</div>
              <div class="text-xs text-gray-500 dark:text-gray-400">{{ t('modelMarketplace.stats.channels') }}</div>
            </div>
          </div>
        </div>
        <div class="card p-4">
          <div class="flex items-center gap-3">
            <div class="flex h-10 w-10 items-center justify-center rounded-xl bg-blue-50 text-blue-600 dark:bg-blue-900/20 dark:text-blue-400">
              <Icon name="grid" size="md" />
            </div>
            <div class="min-w-0">
              <div class="text-2xl font-semibold text-gray-900 dark:text-white">{{ marketplaceStats.platforms }}</div>
              <div class="text-xs text-gray-500 dark:text-gray-400">{{ t('modelMarketplace.stats.platforms') }}</div>
            </div>
          </div>
        </div>
      </div>

      <div class="card p-4">
        <div class="flex flex-col gap-4 xl:flex-row xl:items-center xl:justify-between">
          <div class="flex flex-1 flex-col gap-3 lg:flex-row lg:items-center">
            <div class="relative w-full lg:max-w-md">
              <Icon
                name="search"
                size="md"
                class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400 dark:text-gray-500"
              />
              <input
                v-model="searchQuery"
                type="text"
                :placeholder="t('modelMarketplace.searchPlaceholder')"
                class="input pl-10"
              />
            </div>

            <div class="flex min-w-0 flex-wrap items-center gap-2">
              <button
                v-for="option in platformOptions"
                :key="option.value"
                type="button"
                class="inline-flex h-10 items-center gap-1.5 rounded-xl border px-3 text-sm font-medium transition-colors"
                :class="selectedPlatform === option.value
                  ? 'border-primary-500 bg-primary-50 text-primary-700 dark:border-primary-500/60 dark:bg-primary-900/30 dark:text-primary-300'
                  : 'border-gray-200 bg-white text-gray-600 hover:bg-gray-50 dark:border-dark-600 dark:bg-dark-800 dark:text-gray-300 dark:hover:bg-dark-700'"
                @click="selectedPlatform = option.value"
              >
                <PlatformIcon
                  v-if="option.value !== ALL_PLATFORMS"
                  :platform="option.value as GroupPlatform"
                  size="sm"
                />
                <Icon v-else name="grid" size="sm" />
                <span>{{ option.label }}</span>
                <span class="rounded-md bg-black/5 px-1.5 py-0.5 text-[11px] dark:bg-white/10">
                  {{ option.count }}
                </span>
              </button>
            </div>
          </div>

          <div class="flex flex-wrap items-center justify-end gap-3">
            <label class="sr-only" for="model-marketplace-sort">{{ t('modelMarketplace.sort.label') }}</label>
            <select id="model-marketplace-sort" v-model="sortMode" class="input h-10 w-full sm:w-48">
              <option value="channels-desc">{{ t('modelMarketplace.sort.channelsDesc') }}</option>
              <option value="name-asc">{{ t('modelMarketplace.sort.nameAsc') }}</option>
              <option value="platform-asc">{{ t('modelMarketplace.sort.platformAsc') }}</option>
            </select>
            <button
              type="button"
              @click="loadChannels"
              :disabled="loading"
              class="btn btn-secondary h-10"
              :title="t('common.refresh', 'Refresh')"
            >
              <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
            </button>
          </div>
        </div>
      </div>

      <div v-if="loading" class="grid grid-cols-1 gap-4 xl:grid-cols-2 2xl:grid-cols-3">
        <div v-for="idx in 6" :key="idx" class="card p-5">
          <div class="mb-4 flex items-center justify-between">
            <div class="h-5 w-48 rounded bg-gray-200 dark:bg-dark-700"></div>
            <div class="h-6 w-20 rounded bg-gray-200 dark:bg-dark-700"></div>
          </div>
          <div class="space-y-3">
            <div class="h-4 w-full rounded bg-gray-100 dark:bg-dark-800"></div>
            <div class="h-4 w-4/5 rounded bg-gray-100 dark:bg-dark-800"></div>
            <div class="h-10 w-full rounded bg-gray-100 dark:bg-dark-800"></div>
          </div>
        </div>
      </div>

      <div v-else-if="filteredModels.length === 0" class="card empty-state">
        <Icon name="inbox" size="xl" class="empty-state-icon" />
        <div class="empty-state-title">{{ t('modelMarketplace.empty.title') }}</div>
        <div class="empty-state-description">{{ t('modelMarketplace.empty.description') }}</div>
      </div>

      <div v-else class="grid grid-cols-1 gap-4 xl:grid-cols-2 2xl:grid-cols-3">
        <article
          v-for="model in filteredModels"
          :key="model.key"
          class="card flex min-h-[260px] flex-col overflow-hidden"
        >
          <div class="h-1" :class="platformAccentBarClass(model.platform)"></div>
          <div class="flex flex-1 flex-col p-5">
            <div class="mb-4 flex items-start justify-between gap-3">
              <div class="min-w-0">
                <div class="flex items-center gap-2">
                  <ModelIcon :model="model.name" size="20px" />
                  <h2 class="truncate font-mono text-base font-semibold text-gray-900 dark:text-white" :title="model.name">
                    {{ model.name }}
                  </h2>
                </div>
                <div class="mt-2 flex flex-wrap items-center gap-2">
                  <span
                    class="inline-flex items-center gap-1 rounded-md border px-2 py-0.5 text-[11px] font-medium uppercase"
                    :class="platformBadgeClass(model.platform)"
                  >
                    {{ platformLabel(model.platform) }}
                  </span>
                  <span class="rounded-md bg-gray-100 px-2 py-0.5 text-[11px] font-medium text-gray-600 dark:bg-dark-700 dark:text-gray-300">
                    {{ t('modelMarketplace.card.channelCount', { count: model.channels.length }) }}
                  </span>
                </div>
              </div>

              <button
                type="button"
                class="btn btn-ghost btn-icon flex-shrink-0"
                :title="t('modelMarketplace.copyModel')"
                @click="copyModelName(model.name)"
              >
                <Icon name="copy" size="sm" />
              </button>
            </div>

            <div class="mb-4 rounded-xl border border-gray-100 bg-gray-50 px-3 py-2 dark:border-dark-700 dark:bg-dark-900/40">
              <div class="mb-2 flex items-center justify-between gap-3">
                <span class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('modelMarketplace.card.pricing') }}</span>
                <SupportedModelChip
                  :model="{ name: model.name, platform: model.platform, pricing: model.pricing }"
                  pricing-key-prefix="availableChannels.pricing"
                  :no-pricing-label="t('availableChannels.noPricing')"
                  :show-platform="false"
                  :platform-hint="model.platform"
                />
              </div>
              <div class="grid grid-cols-2 gap-2 text-xs">
                <div>
                  <div class="text-gray-500 dark:text-gray-400">{{ t('modelMarketplace.card.input') }}</div>
                  <div class="font-semibold text-gray-900 dark:text-white">{{ pricingSummary(model).input }}</div>
                </div>
                <div>
                  <div class="text-gray-500 dark:text-gray-400">{{ t('modelMarketplace.card.output') }}</div>
                  <div class="font-semibold text-gray-900 dark:text-white">{{ pricingSummary(model).output }}</div>
                </div>
              </div>
            </div>

            <div class="space-y-3">
              <div
                v-for="channel in visibleChannels(model)"
                :key="channel.channelName"
                class="rounded-xl border border-gray-100 p-3 dark:border-dark-700"
              >
                <div class="mb-2 flex min-w-0 items-center justify-between gap-2">
                  <div class="min-w-0">
                    <div class="truncate text-sm font-semibold text-gray-900 dark:text-white" :title="channel.channelName">
                      {{ channel.channelName }}
                    </div>
                    <div v-if="channel.description" class="truncate text-xs text-gray-500 dark:text-gray-400" :title="channel.description">
                      {{ channel.description }}
                    </div>
                  </div>
                  <span
                    v-if="channel.exclusiveCount > 0"
                    class="inline-flex flex-shrink-0 items-center gap-1 rounded-md bg-purple-50 px-2 py-0.5 text-[11px] font-medium text-purple-600 dark:bg-purple-900/20 dark:text-purple-300"
                  >
                    <Icon name="shield" size="xs" />
                    {{ t('availableChannels.exclusive') }}
                  </span>
                </div>
                <div class="flex flex-wrap gap-1.5">
                  <GroupBadge
                    v-for="group in channel.groups"
                    :key="`${channel.channelName}-${group.id}`"
                    :name="group.name"
                    :platform="group.platform as GroupPlatform"
                    :subscription-type="(group.subscription_type || 'standard') as SubscriptionType"
                    :rate-multiplier="group.rate_multiplier"
                    :user-rate-multiplier="userGroupRates[group.id] ?? null"
                    always-show-rate
                  />
                </div>
              </div>
            </div>

            <button
              v-if="model.channels.length > CHANNEL_PREVIEW_LIMIT"
              type="button"
              class="mt-3 inline-flex items-center justify-center gap-1 rounded-lg px-3 py-2 text-sm font-medium text-primary-600 hover:bg-primary-50 dark:text-primary-400 dark:hover:bg-primary-900/20"
              @click="toggleExpanded(model.key)"
            >
              <template v-if="expandedModelKeys.has(model.key)">
                {{ t('modelMarketplace.card.showLess') }}
                <Icon name="chevronUp" size="sm" />
              </template>
              <template v-else>
                {{ t('modelMarketplace.card.showMore', { count: model.channels.length - CHANNEL_PREVIEW_LIMIT }) }}
                <Icon name="chevronDown" size="sm" />
              </template>
            </button>
          </div>
        </article>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import ModelIcon from '@/components/common/ModelIcon.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import SupportedModelChip from '@/components/channels/SupportedModelChip.vue'
import userChannelsAPI, {
  type UserAvailableChannel,
  type UserAvailableGroup,
  type UserSupportedModel,
  type UserSupportedModelPricing,
} from '@/api/channels'
import userGroupsAPI from '@/api/groups'
import { useAppStore } from '@/stores/app'
import { useClipboard } from '@/composables/useClipboard'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatScaled } from '@/utils/pricing'
import { platformAccentBarClass, platformBadgeClass, platformLabel } from '@/utils/platformColors'
import { BILLING_MODE_IMAGE, BILLING_MODE_PER_REQUEST, BILLING_MODE_TOKEN } from '@/constants/channel'
import type { GroupPlatform, SubscriptionType } from '@/types'

const ALL_PLATFORMS = '__all__'
const CHANNEL_PREVIEW_LIMIT = 2
const perMillionScale = 1_000_000

type SortMode = 'channels-desc' | 'name-asc' | 'platform-asc'

interface ModelChannel {
  channelName: string
  description: string
  groups: UserAvailableGroup[]
  exclusiveCount: number
}

interface MarketplaceModel {
  key: string
  name: string
  platform: string
  pricing: UserSupportedModelPricing | null
  channels: ModelChannel[]
}

const { t } = useI18n()
const appStore = useAppStore()
const { copyToClipboard } = useClipboard()

const channels = ref<UserAvailableChannel[]>([])
const userGroupRates = ref<Record<number, number>>({})
const loading = ref(false)
const searchQuery = ref('')
const selectedPlatform = ref<string>(ALL_PLATFORMS)
const sortMode = ref<SortMode>('channels-desc')
const expandedModelKeys = ref<Set<string>>(new Set())

const marketplaceModels = computed<MarketplaceModel[]>(() => {
  const modelMap = new Map<string, MarketplaceModel>()

  for (const channel of channels.value) {
    for (const section of channel.platforms) {
      for (const supportedModel of section.supported_models) {
        const modelName = supportedModel.name.trim()
        if (!modelName) continue
        const modelGroups = groupsForSupportedModel(section.groups, supportedModel)
        if (modelGroups.length === 0) continue
        const platform = supportedModel.platform || section.platform
        const key = modelMarketplaceKey(platform, modelName, supportedModel.pricing)
        let entry = modelMap.get(key)
        if (!entry) {
          entry = {
            key,
            name: modelName,
            platform,
            pricing: supportedModel.pricing,
            channels: [],
          }
          modelMap.set(key, entry)
        } else if (!entry.pricing && supportedModel.pricing) {
          entry.pricing = supportedModel.pricing
        }

        const existingChannel = entry.channels.find((item) => item.channelName === channel.name)
        if (existingChannel) {
          existingChannel.groups = mergeGroups(existingChannel.groups, modelGroups)
          existingChannel.exclusiveCount = existingChannel.groups.filter((g) => g.is_exclusive).length
          continue
        }

        entry.channels.push({
          channelName: channel.name,
          description: channel.description || '',
          groups: modelGroups,
          exclusiveCount: modelGroups.filter((g) => g.is_exclusive).length,
        })
      }
    }
  }

  return Array.from(modelMap.values()).map((model) => ({
    ...model,
    channels: model.channels.sort((a, b) => {
      if (b.exclusiveCount !== a.exclusiveCount) return b.exclusiveCount - a.exclusiveCount
      return a.channelName.localeCompare(b.channelName)
    }),
  }))
})

const marketplaceStats = computed(() => {
  const channelNames = new Set(channels.value.map((channel) => channel.name))
  const platforms = new Set(marketplaceModels.value.map((model) => model.platform))
  return {
    models: marketplaceModels.value.length,
    channels: channelNames.size,
    platforms: platforms.size,
  }
})

const platformOptions = computed(() => {
  const counts = new Map<string, number>()
  for (const model of marketplaceModels.value) {
    counts.set(model.platform, (counts.get(model.platform) ?? 0) + 1)
  }

  const options = Array.from(counts.entries())
    .sort(([a], [b]) => platformLabel(a).localeCompare(platformLabel(b)))
    .map(([value, count]) => ({
      value,
      label: platformLabel(value),
      count,
    }))

  return [
    { value: ALL_PLATFORMS, label: t('common.all'), count: marketplaceModels.value.length },
    ...options,
  ]
})


function modelMarketplaceKey(
  platform: string,
  modelName: string,
  pricing: UserSupportedModelPricing | null,
): string {
  return `${platform}::${modelName}::${pricingSignature(pricing)}`.toLowerCase()
}

function pricingSignature(pricing: UserSupportedModelPricing | null): string {
  if (!pricing) return 'no-pricing'
  const intervals = [...(pricing.intervals || [])]
    .sort((a, b) => {
      const minResult = a.min_tokens - b.min_tokens
      if (minResult !== 0) return minResult
      const aMax = a.max_tokens ?? Number.POSITIVE_INFINITY
      const bMax = b.max_tokens ?? Number.POSITIVE_INFINITY
      const maxResult = aMax - bMax
      if (maxResult !== 0) return maxResult
      return (a.tier_label || '').localeCompare(b.tier_label || '')
    })
    .map((iv) => [
      iv.min_tokens,
      iv.max_tokens ?? '∞',
      iv.tier_label || '',
      pricePart(iv.input_price),
      pricePart(iv.output_price),
      pricePart(iv.cache_write_price),
      pricePart(iv.cache_read_price),
      pricePart(iv.per_request_price),
    ].join(':'))
    .join('|')

  return [
    pricing.billing_mode || BILLING_MODE_TOKEN,
    pricePart(pricing.input_price),
    pricePart(pricing.output_price),
    pricePart(pricing.cache_write_price),
    pricePart(pricing.cache_read_price),
    pricePart(pricing.image_output_price),
    pricePart(pricing.per_request_price),
    intervals,
  ].join(';')
}

function pricePart(value: number | null | undefined): string {
  return value == null ? 'null' : String(value)
}

const filteredModels = computed(() => {
  const q = searchQuery.value.trim().toLowerCase()
  const filtered = marketplaceModels.value.filter((model) => {
    const platformHit = selectedPlatform.value === ALL_PLATFORMS || model.platform === selectedPlatform.value
    if (!platformHit) return false
    if (!q) return true
    return (
      model.name.toLowerCase().includes(q) ||
      model.platform.toLowerCase().includes(q) ||
      model.channels.some((channel) =>
        channel.channelName.toLowerCase().includes(q) ||
        channel.description.toLowerCase().includes(q) ||
        channel.groups.some((group) => group.name.toLowerCase().includes(q)),
      )
    )
  })

  return filtered.sort((a, b) => {
    if (sortMode.value === 'name-asc') return a.name.localeCompare(b.name)
    if (sortMode.value === 'platform-asc') {
      const platformResult = platformLabel(a.platform).localeCompare(platformLabel(b.platform))
      return platformResult || a.name.localeCompare(b.name)
    }
    const channelResult = b.channels.length - a.channels.length
    return channelResult || a.name.localeCompare(b.name)
  })
})

function mergeGroups(left: UserAvailableGroup[], right: UserAvailableGroup[]): UserAvailableGroup[] {
  const seen = new Set<number>()
  const merged: UserAvailableGroup[] = []
  for (const group of [...left, ...right]) {
    if (seen.has(group.id)) continue
    seen.add(group.id)
    merged.push(group)
  }
  return merged.sort((a, b) => Number(b.is_exclusive) - Number(a.is_exclusive) || a.name.localeCompare(b.name))
}

function groupsForSupportedModel(
  groups: UserAvailableGroup[],
  model: UserSupportedModel,
): UserAvailableGroup[] {
  const allowedGroupIds = model.group_ids
  if (!allowedGroupIds || allowedGroupIds.length === 0) {
    return [...groups]
  }
  const allowed = new Set(allowedGroupIds)
  return groups.filter((group) => allowed.has(group.id))
}

function visibleChannels(model: MarketplaceModel): ModelChannel[] {
  return expandedModelKeys.value.has(model.key)
    ? model.channels
    : model.channels.slice(0, CHANNEL_PREVIEW_LIMIT)
}

function toggleExpanded(modelKey: string) {
  const next = new Set(expandedModelKeys.value)
  if (next.has(modelKey)) {
    next.delete(modelKey)
  } else {
    next.add(modelKey)
  }
  expandedModelKeys.value = next
}

function pricingSummary(model: Pick<UserSupportedModel, 'pricing'>): { input: string; output: string } {
  const pricing = model.pricing
  if (!pricing) {
    return {
      input: t('availableChannels.noPricing'),
      output: t('availableChannels.noPricing'),
    }
  }

  if (pricing.billing_mode === BILLING_MODE_PER_REQUEST) {
    const value = `${formatScaled(pricing.per_request_price, 1)} ${t('availableChannels.pricing.unitPerRequest')}`
    return { input: value, output: value }
  }

  if (pricing.billing_mode === BILLING_MODE_IMAGE) {
    const value = `${formatScaled(pricing.image_output_price, 1)} ${t('availableChannels.pricing.unitPerRequest')}`
    return { input: '-', output: value }
  }

  if (pricing.billing_mode === BILLING_MODE_TOKEN) {
    return {
      input: `${formatScaled(pricing.input_price, perMillionScale)} ${t('availableChannels.pricing.unitPerMillion')}`,
      output: `${formatScaled(pricing.output_price, perMillionScale)} ${t('availableChannels.pricing.unitPerMillion')}`,
    }
  }

  return { input: '-', output: '-' }
}

async function copyModelName(modelName: string) {
  await copyToClipboard(modelName, t('modelMarketplace.modelCopied'))
}

async function loadChannels() {
  loading.value = true
  try {
    const [list, rates] = await Promise.all([
      userChannelsAPI.getAvailable(),
      userGroupsAPI.getUserGroupRates().catch((err: unknown) => {
        console.error('Failed to load user group rates:', err)
        return {} as Record<number, number>
      }),
    ])
    channels.value = list
    userGroupRates.value = rates
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('common.error')))
  } finally {
    loading.value = false
  }
}

onMounted(loadChannels)
</script>
