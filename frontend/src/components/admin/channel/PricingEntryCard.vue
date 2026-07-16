<template>
  <div class="rounded-lg border border-gray-200 bg-gray-50 p-3 dark:border-dark-600 dark:bg-dark-800">
    <!-- Collapsed summary header (clickable) -->
    <div
      class="flex cursor-pointer select-none items-center gap-2"
      @click="collapsed = !collapsed"
    >
      <Icon
        :name="collapsed ? 'chevronRight' : 'chevronDown'"
        size="sm"
        :stroke-width="2"
        class="flex-shrink-0 text-gray-400 transition-transform duration-200"
      />

      <!-- Summary: model tags + billing badge -->
      <div v-if="collapsed" class="flex min-w-0 flex-1 items-center gap-2 overflow-hidden">
        <!-- Compact model tags (show first 3) -->
        <div class="flex min-w-0 flex-1 flex-wrap items-center gap-1">
          <span
            v-for="(m, i) in entry.models.slice(0, 3)"
            :key="i"
            class="inline-flex shrink-0 rounded px-1.5 py-0.5 text-xs"
            :class="getPlatformTagClass(props.platform || '')"
          >
            {{ m }}
          </span>
          <span
            v-if="entry.models.length > 3"
            class="whitespace-nowrap text-xs text-gray-400"
          >
            +{{ entry.models.length - 3 }}
          </span>
          <span
            v-if="entry.models.length === 0"
            class="text-xs italic text-gray-400"
          >
            {{ t('admin.channels.form.noModels') }}
          </span>
        </div>

        <!-- Billing mode badge -->
        <span
          class="flex-shrink-0 rounded-full bg-primary-100 px-2 py-0.5 text-xs font-medium text-primary-700 dark:bg-primary-900/30 dark:text-primary-300"
        >
          {{ billingModeLabel }}
        </span>
        <span
          v-if="excludedGroupCount > 0"
          class="flex-shrink-0 rounded-full bg-amber-100 px-2 py-0.5 text-xs font-medium text-amber-700 dark:bg-amber-900/30 dark:text-amber-300"
        >
          {{ t('admin.channels.form.excludedGroupsCount', { count: excludedGroupCount }, `排除 ${excludedGroupCount} 组`) }}
        </span>
      </div>

      <!-- Expanded: show the label "Pricing Entry" or similar -->
      <div v-else class="flex-1 text-xs font-medium text-gray-500 dark:text-gray-400">
        {{ t('admin.channels.form.pricingEntry') }}
      </div>

      <!-- Remove button (always visible, stop propagation) -->
      <button
        type="button"
        @click.stop="emit('remove')"
        class="flex-shrink-0 rounded p-1 text-gray-400 hover:text-red-500"
      >
        <Icon name="trash" size="sm" />
      </button>
    </div>

    <!-- Expandable content with transition -->
    <div
      class="collapsible-content"
      :class="{ 'collapsible-content--collapsed': collapsed }"
    >
      <div class="collapsible-inner">
        <!-- Header: Models + Billing Mode -->
        <div class="mt-3 flex items-start gap-2">
          <div class="flex-1">
            <label class="text-xs font-medium text-gray-500 dark:text-gray-400">
              {{ t('admin.channels.form.models') }} <span class="text-red-500">*</span>
            </label>
            <ModelTagInput
              :models="entry.models"
              :platform="props.platform"
              @update:models="onModelsUpdate($event)"
              :placeholder="t('admin.channels.form.modelsPlaceholder')"
              class="mt-1"
            />
          </div>
          <div class="w-40">
            <label class="text-xs font-medium text-gray-500 dark:text-gray-400">
              {{ t('admin.channels.form.billingMode') }}
            </label>
            <Select
              :modelValue="entry.billing_mode"
              @update:modelValue="emit('update', { ...entry, billing_mode: $event as BillingMode, intervals: [] })"
              :options="billingModeOptions"
              class="mt-1"
            />
          </div>
        </div>

        <div v-if="groupOptions.length > 0" class="mt-3">
          <div class="flex items-center justify-between gap-3">
            <label class="text-xs font-medium text-gray-500 dark:text-gray-400">
              {{ t('admin.channels.form.excludedGroups', '排除分组') }}
            </label>
            <span class="text-[11px] text-gray-400">
              {{ t('admin.channels.form.excludedGroupsHint', '勾选后，这些分组不能使用本条模型') }}
            </span>
          </div>
          <div class="mt-1 flex flex-wrap gap-1.5">
            <label
              v-for="group in groupOptions"
              :key="group.id"
              class="inline-flex cursor-pointer items-center gap-1.5 rounded-md border px-2 py-1 text-xs transition-colors"
              :class="isGroupExcluded(group.id)
                ? 'border-amber-300 bg-amber-50 text-amber-700 dark:border-amber-700 dark:bg-amber-900/20 dark:text-amber-300'
                : 'border-gray-200 bg-white text-gray-600 hover:bg-gray-50 dark:border-dark-600 dark:bg-dark-700 dark:text-gray-300 dark:hover:bg-dark-600'"
            >
              <input
                type="checkbox"
                class="h-3 w-3 rounded border-gray-300 text-amber-600 focus:ring-amber-500"
                :checked="isGroupExcluded(group.id)"
                @change="toggleExcludedGroup(group.id)"
              />
              <span class="font-medium">{{ group.name }}</span>
              <span v-if="group.rate_multiplier != null" class="rounded-full bg-black/5 px-1 py-0 text-[10px] dark:bg-white/10">
                {{ group.rate_multiplier }}x
              </span>
            </label>
          </div>
        </div>

        <div v-if="entry.models.length > 0" class="mt-2 flex flex-wrap items-center gap-2">
          <button
            type="button"
            @click="fetchOfficialPricing"
            :disabled="!canFetchOfficialPricing || pricingLookupState === 'loading'"
            class="inline-flex h-7 items-center gap-1.5 rounded-md border border-gray-200 bg-white px-2.5 text-xs font-medium text-gray-600 transition-colors hover:border-primary-300 hover:text-primary-600 disabled:cursor-not-allowed disabled:opacity-50 dark:border-dark-600 dark:bg-dark-700 dark:text-gray-300 dark:hover:border-primary-600 dark:hover:text-primary-300"
            :title="canFetchOfficialPricing
              ? t('admin.channels.form.officialPricingOverwriteTitle', '从默认定价库获取并覆盖当前价格')
              : t('admin.channels.form.officialPricingNoModel', '请先添加完整模型名')"
          >
            <Icon
              :name="pricingLookupState === 'loading' ? 'refresh' : 'dollar'"
              size="xs"
              :class="{ 'animate-spin': pricingLookupState === 'loading' }"
            />
            <span>
              {{ pricingLookupState === 'loading'
                ? t('admin.channels.form.fetchingOfficialPricing', '获取中...')
                : t('admin.channels.form.fetchOfficialPricing', '获取官方定价') }}
            </span>
          </button>
          <span
            v-if="pricingLookupMessage"
            class="inline-flex min-w-0 items-center gap-1 text-xs"
            :class="pricingLookupMessageClass"
          >
            <Icon :name="pricingLookupIcon" size="xs" class="flex-shrink-0" />
            <span class="truncate">{{ pricingLookupMessage }}</span>
          </span>
        </div>

        <!-- Token mode -->
        <div v-if="entry.billing_mode === 'token'">
          <!-- Default prices (fallback when no interval matches) -->
          <label class="mt-3 block text-xs font-medium text-gray-500 dark:text-gray-400">
            {{ t('admin.channels.form.defaultPrices') }}
            <span class="ml-1 font-normal text-gray-400">$/MTok</span>
          </label>
          <div class="mt-1 grid grid-cols-2 gap-2 sm:grid-cols-6">
            <div>
              <label class="text-xs text-gray-400">{{ t('admin.channels.form.inputPrice') }}</label>
              <input :value="entry.input_price" @input="emitField('input_price', ($event.target as HTMLInputElement).value)"
                type="number" step="any" min="0" class="input mt-0.5 text-sm" :placeholder="t('admin.channels.form.pricePlaceholder')" />
            </div>
            <div>
              <label class="text-xs text-gray-400">{{ t('admin.channels.form.outputPrice') }}</label>
              <input :value="entry.output_price" @input="emitField('output_price', ($event.target as HTMLInputElement).value)"
                type="number" step="any" min="0" class="input mt-0.5 text-sm" :placeholder="t('admin.channels.form.pricePlaceholder')" />
            </div>
            <div>
              <label class="text-xs text-gray-400">{{ t('admin.channels.form.cacheWritePrice') }}</label>
              <input :value="entry.cache_write_price" @input="emitField('cache_write_price', ($event.target as HTMLInputElement).value)"
                type="number" step="any" min="0" class="input mt-0.5 text-sm" :placeholder="t('admin.channels.form.pricePlaceholder')" />
            </div>
            <div>
              <label class="text-xs text-gray-400">{{ t('admin.channels.form.cacheReadPrice') }}</label>
              <input :value="entry.cache_read_price" @input="emitField('cache_read_price', ($event.target as HTMLInputElement).value)"
                type="number" step="any" min="0" class="input mt-0.5 text-sm" :placeholder="t('admin.channels.form.pricePlaceholder')" />
            </div>
            <div>
              <label class="text-xs text-gray-400">{{ t('admin.channels.form.imageInputPrice') }}</label>
              <input :value="entry.image_input_price" @input="emitField('image_input_price', ($event.target as HTMLInputElement).value)"
                type="number" step="any" min="0" class="input mt-0.5 text-sm" :placeholder="t('admin.channels.form.pricePlaceholder')" />
            </div>
            <div>
              <label class="text-xs text-gray-400">{{ t('admin.channels.form.imageTokenPrice') }}</label>
              <input :value="entry.image_output_price" @input="emitField('image_output_price', ($event.target as HTMLInputElement).value)"
                type="number" step="any" min="0" class="input mt-0.5 text-sm" :placeholder="t('admin.channels.form.pricePlaceholder')" />
            </div>
          </div>

          <!-- Token intervals -->
          <div class="mt-3">
            <div class="flex items-center justify-between">
              <label class="text-xs font-medium text-gray-500 dark:text-gray-400">
                {{ t('admin.channels.form.intervals') }}
                <span class="ml-1 font-normal text-gray-400">(min, max]</span>
              </label>
              <button type="button" @click="addInterval" class="text-xs text-primary-600 hover:text-primary-700">
                + {{ t('admin.channels.form.addInterval') }}
              </button>
            </div>
            <div v-if="entry.intervals && entry.intervals.length > 0" class="mt-2 space-y-2">
              <IntervalRow
                v-for="(iv, idx) in entry.intervals"
                :key="idx"
                :interval="iv"
                :mode="entry.billing_mode"
                @update="updateInterval(idx, $event)"
                @remove="removeInterval(idx)"
              />
            </div>
          </div>
        </div>

        <!-- Per-request mode -->
        <div v-else-if="entry.billing_mode === 'per_request'">
          <!-- Default per-request price -->
          <label class="mt-3 block text-xs font-medium text-gray-500 dark:text-gray-400">
            {{ t('admin.channels.form.defaultPerRequestPrice') }}
            <span class="ml-1 font-normal text-gray-400">$</span>
          </label>
          <div class="mt-1 w-48">
            <input :value="entry.per_request_price" @input="emitField('per_request_price', ($event.target as HTMLInputElement).value)"
              type="number" step="any" min="0" class="input text-sm" :placeholder="t('admin.channels.form.pricePlaceholder')" />
          </div>

          <!-- Tiers -->
          <div class="mt-3 flex items-center justify-between">
            <label class="text-xs font-medium text-gray-500 dark:text-gray-400">
              {{ t('admin.channels.form.requestTiers') }}
            </label>
            <button type="button" @click="addInterval" class="text-xs text-primary-600 hover:text-primary-700">
              + {{ t('admin.channels.form.addTier') }}
            </button>
          </div>
          <div v-if="entry.intervals && entry.intervals.length > 0" class="mt-2 space-y-2">
            <IntervalRow
              v-for="(iv, idx) in entry.intervals"
              :key="idx"
              :interval="iv"
              :mode="entry.billing_mode"
              @update="updateInterval(idx, $event)"
              @remove="removeInterval(idx)"
            />
          </div>
          <div v-else class="mt-2 rounded border border-dashed border-gray-300 p-3 text-center text-xs text-gray-400 dark:border-dark-500">
            {{ t('admin.channels.form.noTiersYet') }}
          </div>
        </div>

        <!-- Image mode -->
        <div v-else-if="entry.billing_mode === 'image'">
          <!-- Default image price (per-request, same as per_request mode) -->
          <label class="mt-3 block text-xs font-medium text-gray-500 dark:text-gray-400">
            {{ t('admin.channels.form.defaultImagePrice') }}
            <span class="ml-1 font-normal text-gray-400">$</span>
          </label>
          <div class="mt-1 w-48">
            <input :value="entry.per_request_price" @input="emitField('per_request_price', ($event.target as HTMLInputElement).value)"
              type="number" step="any" min="0" class="input text-sm" :placeholder="t('admin.channels.form.pricePlaceholder')" />
          </div>

          <!-- Image tiers -->
          <div class="mt-3 flex items-center justify-between">
            <label class="text-xs font-medium text-gray-500 dark:text-gray-400">
              {{ t('admin.channels.form.imageTiers') }}
            </label>
            <button type="button" @click="addImageTier" class="text-xs text-primary-600 hover:text-primary-700">
              + {{ t('admin.channels.form.addTier') }}
            </button>
          </div>
          <div v-if="entry.intervals && entry.intervals.length > 0" class="mt-2 space-y-2">
            <IntervalRow
              v-for="(iv, idx) in entry.intervals"
              :key="idx"
              :interval="iv"
              :mode="entry.billing_mode"
              @update="updateInterval(idx, $event)"
              @remove="removeInterval(idx)"
            />
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import IntervalRow from './IntervalRow.vue'
import ModelTagInput from './ModelTagInput.vue'
import type { PricingFormEntry, IntervalFormEntry } from './types'
import { perTokenToMTok, getPlatformTagClass } from './types'
import type { BillingMode, ModelDefaultPricing } from '@/api/admin/channels'
import channelsAPI from '@/api/admin/channels'

