import { describe, it, expect, vi, beforeEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ImportDataModal from '@/components/admin/account/ImportDataModal.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import { adminAPI } from '@/api/admin'
import type { AdminDataImportResult, AdminGroup } from '@/types'

const showError = vi.fn()
const showSuccess = vi.fn()
const showWarning = vi.fn()

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
    showWarning
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
    t: (key: string) => key
  })
}))

const mountModal = (groups: AdminGroup[] = []) =>
  mount(ImportDataModal, {
    props: { show: true, groups },
    global: {
      stubs: {
        BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' }
      }
    }
  })

const makeJsonFile = (name: string, content: string, type = 'application/json') => {
  const file = new File([content], name, { type })
  Object.defineProperty(file, 'text', {
    value: () => Promise.resolve(content)
  })
  return file
}

const setInputFiles = (element: Element, files: File[]) => {
  Object.defineProperty(element, 'files', {
    value: files,
    configurable: true
  })
}

const importGroups = [
  {
    id: 11,
    name: 'OpenAI Primary',
    platform: 'openai',
    rate_multiplier: 1,
    account_count: 2
  },
  {
    id: 12,
    name: 'OpenAI Backup',
    platform: 'openai',
    rate_multiplier: 1,
    account_count: 0
  }
] as AdminGroup[]

