import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import AccountTableFilters from '../AccountTableFilters.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

const SelectStub = {
  name: 'Select',
  props: ['modelValue', 'options'],
  template: '<div class="select-stub" :data-options="JSON.stringify(options)" />',
}

const SearchInputStub = {
  name: 'SearchInput',
  template: '<input />',
}

describe('AccountTableFilters', () => {
  it('includes disabled in the account status filter', () => {
    const wrapper = mount(AccountTableFilters, {
      props: {
        searchQuery: '',
        filters: {
          platform: '',
          type: '',
          status: '',
          privacy_mode: '',
          group: '',
        },
        groups: [],
      },
      global: {
        stubs: {
          Select: SelectStub,
          SearchInput: SearchInputStub,
        },
      },
    })

    const statusSelect = wrapper.findAll('.select-stub')[2]
    const statusOptions = JSON.parse(statusSelect.attributes('data-options') || '[]')

    expect(statusOptions).toContainEqual({
      value: 'disabled',
      label: 'admin.accounts.status.disabled',
    })
  })
})
