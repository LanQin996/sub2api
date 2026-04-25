import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import UserDashboardStats from '../UserDashboardStats.vue'
import type { UserDashboardStats as UserStatsType } from '@/api/usage'

const messages: Record<string, string> = {
  'dashboard.balance': 'Balance',
  'dashboard.apiKeys': 'API Keys',
  'dashboard.todayRequests': 'Today Requests',
  'dashboard.todayCost': 'Today Cost',
  'dashboard.todayTokens': 'Today Tokens',
  'dashboard.totalTokens': 'Total Tokens',
  'dashboard.todayCacheHitRate': 'Today Cache Hit Rate',
  'dashboard.totalCacheHitRate': 'Historical Cache Hit Rate',
  'dashboard.cacheRead': 'Cache Read',
  'dashboard.performance': 'Performance',
  'dashboard.avgResponse': 'Avg Response',
  'dashboard.averageTime': 'Average time',
  'dashboard.actual': 'Actual',
  'dashboard.standard': 'Standard',
  'dashboard.input': 'Input',
  'dashboard.output': 'Output',
  'common.available': 'Available',
  'common.active': 'active',
  'common.total': 'Total',
}

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => messages[key] ?? key,
    }),
  }
})

const createStats = (): UserStatsType => ({
  total_api_keys: 2,
  active_api_keys: 1,
  total_requests: 20,
  total_input_tokens: 300,
  total_output_tokens: 80,
  total_cache_creation_tokens: 100,
  total_cache_read_tokens: 100,
  total_cache_hit_rate: 0.2,
  total_tokens: 580,
  total_cost: 1,
  total_actual_cost: 0.8,
  today_requests: 5,
  today_input_tokens: 100,
  today_output_tokens: 20,
  today_cache_creation_tokens: 50,
  today_cache_read_tokens: 50,
  today_cache_hit_rate: 0.25,
  today_tokens: 220,
  today_cost: 0.2,
  today_actual_cost: 0.16,
  average_duration_ms: 120,
  rpm: 1,
  tpm: 44,
})

describe('UserDashboardStats', () => {
  it('renders user cache hit rate cards', () => {
    const wrapper = mount(UserDashboardStats, {
      props: {
        stats: createStats(),
        balance: 10,
        isSimple: false,
      },
      global: {
        stubs: {
          Icon: true,
        },
      },
    })

    const text = wrapper.text()
    expect(text).toContain('Today Cache Hit Rate')
    expect(text).toContain('25.0%')
    expect(text).toContain('Historical Cache Hit Rate')
    expect(text).toContain('20.0%')
    expect(text).toContain('Cache Read: 50')
    expect(text).toContain('Cache Read: 100')
  })
})