describe('ImportDataModal', () => {
  beforeEach(async () => {
    showError.mockReset()
    showSuccess.mockReset()
    showWarning.mockReset()
    const { adminAPI } = await import('@/api/admin')
    vi.mocked(adminAPI.accounts.importData).mockReset()
  })

  it('未选择文件时提示错误', async () => {
    const wrapper = mountModal()

    await wrapper.find('form').trigger('submit')
    expect(showError).toHaveBeenCalledWith('admin.accounts.dataImportSelectFile')
  })

  it('无效 JSON 时按文件名提示解析失败', async () => {
    const { adminAPI } = await import('@/api/admin')
    const wrapper = mountModal()

    const input = wrapper.find('input[type="file"]')
    setInputFiles(input.element, [makeJsonFile('data.json', 'invalid json')])

    await input.trigger('change')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('admin.accounts.dataImportParseFailedFile')
    expect(adminAPI.accounts.importData).not.toHaveBeenCalled()
  })

  it('不是导出数据的 JSON 按文件名拒绝', async () => {
    const { adminAPI } = await import('@/api/admin')
    const wrapper = mountModal()

    const input = wrapper.find('input[type="file"]')
    setInputFiles(input.element, [makeJsonFile('random.json', JSON.stringify({ name: 'test' }))])

    await input.trigger('change')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('admin.accounts.dataImportInvalidFile')
    expect(adminAPI.accounts.importData).not.toHaveBeenCalled()
  })

  it('无有效 JSON 的选择不清空已有选择', async () => {
    const { adminAPI } = await import('@/api/admin')
    vi.mocked(adminAPI.accounts.importData).mockResolvedValue({
      proxy_created: 0,
      proxy_reused: 0,
      proxy_failed: 0,
      account_created: 1,
      account_failed: 0
    })

    const wrapper = mountModal()
    const input = wrapper.find('input[type="file"]')

    const valid = makeJsonFile(
      'valid.json',
      JSON.stringify({ exported_at: '2026-07-05T00:00:00Z', proxies: [], accounts: [{ name: 'a' }] })
    )
    setInputFiles(input.element, [valid])
    await input.trigger('change')

    setInputFiles(input.element, [new File(['hello'], 'notes.txt', { type: 'text/plain' })])
    await input.trigger('change')
    expect(showError).toHaveBeenCalledWith('admin.accounts.dataImportSelectFile')

    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(adminAPI.accounts.importData).toHaveBeenCalledWith({
      data: expect.objectContaining({
        accounts: [{ name: 'a' }]
      }),
      skip_default_group_bind: true
    })
  })

  it('merges multiple selected JSON files before importing', async () => {
    const { adminAPI } = await import('@/api/admin')
    vi.mocked(adminAPI.accounts.importData).mockResolvedValue({
      proxy_created: 0,
      proxy_reused: 0,
      proxy_failed: 0,
      account_created: 2,
      account_failed: 0
    })

    const wrapper = mountModal()

    const input = wrapper.find('input[type="file"]')
    const first = makeJsonFile(
      'first.json',
      JSON.stringify({ exported_at: '2026-07-05T00:00:00Z', proxies: [], accounts: [{ name: 'a' }] })
    )
    const second = makeJsonFile(
      'second.json',
      JSON.stringify({
        exported_at: '2026-07-05T00:00:01Z',
        proxies: [{ proxy_key: 'p' }],
        accounts: [{ name: 'b' }]
      })
    )
    setInputFiles(input.element, [first, second])

    await input.trigger('change')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(adminAPI.accounts.importData).toHaveBeenCalledWith({
      data: expect.objectContaining({
        proxies: [{ proxy_key: 'p' }],
        accounts: [{ name: 'a' }, { name: 'b' }]
      }),
      skip_default_group_bind: true
    })
    expect(showSuccess).toHaveBeenCalledWith('admin.accounts.dataImportSuccess')
  })

  it('随导入请求发送所选群组并在重新打开时清空选择', async () => {
    vi.mocked(adminAPI.accounts.importData).mockResolvedValue({
      proxy_created: 0,
      proxy_reused: 0,
      proxy_failed: 0,
      account_created: 1,
      account_failed: 0
    })

    const wrapper = mountModal(importGroups)
    const groupCheckboxes = wrapper.findAll('input[type="checkbox"]')
    expect(groupCheckboxes).toHaveLength(2)
    await groupCheckboxes[0]!.setValue(true)
    await groupCheckboxes[1]!.setValue(true)

    const input = wrapper.find('input[type="file"]')
    setInputFiles(input.element, [
      makeJsonFile(
        'accounts.json',
        JSON.stringify({
          exported_at: '2026-07-05T00:00:00Z',
          proxies: [],
          accounts: [{ name: 'a', platform: 'openai' }]
        })
      )
    ])
    await input.trigger('change')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(adminAPI.accounts.importData).toHaveBeenCalledWith({
      data: expect.objectContaining({ accounts: [{ name: 'a', platform: 'openai' }] }),
      skip_default_group_bind: true,
      group_ids: [11, 12]
    })

    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    const reopenedCheckboxes = wrapper.findAll('input[type="checkbox"]')
    expect((reopenedCheckboxes[0]!.element as HTMLInputElement).checked).toBe(false)
    expect((reopenedCheckboxes[1]!.element as HTMLInputElement).checked).toBe(false)
  })

  it('所选群组未覆盖导入账号平台时拒绝请求', async () => {
    const wrapper = mountModal(importGroups)
    await wrapper.findAll('input[type="checkbox"]')[0]!.setValue(true)

    const input = wrapper.find('input[type="file"]')
    setInputFiles(input.element, [
      makeJsonFile(
        'anthropic.json',
        JSON.stringify({
          exported_at: '2026-07-05T00:00:00Z',
          proxies: [],
          accounts: [
            {
              name: 'claude',
              platform: 'anthropic',
              type: 'oauth',
              credentials: { token: 'test' },
              concurrency: 1,
              priority: 1
            }
          ]
        })
      )
    ])
    await input.trigger('change')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('admin.accounts.dataImportGroupPlatformMismatch')
    expect(adminAPI.accounts.importData).not.toHaveBeenCalled()
  })

  it('Antigravity 混合调度账号可绑定 Anthropic 群组', async () => {
    vi.mocked(adminAPI.accounts.importData).mockResolvedValue({
      proxy_created: 0,
      proxy_reused: 0,
      proxy_failed: 0,
      account_created: 1,
      account_failed: 0
    })
    const groups = [
      {
        id: 21,
        name: 'Claude Mixed',
        platform: 'anthropic',
        rate_multiplier: 1,
        account_count: 0
      }
    ] as AdminGroup[]
    const wrapper = mountModal(groups)
    await wrapper.find('input[type="checkbox"]').setValue(true)

    const input = wrapper.find('input[type="file"]')
    setInputFiles(input.element, [
      makeJsonFile(
        'antigravity.json',
        JSON.stringify({
          exported_at: '2026-07-05T00:00:00Z',
          proxies: [],
          accounts: [
            {
              name: 'antigravity-mixed',
              platform: 'antigravity',
              type: 'oauth',
              credentials: { token: 'test' },
              extra: { mixed_scheduling: true },
              concurrency: 1,
              priority: 1
            }
          ]
        })
      )
    ])
    await input.trigger('change')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(adminAPI.accounts.importData).toHaveBeenCalledWith({
      data: expect.objectContaining({
        accounts: [expect.objectContaining({ platform: 'antigravity' })]
      }),
      skip_default_group_bind: true,
      group_ids: [21]
    })
    expect(showError).not.toHaveBeenCalledWith('admin.accounts.dataImportGroupPlatformMismatch')
  })

  it('群组与账号平台别名会去空格并忽略大小写', async () => {
    vi.mocked(adminAPI.accounts.importData).mockResolvedValue({
      proxy_created: 0,
      proxy_reused: 0,
      proxy_failed: 0,
      account_created: 1,
      account_failed: 0
    })
    const groups = [
      {
        id: 22,
        name: 'Claude Alias',
        platform: ' Anthropic ',
        rate_multiplier: 1,
        account_count: 0
      }
    ] as AdminGroup[]
    const wrapper = mountModal(groups)
    await wrapper.find('input[type="checkbox"]').setValue(true)

    const input = wrapper.find('input[type="file"]')
    setInputFiles(input.element, [
      makeJsonFile(
        'claude.json',
        JSON.stringify({
          exported_at: '2026-07-05T00:00:00Z',
          proxies: [],
          accounts: [
            {
              name: 'claude-alias',
              platform: ' CLAUDE ',
              type: 'oauth',
              credentials: { token: 'test' },
              concurrency: 1,
              priority: 1
            }
          ]
        })
      )
    ])
    await input.trigger('change')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(adminAPI.accounts.importData).toHaveBeenCalledWith(
      expect.objectContaining({ group_ids: [22] })
    )
    expect(showError).not.toHaveBeenCalledWith('admin.accounts.dataImportGroupPlatformMismatch')
  })

  it('无效账号不阻断同批次合法账号导入', async () => {
    vi.mocked(adminAPI.accounts.importData).mockResolvedValue({
      proxy_created: 0,
      proxy_reused: 0,
      proxy_failed: 0,
      account_created: 1,
      account_failed: 1
    })
    const wrapper = mountModal(importGroups)
    await wrapper.find('input[type="checkbox"]').setValue(true)

    const input = wrapper.find('input[type="file"]')
    setInputFiles(input.element, [
      makeJsonFile(
        'partial.json',
        JSON.stringify({
          exported_at: '2026-07-05T00:00:00Z',
          proxies: [],
          accounts: [
            {
              name: 'valid',
              platform: 'openai',
              type: 'oauth',
              credentials: { token: 'test' },
              concurrency: 1,
              priority: 1
            },
            { name: 'invalid' }
          ]
        })
      )
    ])
    await input.trigger('change')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(adminAPI.accounts.importData).toHaveBeenCalledOnce()
    expect(showError).toHaveBeenCalledWith('admin.accounts.dataImportCompletedWithErrors')
  })

  it('收到真实形状的混合渠道 409 后确认并携带风险标记重试', async () => {
    vi.mocked(adminAPI.accounts.importData)
      .mockRejectedValueOnce({
        status: 409,
        code: 409,
        reason: 'MIXED_CHANNEL_WARNING',
        message: 'mixed channel warning'
      })
      .mockResolvedValueOnce({
        proxy_created: 0,
        proxy_reused: 0,
        proxy_failed: 0,
        account_created: 1,
        account_failed: 0
      })
    const wrapper = mountModal(importGroups)
    await wrapper.find('input[type="checkbox"]').setValue(true)

    const input = wrapper.find('input[type="file"]')
    setInputFiles(input.element, [
      makeJsonFile(
        'mixed-channel.json',
        JSON.stringify({
          exported_at: '2026-07-05T00:00:00Z',
          proxies: [],
          accounts: [
            {
              name: 'openai',
              platform: 'openai',
              type: 'oauth',
              credentials: { token: 'test' },
              concurrency: 1,
              priority: 1
            }
          ]
        })
      )
    ])
    await input.trigger('change')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    const confirmDialog = wrapper.getComponent(ConfirmDialog)
    expect(confirmDialog.props('show')).toBe(true)
    expect(adminAPI.accounts.importData).toHaveBeenNthCalledWith(1, {
      data: expect.any(Object),
      skip_default_group_bind: true,
      group_ids: [11]
    })

    confirmDialog.vm.$emit('confirm')
    await flushPromises()

    expect(adminAPI.accounts.importData).toHaveBeenNthCalledWith(2, {
      data: expect.any(Object),
      skip_default_group_bind: true,
      group_ids: [11],
      confirm_mixed_channel_risk: true
    })
    expect(confirmDialog.props('show')).toBe(false)
    expect(showSuccess).toHaveBeenCalledWith('admin.accounts.dataImportSuccess')
  })

  it('混合渠道确认在群组变化后立即失效', async () => {
    vi.mocked(adminAPI.accounts.importData).mockRejectedValueOnce({
      status: 409,
      code: 409,
      reason: 'MIXED_CHANNEL_WARNING',
      message: 'mixed channel warning'
    })
    const wrapper = mountModal(importGroups)
    const groupCheckboxes = wrapper.findAll('input[type="checkbox"]')
    await groupCheckboxes[0]!.setValue(true)

    const input = wrapper.find('input[type="file"]')
    setInputFiles(input.element, [
      makeJsonFile(
        'mixed-channel.json',
        JSON.stringify({
          exported_at: '2026-07-05T00:00:00Z',
          proxies: [],
          accounts: [
            {
              name: 'openai',
              platform: 'openai',
              type: 'oauth',
              credentials: { token: 'test' },
              concurrency: 1,
              priority: 1
            }
          ]
        })
      )
    ])
    await input.trigger('change')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    const confirmDialog = wrapper.getComponent(ConfirmDialog)
    expect(confirmDialog.props('show')).toBe(true)

    await groupCheckboxes[1]!.setValue(true)
    await flushPromises()
    expect(confirmDialog.props('show')).toBe(false)

    confirmDialog.vm.$emit('confirm')
    await flushPromises()
    expect(adminAPI.accounts.importData).toHaveBeenCalledOnce()
  })

  it('导入进行中禁用群组选择', async () => {
    let resolveImport!: (result: AdminDataImportResult) => void
    vi.mocked(adminAPI.accounts.importData).mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveImport = resolve
        })
    )

    const wrapper = mountModal(importGroups)
    await wrapper.findAll('input[type="checkbox"]')[0]!.setValue(true)
    const input = wrapper.find('input[type="file"]')
    setInputFiles(input.element, [
      makeJsonFile(
        'accounts.json',
        JSON.stringify({
          exported_at: '2026-07-05T00:00:00Z',
          proxies: [],
          accounts: [{ name: 'a', platform: 'openai' }]
        })
      )
    ])
    await input.trigger('change')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    const groupFieldset = wrapper.get('[data-testid="import-groups"]')
    expect(groupFieldset.attributes('disabled')).toBeDefined()

    resolveImport({
      proxy_created: 0,
      proxy_reused: 0,
      proxy_failed: 0,
      account_created: 1,
      account_failed: 0
    })
    await flushPromises()
    expect(groupFieldset.attributes('disabled')).toBeUndefined()
  })

  it('部分成功时关闭弹窗仍通知父组件刷新', async () => {
    const { adminAPI } = await import('@/api/admin')
    vi.mocked(adminAPI.accounts.importData).mockResolvedValue({
      proxy_created: 0,
      proxy_reused: 0,
      proxy_failed: 0,
      account_created: 1,
      account_failed: 1
    })

    const wrapper = mountModal()
    const input = wrapper.find('input[type="file"]')
    setInputFiles(input.element, [
      makeJsonFile(
        'mixed.json',
        JSON.stringify({
          exported_at: '2026-07-05T00:00:00Z',
          proxies: [],
          accounts: [{ name: 'a' }, { name: 'b' }]
        })
      )
    ])

    await input.trigger('change')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('admin.accounts.dataImportCompletedWithErrors')
    expect(wrapper.emitted('imported')).toBeUndefined()

    // 第二个 btn-secondary 是 footer 的取消按钮(第一个是选择文件)
    await wrapper.findAll('button.btn-secondary')[1]!.trigger('click')

    expect(wrapper.emitted('imported')).toHaveLength(1)
    expect(wrapper.emitted('close')).toHaveLength(1)
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
      props: { show: true, groups: [] },
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
