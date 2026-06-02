<template>
  <AppLayout>
    <div class="flex h-[calc(100vh-5rem)] min-h-[760px] flex-col gap-4 p-4 lg:p-6">
      <div class="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
        <div>
          <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">{{ ui.title }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-300">{{ ui.subtitle }}</p>
        </div>
        <div class="flex flex-col gap-2 sm:flex-row sm:items-center">
          <Select
            v-model="selectedKeyId"
            :options="keyOptions"
            class="w-full sm:w-72"
            :placeholder="ui.selectKey"
            :disabled="loadingKeys || apiKeys.length === 0"
          />
          <button class="btn btn-secondary" :disabled="loadingKeys" @click="loadKeys">
            <Icon name="refresh" size="sm" :class="loadingKeys ? 'animate-spin' : ''" />
          </button>
          <router-link class="btn btn-primary" to="/keys">
            <Icon name="key" size="sm" class="mr-2" />
            {{ ui.manageKeys }}
          </router-link>
        </div>
      </div>

      <div
        v-if="apiKeys.length === 0 && !loadingKeys"
        class="rounded-lg border border-dashed border-gray-300 bg-white p-8 text-center dark:border-dark-600 dark:bg-dark-800"
      >
        <p class="text-base font-medium text-gray-900 dark:text-white">{{ ui.noKeysTitle }}</p>
        <p class="mt-2 text-sm text-gray-500 dark:text-dark-300">{{ ui.noKeysText }}</p>
        <router-link class="btn btn-primary mt-4" to="/keys">{{ ui.createKey }}</router-link>
      </div>

      <div v-else class="grid min-h-0 flex-1 grid-cols-1 gap-4 xl:grid-cols-[minmax(0,1.45fr)_minmax(380px,0.75fr)]">
        <section class="flex min-h-0 flex-col rounded-lg border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-800">
          <div class="flex flex-wrap items-center gap-2 border-b border-gray-200 p-3 dark:border-dark-700">
            <button class="btn" :class="mode === 'chat' ? 'btn-primary' : 'btn-secondary'" @click="mode = 'chat'">
              <Icon name="chat" size="sm" class="mr-2" />
              {{ ui.chat }}
            </button>
            <button class="btn" :class="mode === 'image' ? 'btn-primary' : 'btn-secondary'" @click="mode = 'image'">
              <Icon name="sparkles" size="sm" class="mr-2" />
              {{ ui.image }}
            </button>
            <div class="ml-auto min-w-0 text-xs text-gray-500 dark:text-dark-300">
              {{ ui.currentKey }}<span class="font-mono">{{ selectedKeyLabel || ui.notSelected }}</span>
            </div>
          </div>

          <div v-if="mode === 'chat'" ref="chatScrollRef" class="min-h-0 flex-1 overflow-y-auto p-4">
            <div class="mx-auto flex max-w-4xl flex-col gap-3">
              <div
                v-for="message in messages"
                :key="message.id"
                class="flex"
                :class="message.role === 'user' ? 'justify-end' : 'justify-start'"
              >
                <div
                  class="max-w-[86%] whitespace-pre-wrap rounded-lg px-4 py-3 text-sm leading-6"
                  :class="message.role === 'user'
                    ? 'bg-primary-600 text-white'
                    : 'bg-gray-100 text-gray-900 dark:bg-dark-700 dark:text-dark-50'"
                >
                  {{ message.content }}
                </div>
              </div>
              <div v-if="chatLoading" class="text-sm text-gray-500 dark:text-dark-300">{{ ui.chatLoading }}</div>
            </div>
          </div>

          <div v-else class="min-h-0 flex-1 overflow-y-auto p-4">
            <div class="grid gap-4 sm:grid-cols-2 2xl:grid-cols-3">
              <div
                v-for="image in images"
                :key="image.id"
                class="overflow-hidden rounded-lg border border-gray-200 bg-gray-50 dark:border-dark-700 dark:bg-dark-900"
              >
                <div
                  v-if="image.status === 'pending'"
                  class="flex aspect-square w-full flex-col items-center justify-center gap-3 bg-gray-100 p-6 text-center dark:bg-dark-700"
                >
                  <Icon name="sparkles" size="xl" class="animate-pulse text-primary-500" />
                  <div>
                    <p class="text-sm font-medium text-gray-900 dark:text-white">
                      {{ ui.imageGenerating }}
                    </p>
                    <p class="mt-1 text-xs text-gray-500 dark:text-dark-300">
                      {{ t('playground.elapsedSeconds', { seconds: image.elapsedSeconds }) }}
                    </p>
                  </div>
                </div>
                <div
                  v-else-if="image.status === 'error'"
                  class="flex aspect-square w-full flex-col items-center justify-center gap-3 bg-red-50 p-6 text-center dark:bg-red-500/10"
                >
                  <Icon name="exclamationCircle" size="xl" class="text-red-500" />
                  <p class="text-sm font-medium text-red-700 dark:text-red-300">{{ ui.imageFailed }}</p>
                  <p class="line-clamp-4 text-xs text-red-600 dark:text-red-300">{{ image.error }}</p>
                </div>
                <img v-else :src="image.url" :alt="image.prompt" class="aspect-square w-full object-cover" />
                <div class="p-3">
                  <div class="mb-2 flex items-center gap-2 text-xs text-gray-500 dark:text-dark-300">
                    <span>{{ image.mode === 'edit' ? ui.editImage : ui.generateImage }}</span>
                    <span>{{ image.size }}</span>
                    <span>{{ image.format }}</span>
                  </div>
                  <p class="line-clamp-2 text-sm text-gray-700 dark:text-dark-100">{{ image.prompt }}</p>
                  <a v-if="image.status === 'done'" :href="image.url" target="_blank" rel="noreferrer" class="mt-2 inline-flex text-sm text-primary-600 hover:text-primary-700">
                    {{ ui.openImage }}
                  </a>
                </div>
              </div>
            </div>
            <div v-if="images.length === 0" class="flex h-full items-center justify-center text-sm text-gray-500 dark:text-dark-300">
              {{ ui.imageEmpty }}
            </div>
          </div>

          <div class="border-t border-gray-200 p-3 dark:border-dark-700">
            <form v-if="mode === 'chat'" class="flex gap-2" @submit.prevent="sendChat">
              <textarea
                v-model="chatInput"
                rows="2"
                class="input min-h-[52px] flex-1 resize-none"
                :placeholder="ui.chatPlaceholder"
                :disabled="chatLoading"
                @keydown.enter.exact.prevent="sendChat"
              />
              <button class="btn btn-primary self-stretch" :disabled="!canSendChat">
                <Icon name="play" size="sm" class="mr-2" />
                {{ ui.send }}
              </button>
            </form>
            <form v-else class="flex gap-2" @submit.prevent="generateImage">
              <textarea
                v-model="imagePrompt"
                rows="2"
                class="input min-h-[52px] flex-1 resize-none"
                :placeholder="ui.imagePlaceholder"
                :disabled="imageLoading"
              />
              <button class="btn btn-primary self-stretch" :disabled="!canGenerateImage">
                <Icon name="sparkles" size="sm" class="mr-2" />
                {{ imageEditFiles.length > 0 ? ui.editImage : ui.generateImage }}
              </button>
            </form>
          </div>
        </section>

        <aside class="flex min-h-0 flex-col gap-4 overflow-y-auto">
          <section v-if="mode === 'chat'" class="rounded-lg border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800">
            <h2 class="text-sm font-semibold text-gray-900 dark:text-white">{{ ui.chatSettings }}</h2>
            <div class="mt-4 space-y-4">
              <label class="block">
                <span class="flex items-center justify-between gap-2 text-sm text-gray-600 dark:text-dark-200">
                  <span>{{ ui.chatModel }}</span>
                  <span v-if="loadingModels" class="text-xs text-gray-400">{{ ui.loadingModels }}</span>
                </span>
                <Select v-model="selectedChatModel" class="mt-1" :options="chatModelOptions" />
              </label>
              <input
                v-if="isCustomChatModel"
                v-model="customChatModel"
                class="input w-full font-mono text-sm"
                :placeholder="ui.customModelPlaceholder"
              />
            </div>
          </section>

          <section v-if="mode === 'image'" class="rounded-lg border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800">
            <div class="flex items-center justify-between gap-3">
              <h2 class="text-sm font-semibold text-gray-900 dark:text-white">{{ ui.imageSettings }}</h2>
              <span class="text-xs text-gray-500 dark:text-dark-300">
                {{ imageEditFiles.length > 0 ? ui.editMode : ui.generateMode }}
              </span>
            </div>
            <div class="mt-4 space-y-4">
              <label class="block">
                <span class="flex items-center justify-between gap-2 text-sm text-gray-600 dark:text-dark-200">
                  <span>{{ ui.imageModel }}</span>
                  <span v-if="loadingModels" class="text-xs text-gray-400">{{ ui.loadingModels }}</span>
                </span>
                <Select v-model="selectedImageModel" class="mt-1" :options="imageModelOptions" />
              </label>
              <input
                v-if="isCustomImageModel"
                v-model="customImageModel"
                class="input w-full font-mono text-sm"
                :placeholder="ui.customModelPlaceholder"
              />
            </div>
            <div class="mt-4 grid grid-cols-2 gap-3">
              <label class="block">
                <span class="text-sm text-gray-600 dark:text-dark-200">{{ ui.count }}</span>
                <Select v-model="imageCount" class="mt-1" :options="imageCountOptions" />
              </label>
              <label class="block">
                <span class="text-sm text-gray-600 dark:text-dark-200">{{ ui.size }}</span>
                <Select v-model="imageSize" class="mt-1" :options="imageSizeOptions" />
              </label>
              <div v-if="isCustomImageSize" class="col-span-2 grid grid-cols-2 gap-3">
                <label class="block">
                  <span class="text-sm text-gray-600 dark:text-dark-200">{{ ui.customWidth }}</span>
                  <input
                    v-model.number="customImageWidth"
                    class="input mt-1 w-full"
                    type="number"
                    min="1"
                    step="1"
                    placeholder="1024"
                  />
                </label>
                <label class="block">
                  <span class="text-sm text-gray-600 dark:text-dark-200">{{ ui.customHeight }}</span>
                  <input
                    v-model.number="customImageHeight"
                    class="input mt-1 w-full"
                    type="number"
                    min="1"
                    step="1"
                    placeholder="1024"
                  />
                </label>
              </div>
              <label class="block">
                <span class="text-sm text-gray-600 dark:text-dark-200">{{ ui.quality }}</span>
                <Select v-model="imageQuality" class="mt-1" :options="imageQualityOptions" />
              </label>
              <label class="block">
                <span class="text-sm text-gray-600 dark:text-dark-200">{{ ui.background }}</span>
                <Select v-model="imageBackground" class="mt-1" :options="imageBackgroundOptions" />
              </label>
              <label class="block">
                <span class="text-sm text-gray-600 dark:text-dark-200">{{ ui.outputFormat }}</span>
                <Select v-model="imageOutputFormat" class="mt-1" :options="imageOutputFormatOptions" />
              </label>
            </div>

            <div class="mt-4">
              <label class="block">
                <span class="text-sm text-gray-600 dark:text-dark-200">{{ ui.referenceImages }}</span>
                <input
                  type="file"
                  accept="image/png,image/jpeg,image/webp"
                  multiple
                  class="mt-1 block w-full text-sm text-gray-600 file:mr-3 file:rounded-md file:border-0 file:bg-primary-50 file:px-3 file:py-2 file:text-sm file:font-medium file:text-primary-700 hover:file:bg-primary-100 dark:text-dark-200 dark:file:bg-dark-700 dark:file:text-dark-50"
                  @change="onImageFilesChange"
                />
              </label>
              <div v-if="imageEditFiles.length > 0" class="mt-3 space-y-2">
                <div
                  v-for="file in imageEditFiles"
                  :key="`${file.name}-${file.size}-${file.lastModified}`"
                  class="flex items-center justify-between gap-2 rounded-md bg-gray-50 px-3 py-2 text-xs dark:bg-dark-700"
                >
                  <span class="truncate text-gray-700 dark:text-dark-100">{{ file.name }}</span>
                  <span class="shrink-0 text-gray-500 dark:text-dark-300">{{ formatFileSize(file.size) }}</span>
                </div>
                <button type="button" class="btn btn-secondary w-full" @click="clearImageFiles">{{ ui.clearReferenceImages }}</button>
              </div>
              <p class="mt-2 text-xs text-gray-500 dark:text-dark-300">{{ ui.referenceHint }}</p>
            </div>
          </section>

          <section class="rounded-lg border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800">
            <h2 class="text-sm font-semibold text-gray-900 dark:text-white">{{ ui.actions }}</h2>
            <div class="mt-4 grid grid-cols-2 gap-2">
              <button class="btn btn-secondary" @click="clearChat">{{ ui.clearChat }}</button>
              <button class="btn btn-secondary" @click="clearImages">{{ ui.clearImages }}</button>
            </div>
            <p v-if="errorMessage" class="mt-4 rounded-lg bg-red-50 p-3 text-sm text-red-700 dark:bg-red-500/10 dark:text-red-300">
              {{ errorMessage }}
            </p>
          </section>
        </aside>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import { keysAPI } from '@/api/keys'
