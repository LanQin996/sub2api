import { readdirSync, readFileSync } from 'node:fs'
import { extname, resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

import en from '../locales/en'
import zh from '../locales/zh'

const srcDir = resolve(process.cwd(), 'src')
const supportedExtensions = new Set(['.ts', '.vue'])
const literalTranslationCall = /(?:\$t|\bt)\(\s*(['"])([A-Za-z0-9_.-]+)\1/g

function sourceFiles(dir: string): string[] {
  const files: string[] = []
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = resolve(dir, entry.name)
    if (entry.isDirectory()) {
      if (entry.name !== '__tests__' && entry.name !== 'i18n') files.push(...sourceFiles(path))
      continue
    }
    if (supportedExtensions.has(extname(entry.name)) && !entry.name.endsWith('.spec.ts')) files.push(path)
  }
  return files
}

function referencedKeys(): string[] {
  const keys = new Set<string>()
  for (const file of sourceFiles(srcDir)) {
    const source = readFileSync(file, 'utf8')
    for (const match of source.matchAll(literalTranslationCall)) {
      if (!match[2].endsWith('.')) keys.add(match[2])
    }
  }
  return [...keys].sort()
}

function hasKey(messages: unknown, key: string): boolean {
  let current = messages
  for (const segment of key.split('.')) {
    if (!current || typeof current !== 'object' || !(segment in current)) return false
    current = (current as Record<string, unknown>)[segment]
  }
  return typeof current === 'string'
}

describe('referenced locale keys', () => {
  it('defines every literal translation key in English and Chinese', () => {
    const keys = referencedKeys()
    expect({
      missingInEnglish: keys.filter((key) => !hasKey(en, key)),
      missingInChinese: keys.filter((key) => !hasKey(zh, key)),
    }).toEqual({
      missingInEnglish: [],
      missingInChinese: [],
    })
  })
})
