import { describe, expect, it } from 'vitest'

import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'
import { formatAuditAction } from '../auditAction'

function translator(messages: Record<string, any>) {
  const resolve = (key: string): unknown => key.split('.').reduce<unknown>((value, segment) => {
    if (!value || typeof value !== 'object') return undefined
    return (value as Record<string, unknown>)[segment]
  }, messages)

  return {
    t: (key: string) => String(resolve(key) ?? key),
    te: (key: string) => typeof resolve(key) === 'string',
  }
}

describe('formatAuditAction', () => {
  it.each([
    [en, 'usage.dashboard.api_keys_usage.create', 'Query API Key Usage'],
    [zh, 'usage.dashboard.api_keys_usage.create', '查询 API 密钥用量'],
    [en, 'user.api_keys.update', 'Update API Key'],
    [zh, 'user.api_keys.update', '更新 API 密钥'],
  ])('formats exact action labels', (messages, action, expected) => {
    const { t, te } = translator(messages)
    expect(formatAuditAction(action, t, te)).toBe(expected)
  })

  it('translates known segments and humanizes unknown ones', () => {
    const { t, te } = translator(en)
    expect(formatAuditAction('admin.future_jobs.retry_now', t, te)).toBe('Admin / Future Jobs / Retry Now')
  })
})
