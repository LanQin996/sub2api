import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key
  })
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard: vi.fn().mockResolvedValue(true)
  })
}))

import UseKeyModal from '../UseKeyModal.vue'

describe('UseKeyModal', () => {
  const mountOpenAIUseKeyModal = () => mount(UseKeyModal, {
    props: {
      show: true,
      apiKey: 'sk-test',
      baseUrl: 'https://example.com/v1',
      platform: 'openai'
    },
    global: {
      stubs: {
        BaseDialog: {
          template: '<div><slot /><slot name="footer" /></div>'
        },
        Icon: {
          template: '<span />'
        }
      }
    }
  })

  it('uses ai as the Codex CLI model provider name', async () => {
    const wrapper = mountOpenAIUseKeyModal()

    let codeBlock = wrapper.find('pre code')
    expect(codeBlock.text()).toContain('model_provider = "ai"')
    expect(codeBlock.text()).toContain('[model_providers.ai]')
    expect(codeBlock.text()).toContain('name = "ai"')
    expect(codeBlock.text()).not.toContain('model_provider = "OpenAI"')
    expect(codeBlock.text()).not.toContain('[model_providers.OpenAI]')
    expect(codeBlock.text()).not.toContain('name = "OpenAI"')

    const codexWsTab = wrapper.findAll('button').find((button) =>
      button.text().includes('keys.useKeyModal.cliTabs.codexCliWs')
    )

    expect(codexWsTab).toBeDefined()
    await codexWsTab!.trigger('click')
    await nextTick()

    codeBlock = wrapper.find('pre code')
    expect(codeBlock.text()).toContain('model_provider = "ai"')
    expect(codeBlock.text()).toContain('[model_providers.ai]')
    expect(codeBlock.text()).toContain('name = "ai"')
    expect(codeBlock.text()).toContain('supports_websockets = true')
    expect(codeBlock.text()).not.toContain('model_provider = "OpenAI"')
    expect(codeBlock.text()).not.toContain('[model_providers.OpenAI]')
    expect(codeBlock.text()).not.toContain('name = "OpenAI"')
  })

  it('renders GPT-5.4 mini entry in OpenCode config', async () => {
    const wrapper = mountOpenAIUseKeyModal()

    const opencodeTab = wrapper.findAll('button').find((button) =>
      button.text().includes('keys.useKeyModal.cliTabs.opencode')
    )

    expect(opencodeTab).toBeDefined()
    await opencodeTab!.trigger('click')
    await nextTick()

    const codeBlock = wrapper.find('pre code')
    expect(codeBlock.exists()).toBe(true)
    expect(codeBlock.text()).toContain('"name": "GPT-5.4 Mini"')
    expect(codeBlock.text()).not.toContain('"name": "GPT-5.4 Nano"')
  })
})
