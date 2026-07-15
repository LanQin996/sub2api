<template>
  <section class="card overflow-hidden" aria-labelledby="token-activity-title">
    <div class="grid grid-cols-2 border-b border-gray-200 dark:border-dark-600 lg:grid-cols-4">
      <div v-for="metric in metrics" :key="metric.label" class="px-4 py-4 text-center lg:px-6">
        <p class="text-xl font-semibold text-gray-900 dark:text-white">{{ metric.value }}</p>
        <p class="mt-1 text-xs font-medium text-gray-500 dark:text-gray-400">{{ metric.label }}</p>
      </div>
    </div>

    <div class="p-4 sm:p-5">
      <div class="mb-4 flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 id="token-activity-title" class="text-sm font-semibold text-gray-900 dark:text-white">
            {{ t('dashboard.tokenActivity.title') }}
          </h2>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
            {{ t('dashboard.tokenActivity.updatedThrough', { date: activity.data_through_date, timezone: activity.timezone }) }}
          </p>
        </div>
        <div class="inline-flex rounded-md bg-gray-100 p-1 dark:bg-dark-700" role="tablist">
          <button
            v-for="option in viewOptions"
            :key="option.value"
            type="button"
            role="tab"
            :aria-selected="view === option.value"
            class="rounded px-3 py-1.5 text-xs font-medium transition-colors"
            :class="view === option.value
              ? 'bg-white text-gray-900 shadow-sm dark:bg-dark-600 dark:text-white'
              : 'text-gray-500 hover:text-gray-900 dark:text-gray-400 dark:hover:text-white'"
            @click="view = option.value"
          >
            {{ option.label }}
          </button>
        </div>
      </div>

      <div class="overflow-x-auto pb-2">
        <div :style="gridContainerStyle">
          <div v-if="view === 'day'" class="mb-2 grid h-4 text-[10px] text-gray-400" :style="monthLabelStyle">
            <span
              v-for="label in monthLabels"
              :key="label.key"
              :style="{ gridColumn: label.column }"
              class="whitespace-nowrap"
            >{{ label.text }}</span>
          </div>
          <div class="grid gap-1" :style="cellGridStyle" role="grid" :aria-label="t('dashboard.tokenActivity.title')">
            <template v-for="cell in visibleCells" :key="cell.key">
              <span v-if="cell.placeholder" class="h-3.5 w-3.5" aria-hidden="true" />
              <button
                v-else
                type="button"
                class="h-3.5 w-3.5 rounded-[3px] outline-none ring-offset-1 focus:ring-2 focus:ring-blue-500 dark:ring-offset-dark-800"
                :class="levelClass(cell.tokens)"
                :title="cell.tooltip"
                :aria-label="cell.tooltip"
              />
            </template>
          </div>
        </div>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { TokenActivityResponse } from '@/api/usage'

type ActivityView = 'day' | 'week' | 'month'
type ActivityCell = { key: string; start: Date; end: Date; tokens: number; tooltip: string; placeholder?: boolean }

const props = defineProps<{ activity: TokenActivityResponse }>()
const { t, locale } = useI18n()
const view = ref<ActivityView>('day')

const parseDate = (value: string): Date => {
  const [year, month, day] = value.split('-').map(Number)
  return new Date(year, month - 1, day)
}
const dateKey = (date: Date): string => {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}
const addDays = (date: Date, days: number): Date => {
  const result = new Date(date)
  result.setDate(result.getDate() + days)
  return result
}
const mondayOf = (date: Date): Date => addDays(date, -((date.getDay() + 6) % 7))
const formatPeriod = (start: Date, end: Date): string => {
  const formatter = new Intl.DateTimeFormat(locale.value, { year: 'numeric', month: 'short', day: 'numeric' })
  if (dateKey(start) === dateKey(end)) return formatter.format(start)
  return `${formatter.format(start)} - ${formatter.format(end)}`
}
const formatTokens = (value: number): string => new Intl.NumberFormat(locale.value, { notation: 'compact', maximumFractionDigits: 1 }).format(value)

const tokensByDate = computed(() => new Map(props.activity.days.map(item => [item.date, item.total_tokens])))
const startDate = computed(() => parseDate(props.activity.start_date))
const endDate = computed(() => parseDate(props.activity.end_date))
const dataThrough = computed(() => parseDate(props.activity.data_through_date))

const dailyCells = computed<ActivityCell[]>(() => {
  const gridStart = mondayOf(startDate.value)
  const gridEnd = addDays(mondayOf(endDate.value), 6)
  const cells: ActivityCell[] = []
  for (let day = gridStart; day <= gridEnd; day = addDays(day, 1)) {
    const key = dateKey(day)
    if (day < startDate.value || day > endDate.value) {
      cells.push({ key, start: day, end: day, tokens: 0, tooltip: '', placeholder: true })
      continue
    }
    const tokens = tokensByDate.value.get(key) ?? 0
    const unsettled = day > dataThrough.value
    cells.push({
      key,
      start: day,
      end: day,
      tokens,
      tooltip: unsettled
        ? `${formatPeriod(day, day)} · ${t('dashboard.tokenActivity.unsettled')}`
        : `${formatPeriod(day, day)} · ${t('dashboard.tokenActivity.tokenCount', { count: formatTokens(tokens) })}`,
    })
  }
  return cells
})

