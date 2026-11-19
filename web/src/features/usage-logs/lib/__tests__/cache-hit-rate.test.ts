/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { describe, expect, test } from 'vitest'

import { toIntlLocale } from '@/i18n/languages'

import {
  formatCacheHitRate,
  formatCacheHitRateCsv,
  getUsageLogCacheMetrics,
} from '../cache-hit-rate'

describe('getUsageLogCacheMetrics', () => {
  test('uses prompt_tokens directly as total input for OpenAI/Gemini logs', () => {
    const metrics = getUsageLogCacheMetrics(
      { prompt_tokens: 2000 },
      { cache_tokens: 1500, cache_creation_tokens: 200 }
    )

    expect(metrics.totalInputTokens).toBe(2000)
    expect(metrics.cacheReadTokens).toBe(1500)
    expect(metrics.cacheWriteTokens).toBe(200)
    expect(metrics.hitRate).toBe(75)
  })

  test('sums uncached prompt, cache read, and cache write for Anthropic semantic logs', () => {
    const metrics = getUsageLogCacheMetrics(
      { prompt_tokens: 50 },
      {
        usage_semantic: 'anthropic',
        claude: true,
        cache_tokens: 5000,
        cache_write_tokens: 5000,
        cache_creation_tokens_5m: 5000,
      }
    )

    expect(metrics.totalInputTokens).toBe(10050)
    expect(metrics.hitRate).toBeCloseTo((5000 / 10050) * 100, 6)
    expect(formatCacheHitRateCsv(metrics.hitRate)).toBe('49.8%')
  })

  test('computes 100% for fully cached Claude requests with zero uncached prompt tokens', () => {
    const metrics = getUsageLogCacheMetrics(
      { prompt_tokens: 0 },
      {
        usage_semantic: 'anthropic',
        cache_tokens: 8000,
      }
    )

    expect(metrics.totalInputTokens).toBe(8000)
    expect(metrics.hitRate).toBe(100)
  })

  test('handles legacy Claude logs that only carry claude=true', () => {
    const metrics = getUsageLogCacheMetrics(
      { prompt_tokens: 100 },
      {
        claude: true,
        cache_tokens: 400,
        cache_creation_tokens_5m: 300,
        cache_creation_tokens_1h: 200,
      }
    )

    expect(metrics.cacheWriteTokens).toBe(500)
    expect(metrics.totalInputTokens).toBe(1000)
    expect(metrics.hitRate).toBe(40)
  })

  test('prefers explicit input_tokens_total or tiered billing len when present', () => {
    const fromTotal = getUsageLogCacheMetrics(
      { prompt_tokens: 50 },
      {
        cache_tokens: 450,
        input_tokens_total: 1000,
      }
    )
    expect(fromTotal.totalInputTokens).toBe(1000)
    expect(fromTotal.hitRate).toBe(45)

    const fromBillingLen = getUsageLogCacheMetrics(
      { prompt_tokens: 50 },
      {
        cache_tokens: 250,
        billing_tokens: { len: 1000, p: 50, cr: 250 },
      }
    )
    expect(fromBillingLen.totalInputTokens).toBe(1000)
    expect(fromBillingLen.hitRate).toBe(25)
  })

  test('clamps hit rate to 100% when upstream reports cache_tokens exceeding prompt_tokens', () => {
    const metrics = getUsageLogCacheMetrics(
      { prompt_tokens: 1000 },
      { cache_tokens: 1500 }
    )

    expect(metrics.hitRate).toBe(100)
  })

  test('returns 0% when input tokens exist with no cache hits and null when input tokens are zero', () => {
    const missed = getUsageLogCacheMetrics({ prompt_tokens: 500 }, {})
    expect(missed.hitRate).toBe(0)

    const nonToken = getUsageLogCacheMetrics({ prompt_tokens: 0 }, {})
    expect(nonToken.hitRate).toBeNull()
    expect(formatCacheHitRate(nonToken.hitRate, 'en')).toBe('-')
    expect(formatCacheHitRateCsv(nonToken.hitRate)).toBe('')
  })
})

describe('formatCacheHitRate locale handling', () => {
  test.each([
    'en',
    'zh',
    'zhCN',
    'zhTW',
    'fr',
    'ru',
    'ja',
    'vi',
    'invalid_tag',
  ])('formats percentages cleanly after toIntlLocale(%s)', (lang) => {
    const locale = toIntlLocale(lang)
    expect(() => formatCacheHitRate(82.44, locale)).not.toThrow()
    expect(formatCacheHitRate(0, locale)).toContain('0')
    expect(formatCacheHitRate(100, locale)).toContain('100')
  })
})