import { useAppStore } from '@/stores/app'
import { maskApiKey } from '@/utils/maskApiKey'
import type { ApiKey, SelectOption } from '@/types'

type Mode = 'chat' | 'image'
type ChatRole = 'user' | 'assistant'
type ImageMode = 'generate' | 'edit'

interface ChatMessage {
  id: string
  role: ChatRole
  content: string
}

interface GeneratedImage {
  id: string
  url: string
  prompt: string
  size: string
  format: string
  mode: ImageMode
  status: 'pending' | 'done' | 'error'
  elapsedSeconds: number
  error?: string
}

const { t } = useI18n()
const ui = new Proxy({} as Record<string, string>, {
  get: (_target, key: string) => t(`playground.${key}`)
})

const STORAGE_KEY_ID = 'playground:selected-key-id'
const STORAGE_CHAT_MODEL = 'playground:chat-model'
const STORAGE_IMAGE_MODEL = 'playground:image-model'
const STORAGE_SELECTED_CHAT_MODEL = 'playground:selected-chat-model'
const STORAGE_SELECTED_IMAGE_MODEL = 'playground:selected-image-model'
const STORAGE_IMAGE_SIZE = 'playground:image-size'
const STORAGE_CUSTOM_IMAGE_WIDTH = 'playground:custom-image-width'
const STORAGE_CUSTOM_IMAGE_HEIGHT = 'playground:custom-image-height'
const STORAGE_IMAGE_COUNT = 'playground:image-count'
const STORAGE_IMAGE_QUALITY = 'playground:image-quality'
const STORAGE_IMAGE_BACKGROUND = 'playground:image-background'
const STORAGE_IMAGE_OUTPUT_FORMAT = 'playground:image-output-format'