const { t } = useI18n()

const props = defineProps<{
  entry: PricingFormEntry
  platform?: string
  groupOptions?: PricingGroupOption[]
}>()

const emit = defineEmits<{
  update: [entry: PricingFormEntry]
  remove: []
}>()

type PricingLookupState = 'idle' | 'loading' | 'success' | 'empty' | 'error'
interface PricingGroupOption {
  id: number
  name: string
  rate_multiplier?: number
}
type PriceField =
  | 'input_price'
  | 'output_price'
  | 'cache_write_price'
  | 'cache_read_price'
  | 'image_input_price'
  | 'image_output_price'
  | 'per_request_price'
type PricePatch = Partial<Record<PriceField, number | null>>

const tokenPriceFields: PriceField[] = [
  'input_price',
  'output_price',
  'cache_write_price',
  'cache_read_price',
  'image_input_price',
  'image_output_price',
]
const requestPriceFields: PriceField[] = ['per_request_price']

// Collapse state: entries with existing models default to collapsed
const collapsed = ref(props.entry.models.length > 0)
const pricingLookupState = ref<PricingLookupState>('idle')
const pricingLookupMessage = ref('')
let pricingLookupRunId = 0

const billingModeOptions = computed(() => [
  { value: 'token', label: t('admin.channels.billingMode.token') },
  { value: 'per_request', label: t('admin.channels.billingMode.perRequest') },
  { value: 'image', label: t('admin.channels.billingMode.image') }
])

