import { describe, it, expect, vi, beforeEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ImportDataModal from '@/components/admin/account/ImportDataModal.vue'
import { adminAPI } from '@/api/admin'

const showError = vi.fn()
const showSuccess = vi.fn()

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess
  })
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      importData: vi.fn()
    }
  }
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) => {
      if (!params) return key
      return `${key}:${JSON.stringify(params)}`
    }
  })
}))

describe('ImportDataModal', () => {
  beforeEach(() => {
    showError.mockReset()
    showSuccess.mockReset()
    vi.mocked(adminAPI.accounts.importData).mockReset()
  })

  it('未选择文件时提示错误', async () => {
    const wrapper = mount(ImportDataModal, {
      props: { show: true },
      global: {
        stubs: {
          BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' }
        }
      }
    })

    await wrapper.find('form').trigger('submit')
    expect(showError).toHaveBeenCalledWith('admin.accounts.dataImportSelectFile')
  })

  it('无效 JSON 时提示解析失败', async () => {
    const wrapper = mount(ImportDataModal, {
      props: { show: true },
      global: {
        stubs: {
          BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' }
        }
      }
    })

    const input = wrapper.find('input[type="file"]')
    const file = new File(['invalid json'], 'data.json', { type: 'application/json' })
    Object.defineProperty(file, 'text', {
      value: () => Promise.resolve('invalid json')
    })
    Object.defineProperty(input.element, 'files', {
      value: [file]
    })

    await input.trigger('change')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('admin.accounts.dataImportParseFailed')
  })

  it('支持一次选择多个 JSON 文件并合并导入', async () => {
    vi.mocked(adminAPI.accounts.importData).mockResolvedValue({
      proxy_created: 0,
      proxy_reused: 0,
      proxy_failed: 0,
      account_created: 2,
      account_failed: 0
    })

    const wrapper = mount(ImportDataModal, {
      props: { show: true },
      global: {
        stubs: {
          BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' }
        }
      }
    })

    const payloadA = {
      exported_at: '2026-05-21T00:00:00Z',
      proxies: [],
      accounts: [
        {
          name: 'acc-a',
          platform: 'openai',
          type: 'oauth',
          credentials: { token: 'a' },
          concurrency: 1,
          priority: 50
        }
      ]
    }
    const payloadB = {
      exported_at: '2026-05-21T00:00:01Z',
      proxies: [
        {
          proxy_key: 'http|127.0.0.1|8080||',
          name: 'proxy',
          protocol: 'http',
          host: '127.0.0.1',
          port: 8080,
          status: 'active'
        }
      ],
      accounts: [
        {
          name: 'acc-b',
          platform: 'openai',
          type: 'oauth',
          credentials: { token: 'b' },
          proxy_key: 'http|127.0.0.1|8080||',
          concurrency: 1,
          priority: 50
        }
      ]
    }

    const input = wrapper.find('input[type="file"]')
    const fileA = new File([JSON.stringify(payloadA)], 'a.json', { type: 'application/json' })
    const fileB = new File([JSON.stringify(payloadB)], 'b.json', { type: 'application/json' })
    Object.defineProperty(fileA, 'text', {
      value: () => Promise.resolve(JSON.stringify(payloadA))
    })
    Object.defineProperty(fileB, 'text', {
      value: () => Promise.resolve(JSON.stringify(payloadB))
    })
    Object.defineProperty(input.element, 'files', {
      value: [fileA, fileB]
    })

    await input.trigger('change')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(adminAPI.accounts.importData).toHaveBeenCalledWith({
      data: expect.objectContaining({
        proxies: payloadB.proxies,
        accounts: [...payloadA.accounts, ...payloadB.accounts]
      }),
      skip_default_group_bind: true
    })
    expect(showSuccess).toHaveBeenCalled()
  })
})