const appStore = useAppStore()
const apiKeys = ref<ApiKey[]>([])
const selectedKeyId = ref<string | number | boolean | null>(localStorage.getItem(STORAGE_KEY_ID) || null)
const loadingKeys = ref(false)
const loadingModels = ref(false)
const mode = ref<Mode>('chat')
const selectedChatModel = ref<string | number | boolean | null>(localStorage.getItem(STORAGE_SELECTED_CHAT_MODEL) || 'gpt-4o-mini')
const selectedImageModel = ref<string | number | boolean | null>(localStorage.getItem(STORAGE_SELECTED_IMAGE_MODEL) || 'gpt-image-1')
const customChatModel = ref(localStorage.getItem(STORAGE_CHAT_MODEL) || 'gpt-4o-mini')
const customImageModel = ref(localStorage.getItem(STORAGE_IMAGE_MODEL) || 'gpt-image-1')
const keyModelMap = ref<Record<string, string[]>>({})
const keyModelsLoaded = ref<Record<string, boolean>>({})
const imageSize = ref<string | number | boolean | null>(localStorage.getItem(STORAGE_IMAGE_SIZE) || '1024x1024')
const customImageWidth = ref(Number(localStorage.getItem(STORAGE_CUSTOM_IMAGE_WIDTH) || 1024))
const customImageHeight = ref(Number(localStorage.getItem(STORAGE_CUSTOM_IMAGE_HEIGHT) || 1024))
const imageCount = ref<string | number | boolean | null>(localStorage.getItem(STORAGE_IMAGE_COUNT) || '1')
const imageQuality = ref<string | number | boolean | null>(localStorage.getItem(STORAGE_IMAGE_QUALITY) || 'auto')
const imageBackground = ref<string | number | boolean | null>(localStorage.getItem(STORAGE_IMAGE_BACKGROUND) || 'auto')
const imageOutputFormat = ref<string | number | boolean | null>(localStorage.getItem(STORAGE_IMAGE_OUTPUT_FORMAT) || 'png')
const endpointBase = computed(() => resolveDefaultEndpoint())
const chatInput = ref('')
const imagePrompt = ref('')
const imageEditFiles = ref<File[]>([])
const messages = ref<ChatMessage[]>([
  {
    id: crypto.randomUUID(),
    role: 'assistant',
    content: ui.hello
  }
])
const images = ref<GeneratedImage[]>([])
const chatLoading = ref(false)
const imageLoading = ref(false)
const errorMessage = ref('')
const chatScrollRef = ref<HTMLElement | null>(null)
const imageTimers = new Map<string, number>()