const billingModeLabel = computed(() => {
  const opt = billingModeOptions.value.find(o => o.value === props.entry.billing_mode)
  return opt ? opt.label : props.entry.billing_mode
})

const lookupModels = computed(() => modelsForLookup(props.entry.models))
const canFetchOfficialPricing = computed(() => lookupModels.value.length > 0)
const groupOptions = computed(() => props.groupOptions || [])
const excludedGroupCount = computed(() => (props.entry.excluded_group_ids || []).length)

const pricingLookupMessageClass = computed(() => {
  switch (pricingLookupState.value) {
    case 'success':
      return 'text-emerald-600 dark:text-emerald-400'
    case 'empty':
      return 'text-amber-600 dark:text-amber-400'
    case 'error':
      return 'text-red-600 dark:text-red-400'
    default:
      return 'text-gray-500 dark:text-gray-400'
  }
})

const pricingLookupIcon = computed(() => {
  switch (pricingLookupState.value) {
    case 'success':
      return 'checkCircle'
    case 'empty':
      return 'infoCircle'
    case 'error':
      return 'exclamationCircle'
    default:
      return 'infoCircle'
  }
})

function emitField(field: keyof PricingFormEntry, value: string) {
  emit('update', { ...props.entry, [field]: value === '' ? null : value })
}

