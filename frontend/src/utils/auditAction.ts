type Translate = (key: string) => string
type TranslationExists = (key: string) => boolean

const exactActionKeys: Record<string, string> = {
  'usage.dashboard.api_keys_usage.create': 'apiKeysUsageQuery',
  'user.api_keys.create': 'apiKeyCreate',
  'user.api_keys.update': 'apiKeyUpdate',
  'user.api_keys.delete': 'apiKeyDelete',
}

function humanize(segment: string): string {
  return segment
    .split('_')
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ')
}

export function formatAuditAction(action: string, t: Translate, te: TranslationExists): string {
  const normalized = action.trim()
  if (!normalized) return normalized

  const exactKey = exactActionKeys[normalized]
  if (exactKey) return t(`admin.audit.actions.exact.${exactKey}`)

  return normalized
    .split('.')
    .filter(Boolean)
    .map((segment) => {
      const key = `admin.audit.actions.segments.${segment}`
      return te(key) ? t(key) : humanize(segment)
    })
    .join(' / ')
}