const imageCountOptions: SelectOption[] = [
  { value: '1', label: '1' },
  { value: '2', label: '2' },
  { value: '3', label: '3' },
  { value: '4', label: '4' }
]

const defaultChatModels = [
  'gpt-4o-mini',
  'gpt-4o',
  'gpt-4.1-mini',
  'gpt-4.1',
  'claude-3-5-sonnet-latest',
  'gemini-2.0-flash'
]

const defaultImageModels = [
  'gpt-image-1',
  'dall-e-3',
  'dall-e-2'
]

function uniqueModels(models: string[]): string[] {
  return Array.from(new Set(models.map((model) => model.trim()).filter(Boolean))).sort((a, b) => a.localeCompare(b))
}

function modelsForSelectedKey(): string[] {
  return keyModelMap.value[String(selectedKeyId.value || '')] || []
}

function isLikelyImageModel(model: string): boolean {
  const normalized = model.toLowerCase()
  return normalized.includes('image') || normalized.includes('dall') || normalized.includes('flux') || normalized.includes('stable-diffusion')
}

function hasChatModelsForKey(keyId: string | number): boolean {
  return (keyModelMap.value[String(keyId)] || []).some((model) => !isLikelyImageModel(model))
}

function hasImageModelsForKey(keyId: string | number): boolean {
  return (keyModelMap.value[String(keyId)] || []).some((model) => isLikelyImageModel(model))
}