function isGroupExcluded(groupId: number): boolean {
  return (props.entry.excluded_group_ids || []).includes(groupId)
}

function toggleExcludedGroup(groupId: number) {
  const excluded = new Set(props.entry.excluded_group_ids || [])
  if (excluded.has(groupId)) {
    excluded.delete(groupId)
  } else {
    excluded.add(groupId)
  }
  emit('update', { ...props.entry, excluded_group_ids: [...excluded] })
}

function addInterval() {
  const intervals = [...(props.entry.intervals || [])]
  intervals.push({
    min_tokens: 0, max_tokens: null, tier_label: '',
    input_price: null, output_price: null, cache_write_price: null,
    cache_read_price: null, per_request_price: null,
    sort_order: intervals.length
  })
  emit('update', { ...props.entry, intervals })
}

function addImageTier() {
  const intervals = [...(props.entry.intervals || [])]
  const labels = ['1K', '2K', '4K', 'HD']
  intervals.push({
    min_tokens: 0, max_tokens: null, tier_label: labels[intervals.length] || '',
    input_price: null, output_price: null, cache_write_price: null,
    cache_read_price: null, per_request_price: null,
    sort_order: intervals.length
  })
  emit('update', { ...props.entry, intervals })
}

function updateInterval(idx: number, updated: IntervalFormEntry) {
  const intervals = [...(props.entry.intervals || [])]
  intervals[idx] = updated
  emit('update', { ...props.entry, intervals })
}

