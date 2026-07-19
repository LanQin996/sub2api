import { describe, expect, it } from 'vitest'

import en from '../locales/en'
import zh from '../locales/zh'

function collectLeafKeys(node: unknown, path = '', out: string[] = []): string[] {
  if (!node || typeof node !== 'object' || Array.isArray(node)) {
    if (path) out.push(path)
    return out
  }

  for (const [key, value] of Object.entries(node as Record<string, unknown>)) {
    collectLeafKeys(value, path ? `${path}.${key}` : key, out)
  }
  return out
}

describe('locale key parity', () => {
  it('keeps English and Chinese leaf keys in sync', () => {
    const enKeys = collectLeafKeys(en).sort()
    const zhKeys = collectLeafKeys(zh).sort()

    expect({
      missingInEnglish: zhKeys.filter((key) => !enKeys.includes(key)),
      missingInChinese: enKeys.filter((key) => !zhKeys.includes(key)),
    }).toEqual({
      missingInEnglish: [],
      missingInChinese: [],
    })
  })
})