function modelOptions(models: string[]): SelectOption[] {
  return [
    ...models.map((model) => ({ value: model, label: model })),
    { value: 'custom', label: ui.customModel }
  ]
}

const chatModelOptions = computed<SelectOption[]>(() => {
  const keyModels = modelsForSelectedKey().filter((model) => !isLikelyImageModel(model))
  const chatModels = keyModels.length > 0 ? uniqueModels(keyModels) : (selectedModelsLoaded.value ? [] : defaultChatModels)
  return modelOptions(chatModels)
})

const imageModelOptions = computed<SelectOption[]>(() => {
  const keyModels = modelsForSelectedKey().filter((model) => isLikelyImageModel(model))
  const imageModels = keyModels.length > 0 ? uniqueModels(keyModels) : (selectedModelsLoaded.value ? [] : defaultImageModels)
  return modelOptions(imageModels)
})

function ensureSelectedModelIsAvailable(
  selectedModel: typeof selectedChatModel,
  options: SelectOption[],
) {
  const value = String(selectedModel.value || '')
  if (value === 'custom') return
  if (options.some((option) => option.value === value)) return
  selectedModel.value = options.find((option) => option.value !== 'custom')?.value ?? 'custom'
}

const imageSizeOptions = computed<SelectOption[]>(() => [
  { value: '1024x1024', label: `1024x1024 - ${ui.sizeSquare}` },
  { value: '1536x1024', label: `1536x1024 - ${ui.sizeLandscape32}` },
  { value: '1024x1536', label: `1024x1536 - ${ui.sizePortrait23}` },
  { value: '2048x1536', label: `2048x1536 - ${ui.sizeLandscape43}` },
  { value: '1536x2048', label: `1536x2048 - ${ui.sizePortrait34}` },
  { value: '2048x2048', label: `2048x2048 - ${ui.size2kSquare}` },
  { value: '2304x2304', label: `2304x2304 - ${ui.size2kPlusSquare}` },
  { value: '2560x1440', label: `2560x1440 - ${ui.sizeLandscape169}` },
  { value: '1440x2560', label: `1440x2560 - ${ui.sizePortrait916}` },
  { value: '2560x2560', label: `2560x2560 - ${ui.size25kSquare}` },
  { value: '1792x3200', label: `1792x3200 - ${ui.sizeTallPortrait}` },
  { value: '2016x3584', label: `2016x3584 - ${ui.sizeUltraTallPortrait}` },
  { value: '3584x2016', label: `3584x2016 - ${ui.sizeHdLandscape169}` },
  { value: '3840x2160', label: `3840x2160 - ${ui.size4kLandscape}` },
  { value: '3840x1280', label: `3840x1280 - ${ui.sizeUltraWideBanner}` },
  { value: '1280x3840', label: `1280x3840 - ${ui.sizeUltraTallBanner}` },
  { value: 'custom', label: ui.customSize },
  { value: 'auto', label: 'Auto' }
])

const imageQualityOptions: SelectOption[] = [
  { value: 'auto', label: 'Auto' },
  { value: 'high', label: 'High' },
  { value: 'medium', label: 'Medium' },
  { value: 'low', label: 'Low' }
]

const imageBackgroundOptions: SelectOption[] = [
  { value: 'auto', label: 'Auto' },
  { value: 'transparent', label: ui.transparent },
  { value: 'opaque', label: ui.opaque }
]

const imageOutputFormatOptions: SelectOption[] = [
  { value: 'png', label: 'PNG' },
  { value: 'jpeg', label: 'JPEG' },
  { value: 'webp', label: 'WEBP' }
]

const keyOptions = computed<SelectOption[]>(() =>
  apiKeys.value
    .filter((key) => {
      const keyId = String(key.id)
      if (!keyModelsLoaded.value[keyId]) return true
      return mode.value === 'image' ? hasImageModelsForKey(keyId) : hasChatModelsForKey(keyId)
    })
    .map((key) => ({
      value: String(key.id),
      label: `${key.name} (${maskApiKey(key.key)})`
    }))
)