function removeInterval(idx: number) {
  const intervals = [...(props.entry.intervals || [])]
  intervals.splice(idx, 1)
  emit('update', { ...props.entry, intervals })
}

async function onModelsUpdate(newModels: string[]) {
  pricingLookupRunId += 1
  pricingLookupState.value = 'idle'
  pricingLookupMessage.value = ''

  const oldModels = props.entry.models
  emit('update', { ...props.entry, models: newModels })

  // 新增完整模型名时自动补空白价格，避免覆盖用户已手动填写的字段。
  const addedModels = newModels.filter(m => !oldModels.includes(m))
  const addedLookupModels = modelsForLookup(addedModels)
  if (addedLookupModels.length === 0) return

  const nextEntry = { ...props.entry, models: newModels }
  if (!hasBlankRelevantPriceFields(nextEntry)) return

  void applyOfficialPricingForModels(addedLookupModels, {
    overwrite: false,
    modelsOverride: newModels,
  })
}

function fetchOfficialPricing() {
  void applyOfficialPricingForModels(lookupModels.value, { overwrite: true })
}

async function applyOfficialPricingForModels(
  models: string[],
  options: { overwrite: boolean; modelsOverride?: string[] }
) {
  if (models.length === 0) {
    pricingLookupState.value = 'empty'
    pricingLookupMessage.value = t('admin.channels.form.officialPricingNoModel', '请先添加完整模型名')
    return
  }
  const runId = ++pricingLookupRunId
  pricingLookupState.value = 'loading'
  pricingLookupMessage.value = ''

  try {
    for (const model of models) {
      const result = await channelsAPI.getModelDefaultPricing(model)
      if (runId !== pricingLookupRunId) return
      if (!result.found) continue

      const patch = pricingPatchForMode(result, props.entry.billing_mode)
      const intervals = officialIntervalsForMode(result, props.entry.billing_mode)
      if (!patchHasValue(patch) && intervals.length === 0) continue

      const baseEntry: PricingFormEntry = {
        ...props.entry,
        models: options.modelsOverride ?? props.entry.models,
      }
      emit('update', applyPricingPatch(baseEntry, patch, intervals, options.overwrite))
      pricingLookupState.value = 'success'
      pricingLookupMessage.value = t(
        'admin.channels.form.officialPricingFilled',
        { model },
        `已按「${model}」填充官方定价`
      )
      return
    }

    pricingLookupState.value = 'empty'
    pricingLookupMessage.value = t(
      'admin.channels.form.officialPricingNotFound',
      '未找到这些模型的官方定价'
    )
  } catch {
    if (runId !== pricingLookupRunId) return
    pricingLookupState.value = 'error'
    pricingLookupMessage.value = t(
      'admin.channels.form.officialPricingFailed',
      '获取官方定价失败'
    )
  }
}

function pricingPatchForMode(result: ModelDefaultPricing, mode: BillingMode): PricePatch {
  if (mode === 'token') {
    return {
      input_price: perTokenToMTok(result.input_price ?? null),
      output_price: perTokenToMTok(result.output_price ?? null),
      cache_write_price: perTokenToMTok(result.cache_write_price ?? null),
      cache_read_price: perTokenToMTok(result.cache_read_price ?? null),
      image_input_price: perTokenToMTok(result.image_input_price ?? null),
      image_output_price: perTokenToMTok(result.image_output_price ?? null),
    }
  }

  if (result.per_request_price == null) {
    return {}
  }
  return { per_request_price: result.per_request_price }
}

