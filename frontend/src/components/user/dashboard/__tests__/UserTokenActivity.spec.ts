import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { ref } from 'vue'

import UserTokenActivity from '../UserTokenActivity.vue'
import type { TokenActivityResponse } from '@/api/usage'

const messages: Record<string, string> = {
  'dashboard.tokenActivity.title': 'Token activity',
  'dashboard.tokenActivity.totalTokens': 'Total Tokens',
  'dashboard.tokenActivity.peakTokens': 'Peak Tokens',
  'dashboard.tokenActivity.currentStreak': 'Current Streak',
  'dashboard.tokenActivity.longestStreak': 'Longest Streak',
  'dashboard.tokenActivity.daily': 'Daily',
  'dashboard.tokenActivity.weekly': 'Weekly',
  'dashboard.tokenActivity.cumulative': 'Cumulative',
  'dashboard.tokenActivity.unsettled': 'Not settled yet',
}

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    locale: ref('en-US'),
    t: (key: string, params?: Record<string, unknown>) => {
      if (key === 'dashboard.tokenActivity.days') return `${params?.count} days`
      if (key === 'dashboard.tokenActivity.tokenCount') return `${params?.count} Tokens`
      if (key === 'dashboard.tokenActivity.updatedThrough') return `Updated through ${params?.date} · ${params?.timezone}`
      return messages[key] ?? key
    },
  }),
}))

const activity: TokenActivityResponse = {
  start_date: '2025-08-01',
  end_date: '2026-07-03',
  data_through_date: '2026-07-02',
  timezone: 'Asia/Shanghai',
  updated_at: '2026-07-03T03:00:00+08:00',
  summary: { total_tokens: 40, current_streak_days: 2, longest_streak_days: 2 },
  days: [
    { date: '2026-07-01', total_tokens: 10 },
    { date: '2026-07-02', total_tokens: 30 },
  ],
}

describe('UserTokenActivity', () => {
  it('renders snapshot summaries and marks today unsettled', () => {
    const wrapper = mount(UserTokenActivity, { props: { activity } })

    expect(wrapper.text()).toContain('Updated through 2026-07-02 · Asia/Shanghai')
    expect(wrapper.text()).toContain('2 days')
    expect(wrapper.find('[title*="Not settled yet"]').exists()).toBe(true)
    expect(wrapper.findAll('[role="grid"] button').length).toBeGreaterThan(300)
  })

  it('updates peak tokens when switching aggregation view', async () => {
    const wrapper = mount(UserTokenActivity, { props: { activity } })
    expect(wrapper.text()).toContain('30')

    await wrapper.get('button[role="tab"]:nth-child(2)').trigger('click')

    expect(wrapper.text()).toContain('40')
    expect(wrapper.get('button[role="tab"]:nth-child(2)').attributes('aria-selected')).toBe('true')
  })
})