const selectedKey = computed(() =>
  apiKeys.value.find((key) => String(key.id) === String(selectedKeyId.value)) || null
)
const selectedModelsLoaded = computed(() => keyModelsLoaded.value[String(selectedKeyId.value || '')] === true)
const selectedKeyLabel = computed(() => {
  if (!selectedKey.value) return ''
  return `${selectedKey.value.name} / ${maskApiKey(selectedKey.value.key)}`
})
const selectedApiKey = computed(() => selectedKey.value?.key || '')
const isCustomChatModel = computed(() => selectedChatModel.value === 'custom')
const isCustomImageModel = computed(() => selectedImageModel.value === 'custom')
const resolvedChatModel = computed(() =>
  isCustomChatModel.value ? customChatModel.value.trim() : String(selectedChatModel.value || 'gpt-4o-mini')
)
const resolvedImageModel = computed(() =>
  isCustomImageModel.value ? customImageModel.value.trim() : String(selectedImageModel.value || 'gpt-image-1')
)
const canSendChat = computed(() => !!selectedApiKey.value && !!resolvedChatModel.value && chatInput.value.trim().length > 0 && !chatLoading.value)
const canGenerateImage = computed(() => !!selectedApiKey.value && !!resolvedImageModel.value && imagePrompt.value.trim().length > 0 && !imageLoading.value)
const isCustomImageSize = computed(() => imageSize.value === 'custom')
const resolvedImageSize = computed(() => {
  if (!isCustomImageSize.value) return String(imageSize.value || '1024x1024')
  const width = Math.max(1, Math.floor(Number(customImageWidth.value) || 1024))
  const height = Math.max(1, Math.floor(Number(customImageHeight.value) || 1024))
  return `${width}x${height}`
})

function normalizeEndpoint(value: string): string {
  const trimmed = value.trim().replace(/\/+$/, '')
  if (!trimmed) return `${window.location.origin}/v1`
  if (trimmed.endsWith('/v1')) return trimmed
  return `${trimmed}/v1`
}

function resolveDefaultEndpoint(): string {
  const configured = appStore.cachedPublicSettings?.api_base_url || appStore.apiBaseUrl
  return normalizeEndpoint(configured || window.location.origin)
}

function buildImagePayload(): Record<string, unknown> {
  const payload: Record<string, unknown> = {
    model: resolvedImageModel.value,
    prompt: imagePrompt.value.trim(),
    n: Number(imageCount.value || 1),
    size: resolvedImageSize.value,
    quality: imageQuality.value || 'auto',
    background: imageBackground.value || 'auto',
    output_format: imageOutputFormat.value || 'png'
  }
  return payload
}

function appendImageFormValue(formData: FormData, key: string, value: unknown) {
  if (value !== undefined && value !== null && value !== '') {
    formData.append(key, String(value))
  }
}

function extractImages(data: any, prompt: string, imageMode: ImageMode): GeneratedImage[] {
  return (data?.data || [])
    .map((item: any) => {
      const url = item?.url || (item?.b64_json ? `data:image/${imageOutputFormat.value};base64,${item.b64_json}` : '')
      if (!url) return null
      return {
        id: crypto.randomUUID(),
        url,
        prompt,
        size: resolvedImageSize.value,
        format: String(imageOutputFormat.value || 'png').toUpperCase(),
        mode: imageMode,
        status: 'done',
        elapsedSeconds: 0
      }
    })
    .filter(Boolean) as GeneratedImage[]
}

async function loadKeys() {
  loadingKeys.value = true
  errorMessage.value = ''
  try {
    const data = await keysAPI.list(1, 100, { status: 'active', sort_by: 'created_at', sort_order: 'desc' })
    apiKeys.value = data.items
    if (!selectedKey.value && apiKeys.value.length > 0) {
      selectedKeyId.value = String(apiKeys.value[0].id)
    }
  } catch (error) {
    errorMessage.value = (error as { message?: string }).message || ui.loadKeyFailed
  } finally {
    loadingKeys.value = false
  }
}

async function fetchModelsForApiKey(apiKey: string): Promise<string[]> {
  const response = await fetch(`${normalizeEndpoint(endpointBase.value)}/models`, {
    method: 'GET',
    headers: {
      Authorization: `Bearer ${apiKey}`
    }
  })
  const data = await response.json().catch(() => ({}))
  if (!response.ok) {
    throw new Error(data?.error?.message || data?.message || `${ui.requestFailed} (${response.status})`)
  }
  return Array.isArray(data?.data)
    ? uniqueModels(data.data.map((item: any) => String(item?.id || '').trim()))
    : []
}

async function loadModelsForKey(key: ApiKey) {
  const keyId = String(key.id)
  try {
    const models = await fetchModelsForApiKey(key.key)
    keyModelMap.value = { ...keyModelMap.value, [keyId]: models }
    keyModelsLoaded.value = { ...keyModelsLoaded.value, [keyId]: true }
  } catch {
    keyModelMap.value = { ...keyModelMap.value, [keyId]: [] }
    keyModelsLoaded.value = { ...keyModelsLoaded.value, [keyId]: false }
  }
}

async function loadAllKeyModels() {
  loadingModels.value = true
  try {
    await Promise.all(apiKeys.value.map((key) => loadModelsForKey(key)))
  } finally {
    loadingModels.value = false
  }
}