function officialIntervalsForMode(result: ModelDefaultPricing, mode: BillingMode): IntervalFormEntry[] {
  if (mode !== 'token') return []

  const threshold = result.long_context_input_threshold
  const inputMultiplier = result.long_context_input_multiplier
  const outputMultiplier = result.long_context_output_multiplier
  if (!threshold || threshold <= 0) return []

  const inputPrice = perTokenToMTok(result.input_price ?? null)
  const outputPrice = perTokenToMTok(result.output_price ?? null)
  const cacheWritePrice = perTokenToMTok(result.cache_write_price ?? null)
  const cacheReadPrice = perTokenToMTok(result.cache_read_price ?? null)
  const directLongInputPrice = perTokenToMTok(result.long_context_input_price ?? null)
  const directLongOutputPrice = perTokenToMTok(result.long_context_output_price ?? null)
  const directLongCacheWritePrice = perTokenToMTok(result.long_context_cache_write_price ?? null)
  const directLongCacheReadPrice = perTokenToMTok(result.long_context_cache_read_price ?? null)
  const longInputPrice = directLongInputPrice ?? multipliedPrice(inputPrice, inputMultiplier)
  const longOutputPrice = directLongOutputPrice ?? multipliedPrice(outputPrice, outputMultiplier)
  const longCacheWritePrice = directLongCacheWritePrice ?? cacheWritePrice
  const longCacheReadPrice = directLongCacheReadPrice ?? cacheReadPrice

  if (
    longInputPrice == null &&
    longOutputPrice == null &&
    longCacheWritePrice == null &&
    longCacheReadPrice == null
  ) {
    return []
  }

  return [{
    min_tokens: threshold,
    max_tokens: null,
    tier_label: t('admin.channels.form.officialLongContextTier', '官方长上下文'),
    input_price: longInputPrice,
    output_price: longOutputPrice,
    cache_write_price: longCacheWritePrice,
    cache_read_price: longCacheReadPrice,
    per_request_price: null,
    sort_order: 0,
  }]
}

function multipliedPrice(price: number | null, multiplier?: number): number | null {
  if (price == null || !multiplier || multiplier <= 1) return null
  return normalizeDisplayPrice(price * multiplier)
}

function applyPricingPatch(
  entry: PricingFormEntry,
  patch: PricePatch,
  officialIntervals: IntervalFormEntry[],
  overwrite: boolean
): PricingFormEntry {
  const next: PricingFormEntry = { ...entry }
  for (const field of [...tokenPriceFields, ...requestPriceFields]) {
    const value = patch[field]
    if (value === null || value === undefined) continue
    if (overwrite || isBlankPrice(next[field])) {
      next[field] = value
    }
  }
  if (entry.billing_mode === 'token' && officialIntervals.length > 0) {
    if (overwrite || !next.intervals || next.intervals.length === 0) {
      next.intervals = officialIntervals.map((interval, index) => ({
        ...interval,
        sort_order: index,
      }))
    }
  }
  return next
}

function patchHasValue(patch: PricePatch): boolean {
  return Object.values(patch).some(value => !isBlankPrice(value))
}

function hasBlankRelevantPriceFields(entry: PricingFormEntry): boolean {
  const fields = entry.billing_mode === 'token' ? tokenPriceFields : requestPriceFields
  return fields.some(field => isBlankPrice(entry[field]))
}

function isBlankPrice(value: unknown): boolean {
  return value === null || value === undefined || value === ''
}

function normalizeDisplayPrice(value: number): number {
  return parseFloat(value.toPrecision(10))
}

function modelsForLookup(models: string[]): string[] {
  const seen = new Set<string>()
  const result: string[] = []

  for (const model of models) {
    const normalized = model.trim()
    if (!normalized || normalized.includes('*')) continue

    const dedupeKey = normalized.toLowerCase()
    if (seen.has(dedupeKey)) continue
    seen.add(dedupeKey)
    result.push(normalized)
  }

  return result
}
</script>

<style scoped>
.collapsible-content {
  display: grid;
  grid-template-rows: 1fr;
  transition: grid-template-rows 0.25s ease;
}

.collapsible-content--collapsed {
  grid-template-rows: 0fr;
}

.collapsible-inner {
  overflow: hidden;
}
</style>
