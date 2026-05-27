<template>
  <section
    v-if="tickerItems.length > 0"
    class="overflow-hidden rounded-lg border border-blue-200 bg-blue-50/80 dark:border-blue-900/60 dark:bg-blue-950/30"
    :aria-label="t('announcements.ticker')"
  >
    <div class="flex h-11 items-center gap-3 overflow-hidden px-3">
      <div class="flex shrink-0 items-center gap-2 text-sm font-semibold text-blue-700 dark:text-blue-300">
        <Icon name="bell" size="sm" :stroke-width="2" />
        <span>{{ t('announcements.ticker') }}</span>
      </div>

      <div
        class="ticker-viewport"
        tabindex="0"
        aria-live="polite"
        @mouseenter="pauseRotation"
        @mouseleave="resumeRotation"
        @focusin="pauseRotation"
        @focusout="resumeRotation"
      >
        <div
          v-if="currentItem"
          :key="tickerItemKey"
          class="ticker-line"
          :style="{ '--ticker-duration': `${tickerDuration}s`, animationPlayState: isPaused ? 'paused' : 'running' }"
          :title="currentItem.preview ? `${currentItem.title} ${currentItem.preview}` : currentItem.title"
          @animationend="showNextItem"
        >
          <span class="font-semibold text-gray-900 dark:text-white">
            {{ currentItem.title }}
          </span>
          <span v-if="currentItem.preview" class="text-gray-600 dark:text-gray-300">
            {{ currentItem.preview }}
          </span>
        </div>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { UserAnnouncement } from '@/types'

const props = defineProps<{
  announcements: UserAnnouncement[]
}>()

const { t } = useI18n()
const activeIndex = ref(0)
const animationCycle = ref(0)
const isPaused = ref(false)

const tickerItems = computed(() =>
  props.announcements.map((item) => ({
    id: item.id,
    title: item.title,
    preview: buildPreview(item.content)
  }))
)

const currentItem = computed(() => tickerItems.value[activeIndex.value] ?? null)
const tickerItemKey = computed(() => currentItem.value ? `${currentItem.value.id}-${animationCycle.value}` : 'empty')
const itemSignature = computed(() =>
  tickerItems.value.map((item) => `${item.id}:${item.title}:${item.preview}`).join('|')
)
const tickerDuration = computed(() => {
  if (!currentItem.value) return 16
  const charCount = currentItem.value.title.length + currentItem.value.preview.length
  return Math.min(36, Math.max(12, Math.ceil(charCount / 4)))
})

function showNextItem() {
  if (tickerItems.value.length <= 1) {
    animationCycle.value += 1
    return
  }
  activeIndex.value = (activeIndex.value + 1) % tickerItems.value.length
}

function pauseRotation() {
  isPaused.value = true
}

function resumeRotation() {
  isPaused.value = false
}

watch(itemSignature, () => {
  activeIndex.value = 0
  animationCycle.value += 1
})

function buildPreview(content: string): string {
  const text = content
    .replace(/```[\s\S]*?```/g, ' ')
    .replace(/`([^`]+)`/g, '$1')
    .replace(/!\[[^\]]*]\([^)]*\)/g, ' ')
    .replace(/\[([^\]]+)]\([^)]*\)/g, '$1')
    .replace(/[#>*_~\-]+/g, ' ')
    .replace(/\s+/g, ' ')
    .trim()

  if (!text) return ''
  return text.length > 90 ? `${text.slice(0, 90)}...` : text
}
</script>

<style scoped>
.ticker-viewport {
  position: relative;
  min-width: 0;
  flex: 1 1 0%;
  overflow: hidden;
}

.ticker-line {
  --ticker-duration: 16s;
  display: inline-flex;
  align-items: center;
  gap: 0.5rem;
  width: max-content;
  max-width: none;
  padding-left: 100%;
  white-space: nowrap;
  font-size: 0.875rem;
  line-height: 1.25rem;
  animation: ticker-marquee var(--ticker-duration) linear forwards;
  will-change: transform;
}

.ticker-line::before {
  content: "";
  width: 0.375rem;
  height: 0.375rem;
  flex: 0 0 auto;
  border-radius: 9999px;
  background: currentColor;
  color: rgb(37 99 235);
}

@keyframes ticker-marquee {
  from {
    transform: translateX(0);
  }
  to {
    transform: translateX(-100%);
  }
}

@media (prefers-reduced-motion: reduce) {
  .ticker-line {
    animation: none;
    padding-left: 0;
  }
}
</style>