async function callOpenAICompat(path: string, payload: Record<string, unknown>) {
  const response = await fetch(`${normalizeEndpoint(endpointBase.value)}${path}`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${selectedApiKey.value}`,
      'Content-Type': 'application/json'
    },
    body: JSON.stringify(payload)
  })
  const data = await response.json().catch(() => ({}))
  if (!response.ok) {
    const message = data?.error?.message || data?.message || `${ui.requestFailed} (${response.status})`
    throw new Error(message)
  }
  return data
}

async function callOpenAICompatStream(
  path: string,
  payload: Record<string, unknown>,
  onDelta: (content: string) => void,
) {
  const response = await fetch(`${normalizeEndpoint(endpointBase.value)}${path}`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${selectedApiKey.value}`,
      'Content-Type': 'application/json'
    },
    body: JSON.stringify(payload)
  })

  if (!response.ok) {
    const data = await response.json().catch(() => ({}))
    const message = data?.error?.message || data?.message || `${ui.requestFailed} (${response.status})`
    throw new Error(message)
  }

  if (!response.body) {
    throw new Error(ui.streamUnavailable)
  }

  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''

  while (true) {
    const { done, value } = await reader.read()
    if (done) break
    buffer += decoder.decode(value, { stream: true })
    const lines = buffer.split(/\r?\n/)
    buffer = lines.pop() || ''

    for (const line of lines) {
      const trimmed = line.trim()
      if (!trimmed || !trimmed.startsWith('data:')) continue
      const data = trimmed.slice(5).trim()
      if (data === '[DONE]') return
      try {
        const parsed = JSON.parse(data)
        const delta = parsed?.choices?.[0]?.delta?.content
          ?? parsed?.choices?.[0]?.message?.content
          ?? ''
        if (delta) onDelta(delta)
      } catch {
        // Ignore malformed stream keepalive chunks.
      }
    }
  }
}

