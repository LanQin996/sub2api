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

      <div class="ticker-viewport min-w-0 flex-1" tabindex="0">
        <div
          class="ticker-strip"
          :style="{ '--ticker-duration': `${tickerDuration}s` }"
        >
          <div class="ticker-group">
            <span
              v-for="item in tickerItems"
              :key="item.id"
              class="ticker-item"
            >
              <span class="font-semibold text-gray-900 dark:text-white">{{ item.title }}</span>
              <span v-if="item.preview" class="text-gray-600 dark:text-gray-300">{{ item.preview }}</span>
            </span>
          </div>
          <div class="ticker-group" aria-hidden="true">
            <span
              v-for="item in tickerItems"
              :key="`copy-${item.id}`"
              class="ticker-item"
            >
              <span class="font-semibold text-gray-900 dark:text-white">{{ item.title }}</span>
              <span v-if="item.preview" class="text-gray-600 dark:text-gray-300">{{ item.preview }}</span>
            </span>
          </div>
        </div>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { UserAnnouncement } from '@/types'

const props = defineProps<{
  announcements: UserAnnouncement[]
}>()

const { t } = useI18n()

const tickerItems = computed(() =>
  props.announcements.map((item) => ({
    id: item.id,
    title: item.title,
    preview: buildPreview(item.content)
  }))
)

const tickerDuration = computed(() => Math.min(80, Math.max(24, tickerItems.value.length * 12)))

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
  mask-image: linear-gradient(90deg, transparent, #000 24px, #000 calc(100% - 24px), transparent);
  overflow: hidden;
}

.ticker-strip {
  --ticker-duration: 32s;
  display: flex;
  width: max-content;
  animation: ticker-scroll var(--ticker-duration) linear infinite;
}

.ticker-viewport:hover .ticker-strip,
.ticker-viewport:focus-within .ticker-strip {
  animation-play-state: paused;
}

.ticker-group {
  display: inline-flex;
  align-items: center;
  gap: 2rem;
  padding-right: 2rem;
}

.ticker-item {
  display: inline-flex;
  max-width: min(56rem, 72vw);
  align-items: center;
  gap: 0.5rem;
  white-space: nowrap;
  font-size: 0.875rem;
  line-height: 1.25rem;
}

.ticker-item::before {
  content: "";
  width: 0.375rem;
  height: 0.375rem;
  flex: 0 0 auto;
  border-radius: 9999px;
  background: currentColor;
  color: rgb(37 99 235);
}

@keyframes ticker-scroll {
  from {
    transform: translateX(0);
  }
  to {
    transform: translateX(-50%);
  }
}

@media (prefers-reduced-motion: reduce) {
  .ticker-strip {
    animation: none;
  }

  .ticker-group[aria-hidden="true"] {
    display: none;
  }
}
</style>