const weeklyCells = computed<ActivityCell[]>(() => {
  const cells: ActivityCell[] = []
  for (let week = mondayOf(startDate.value); week <= endDate.value; week = addDays(week, 7)) {
    const rangeStart = week < startDate.value ? startDate.value : week
    const sunday = addDays(week, 6)
    const rangeEnd = sunday > endDate.value ? endDate.value : sunday
    let tokens = 0
    for (let day = rangeStart; day <= rangeEnd; day = addDays(day, 1)) tokens += tokensByDate.value.get(dateKey(day)) ?? 0
    cells.push({ key: dateKey(week), start: rangeStart, end: rangeEnd, tokens, tooltip: `${formatPeriod(rangeStart, rangeEnd)} · ${t('dashboard.tokenActivity.tokenCount', { count: formatTokens(tokens) })}` })
  }
  return cells
})

const monthlyCells = computed<ActivityCell[]>(() => {
  const cells: ActivityCell[] = []
  for (let month = new Date(startDate.value.getFullYear(), startDate.value.getMonth(), 1); month <= endDate.value; month = new Date(month.getFullYear(), month.getMonth() + 1, 1)) {
    const monthEnd = new Date(month.getFullYear(), month.getMonth() + 1, 0)
    const rangeEnd = monthEnd > endDate.value ? endDate.value : monthEnd
    let tokens = 0
    for (let day = month; day <= rangeEnd; day = addDays(day, 1)) tokens += tokensByDate.value.get(dateKey(day)) ?? 0
    cells.push({ key: dateKey(month), start: month, end: rangeEnd, tokens, tooltip: `${formatPeriod(month, rangeEnd)} · ${t('dashboard.tokenActivity.tokenCount', { count: formatTokens(tokens) })}` })
  }
  return cells
})

const visibleCells = computed(() => view.value === 'day' ? dailyCells.value : view.value === 'week' ? weeklyCells.value : monthlyCells.value)
const peakTokens = computed(() => Math.max(0, ...visibleCells.value.filter(cell => !cell.placeholder).map(cell => cell.tokens)))
const metrics = computed(() => [
  { label: t('dashboard.tokenActivity.totalTokens'), value: formatTokens(props.activity.summary.total_tokens) },
  { label: t('dashboard.tokenActivity.peakTokens'), value: formatTokens(peakTokens.value) },
  { label: t('dashboard.tokenActivity.currentStreak'), value: t('dashboard.tokenActivity.days', { count: props.activity.summary.current_streak_days }) },
  { label: t('dashboard.tokenActivity.longestStreak'), value: t('dashboard.tokenActivity.days', { count: props.activity.summary.longest_streak_days }) },
])
const viewOptions = computed(() => [
  { value: 'day' as const, label: t('dashboard.tokenActivity.daily') },
  { value: 'week' as const, label: t('dashboard.tokenActivity.weekly') },
  { value: 'month' as const, label: t('dashboard.tokenActivity.cumulative') },
])

const levelClass = (tokens: number): string => {
  if (tokens <= 0 || peakTokens.value <= 0) return 'bg-gray-200 dark:bg-dark-600'
  const ratio = tokens / peakTokens.value
  if (ratio <= 0.25) return 'bg-blue-200 dark:bg-blue-900'
  if (ratio <= 0.5) return 'bg-blue-400 dark:bg-blue-700'
  if (ratio <= 0.75) return 'bg-blue-500 dark:bg-blue-500'
  return 'bg-blue-600 dark:bg-blue-300'
}

const dailyColumnCount = computed(() => Math.ceil(dailyCells.value.length / 7))
const monthLabels = computed(() => monthlyCells.value.map(month => {
  const weeks = Math.floor((mondayOf(month.start).getTime() - mondayOf(startDate.value).getTime()) / 604800000)
  return { key: month.key, column: Math.max(1, weeks + 1), text: new Intl.DateTimeFormat(locale.value, { month: 'short' }).format(month.start) }
}))
const monthLabelStyle = computed(() => ({ gridTemplateColumns: `repeat(${dailyColumnCount.value}, 14px)`, columnGap: '4px' }))
const cellGridStyle = computed(() => view.value === 'day'
  ? { gridTemplateRows: 'repeat(7, 14px)', gridAutoFlow: 'column' as const, gridAutoColumns: '14px' }
  : { gridTemplateColumns: `repeat(${visibleCells.value.length}, 14px)` })
const gridContainerStyle = computed(() => ({
  minWidth: `${view.value === 'day' ? dailyColumnCount.value * 18 : visibleCells.value.length * 18}px`,
  width: 'max-content',
}))
</script>