async function callOpenAICompatForm(path: string, payload: Record<string, unknown>, files: File[]) {
  const formData = new FormData()
  Object.entries(payload).forEach(([key, value]) => appendImageFormValue(formData, key, value))
  files.forEach((file) => formData.append('image', file))

  const response = await fetch(`${normalizeEndpoint(endpointBase.value)}${path}`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${selectedApiKey.value}`
    },
    body: formData
  })
  const data = await response.json().catch(() => ({}))
  if (!response.ok) {
    const message = data?.error?.message || data?.message || `${ui.requestFailed} (${response.status})`
    throw new Error(message)
  }
  return data
}

async function sendChat() {
  if (!canSendChat.value) return
  const content = chatInput.value.trim()
  chatInput.value = ''
  errorMessage.value = ''
  messages.value.push({ id: crypto.randomUUID(), role: 'user', content })
  const assistantMessageId = crypto.randomUUID()
  messages.value.push({ id: assistantMessageId, role: 'assistant', content: '' })
  chatLoading.value = true
  await scrollChatToBottom()
  try {
    await callOpenAICompatStream('/chat/completions', {
      model: resolvedChatModel.value,
      messages: messages.value
        .filter((message) => message.id !== assistantMessageId)
        .map((message) => ({ role: message.role, content: message.content })),
      stream: true
    }, async (delta) => {
      appendChatDelta(assistantMessageId, delta)
      await scrollChatToBottom()
    })
    if (!messages.value.find((message) => message.id === assistantMessageId)?.content) {
      appendChatDelta(assistantMessageId, ui.noText)
    }
  } catch (error) {
    errorMessage.value = (error as { message?: string }).message || ui.chatFailed
    appendChatDelta(assistantMessageId, errorMessage.value)
  } finally {
    chatLoading.value = false
    await scrollChatToBottom()
  }
}

function appendChatDelta(messageId: string, delta: string) {
  messages.value = messages.value.map((message) =>
    message.id === messageId ? { ...message, content: `${message.content}${delta}` } : message
  )
}

async function generateImage() {
  if (!canGenerateImage.value) return
  const prompt = imagePrompt.value.trim()
  errorMessage.value = ''
  imageLoading.value = true
  const imageMode: ImageMode = imageEditFiles.value.length > 0 ? 'edit' : 'generate'
  const taskId = crypto.randomUUID()
  const task: GeneratedImage = {
    id: taskId,
    url: '',
    prompt,
    size: resolvedImageSize.value,
    format: String(imageOutputFormat.value || 'png').toUpperCase(),
    mode: imageMode,
    status: 'pending',
    elapsedSeconds: 0
  }
  images.value.unshift(task)
  startImageTimer(taskId)
  try {
    const payload = buildImagePayload()
    const data =
      imageMode === 'edit'
        ? await callOpenAICompatForm('/images/edits', payload, imageEditFiles.value)
        : await callOpenAICompat('/images/generations', payload)
    const generated = extractImages(data, prompt, imageMode)
    if (generated.length === 0) throw new Error(ui.imageNoReturn)
    replaceImageTask(taskId, generated)
  } catch (error) {
    const message = (error as { message?: string }).message || ui.imageFailed
    errorMessage.value = message
    updateImageTask(taskId, { status: 'error', error: message })
  } finally {
    stopImageTimer(taskId)
    imageLoading.value = false
  }
}

function updateImageTask(taskId: string, updates: Partial<GeneratedImage>) {
  images.value = images.value.map((image) => image.id === taskId ? { ...image, ...updates } : image)
}

function replaceImageTask(taskId: string, generated: GeneratedImage[]) {
  images.value = images.value.flatMap((image) => image.id === taskId ? generated : [image])
}

function startImageTimer(taskId: string) {
  stopImageTimer(taskId)
  const timer = window.setInterval(() => {
    images.value = images.value.map((image) =>
      image.id === taskId ? { ...image, elapsedSeconds: image.elapsedSeconds + 1 } : image
    )
  }, 1000)
  imageTimers.set(taskId, timer)
}

function stopImageTimer(taskId: string) {
  const timer = imageTimers.get(taskId)
  if (timer !== undefined) {
    window.clearInterval(timer)
    imageTimers.delete(taskId)
  }
}

async function scrollChatToBottom() {
  await nextTick()
  if (chatScrollRef.value) {
    chatScrollRef.value.scrollTop = chatScrollRef.value.scrollHeight
  }
}

function onImageFilesChange(event: Event) {
  const input = event.target as HTMLInputElement
  imageEditFiles.value = Array.from(input.files || [])
}

function clearImageFiles() {
  imageEditFiles.value = []
}

function formatFileSize(size: number): string {
  if (size < 1024) return `${size} B`
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`
  return `${(size / 1024 / 1024).toFixed(1)} MB`
}

function clearChat() {
  messages.value = [
    {
      id: crypto.randomUUID(),
      role: 'assistant',
      content: ui.cleared
    }
  ]
}

function clearImages() {
  images.value = []
}

function ensureSelectedKeySupportsMode() {
  if (!keyOptions.value.some((option) => String(option.value) === String(selectedKeyId.value))) {
    selectedKeyId.value = keyOptions.value[0]?.value ?? null
  }
}

watch(selectedKeyId, (value) => {
  if (value) localStorage.setItem(STORAGE_KEY_ID, String(value))
})
watch(
  [mode, keyOptions],
  () => {
    ensureSelectedKeySupportsMode()
  },
  { immediate: true }
)
watch(
  [selectedKeyId, chatModelOptions, imageModelOptions],
  () => {
    ensureSelectedModelIsAvailable(selectedChatModel, chatModelOptions.value)
    ensureSelectedModelIsAvailable(selectedImageModel, imageModelOptions.value)
  },
  { immediate: true }
)
watch(selectedChatModel, (value) => localStorage.setItem(STORAGE_SELECTED_CHAT_MODEL, String(value || 'gpt-4o-mini')))
watch(selectedImageModel, (value) => localStorage.setItem(STORAGE_SELECTED_IMAGE_MODEL, String(value || 'gpt-image-1')))
watch(customChatModel, (value) => localStorage.setItem(STORAGE_CHAT_MODEL, value))
watch(customImageModel, (value) => localStorage.setItem(STORAGE_IMAGE_MODEL, value))
watch(imageSize, (value) => localStorage.setItem(STORAGE_IMAGE_SIZE, String(value || '')))
watch(customImageWidth, (value) => localStorage.setItem(STORAGE_CUSTOM_IMAGE_WIDTH, String(value || 1024)))
watch(customImageHeight, (value) => localStorage.setItem(STORAGE_CUSTOM_IMAGE_HEIGHT, String(value || 1024)))
watch(imageCount, (value) => localStorage.setItem(STORAGE_IMAGE_COUNT, String(value || '1')))
watch(imageQuality, (value) => localStorage.setItem(STORAGE_IMAGE_QUALITY, String(value || 'auto')))
watch(imageBackground, (value) => localStorage.setItem(STORAGE_IMAGE_BACKGROUND, String(value || 'auto')))
watch(imageOutputFormat, (value) => localStorage.setItem(STORAGE_IMAGE_OUTPUT_FORMAT, String(value || 'png')))
onMounted(async () => {
  if (!appStore.publicSettingsLoaded) {
    await appStore.fetchPublicSettings()
  }
  await loadKeys()
  await loadAllKeyModels()
  ensureSelectedKeySupportsMode()
})
</script>
