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

import type { QuotaDataItem } from '@/features/dashboard/types'
import { toIntlLocale } from '@/i18n/languages'

import {
  formatDashboardHitRate,
  processCacheHitRateChartData,
} from '../cache-hit-rate-charts'
import { calculateDashboardStats } from '../stats'

describe('calculateDashboardStats cache hit rate', () => {
  test('excludes pre-upgrade cache_read_tokens with zero input_tokens from hit rate while keeping Cache Read total', () => {
    const data: QuotaDataItem[] = [
      {
        created_at: 1767225600,
        model_name: 'claude-sonnet',
        count: 5,
        quota: 1000,
        token_used: 50000,
        input_tokens: 0,
        cache_read_tokens: 500000,
        cache_creation_tokens: 10000,
      },
      {
        created_at: 1767312000,
        model_name: 'claude-sonnet',
        count: 3,
        quota: 500,
        token_used: 30000,
        input_tokens: 200000,
        cache_read_tokens: 100000,
        cache_creation_tokens: 5000,
      },
    ]

    const stats = calculateDashboardStats(data)

    expect(stats.totalCacheReadTokens).toBe(600000)
    expect(stats.totalCacheCreationTokens).toBe(15000)
    expect(stats.totalInputTokens).toBe(200000)
    expect(stats.totalCacheHitReadTokens).toBe(100000)
    expect(stats.cacheHitRate).toBe(50)
  })

  test('prefers backend cache_hit_read_tokens when provided and falls back to 0% on zero denominator', () => {
    const mixed: QuotaDataItem[] = [
      {
        created_at: 1767312000,
        model_name: 'claude-sonnet',
        count: 4,
        input_tokens: 200000,
        cache_read_tokens: 600000,
        cache_hit_read_tokens: 100000,
      },
    ]
    expect(calculateDashboardStats(mixed).cacheHitRate).toBe(50)

    const zeroDenom: QuotaDataItem[] = [
      {
        created_at: 1767312000,
        model_name: 'midjourney',
        count: 2,
        input_tokens: 0,
        cache_read_tokens: 0,
      },
    ]
    expect(calculateDashboardStats(zeroDenom).cacheHitRate).toBe(0)
  })

  test('clamps hit rate to 100% when cache_read_tokens exceeds input_tokens', () => {
    const stats = calculateDashboardStats([
      {
        created_at: 1767312000,
        model_name: 'gpt-5',
        count: 1,
        input_tokens: 1000,
        cache_read_tokens: 1500,
      },
    ])

    expect(stats.totalCacheHitReadTokens).toBe(1000)
    expect(stats.cacheHitRate).toBe(100)
  })
})

describe('processCacheHitRateChartData', () => {
  test('plots Overall plus per-model lines with null gaps on empty buckets and 0% on active missed buckets', () => {
    const base = new Date('2026-06-01T10:00:00').getTime() / 1000
    const data: QuotaDataItem[] = [
      {
        created_at: base,
        model_name: 'claude-sonnet',
        count: 2,
        input_tokens: 10000,
        cache_read_tokens: 8000,
        cache_creation_tokens: 1000,
      },
      {
        created_at: base,
        model_name: 'gpt-5',
        count: 1,
        input_tokens: 10000,
        cache_read_tokens: 0,
        cache_creation_tokens: 0,
      },
      {
        // Next hour: only gpt-5 had traffic (0% hit), claude-sonnet had no requests.
        created_at: base + 3600,
        model_name: 'gpt-5',
        count: 2,
        input_tokens: 5000,
        cache_read_tokens: 0,
      },
    ]

    const result = processCacheHitRateChartData(data, 'hour', (k) => k, 'en')
    const trendValues = result.spec_hit_rate_trend.data[0].values as Array<{
      Time: string
      Series: string
      HitRate: number | null
    }>

    expect(result.spec_hit_rate_trend.invalidType).toBe('break')
    expect(result.overallHitRate).toBe(32)
    expect(result.overallHitRateDisplay).toBe('32%')

    // First active hour: Overall = 8000 / 20000 = 40%, claude-sonnet = 80%, gpt-5 = 0%
    const hour10 = trendValues.filter((v) => v.Time === '06-01 10:00')
    expect(hour10.find((v) => v.Series === 'Overall')?.HitRate).toBe(40)
    expect(hour10.find((v) => v.Series === 'claude-sonnet')?.HitRate).toBe(80)
    expect(hour10.find((v) => v.Series === 'gpt-5')?.HitRate).toBe(0)

    // Second active hour: gpt-5 missed (0%), claude-sonnet had no requests (null gap)
    const hour11 = trendValues.filter((v) => v.Time === '06-01 11:00')
    expect(hour11.find((v) => v.Series === 'Overall')?.HitRate).toBe(0)
    expect(hour11.find((v) => v.Series === 'claude-sonnet')?.HitRate).toBeNull()
    expect(hour11.find((v) => v.Series === 'gpt-5')?.HitRate).toBe(0)

    // Padded earlier hour with zero traffic leaves null gap
    const hour09 = trendValues.filter((v) => v.Time === '06-01 09:00')
    expect(hour09.every((v) => v.HitRate === null)).toBe(true)

    // Comparison chart ranks models descending by hit rate
    const comparisonValues = result.spec_hit_rate_comparison.data[0]
      .values as Array<{ Model: string; HitRate: number }>
    expect(comparisonValues.map((v) => [v.Model, v.HitRate])).toEqual([
      ['claude-sonnet', 80],
      ['gpt-5', 0],
    ])
  })

  test('omits redundant Overall series when only one model is present', () => {
    const base = new Date('2026-06-01T10:00:00').getTime() / 1000
    const result = processCacheHitRateChartData(
      [
        {
          created_at: base,
          model_name: 'claude-sonnet',
          count: 1,
          input_tokens: 1000,
          cache_read_tokens: 600,
        },
      ],
      'hour',
      (k) => k,
      'en'
    )

    const series = new Set(
      (
        result.spec_hit_rate_trend.data[0].values as Array<{ Series: string }>
      ).map((v) => v.Series)
    )
    expect([...series]).toEqual(['claude-sonnet'])
  })

  test.each(['en', 'zh', 'zhCN', 'zhTW', 'fr', 'ru', 'ja', 'vi', 'bad_locale'])(
    'formats dashboard hit rate safely for %s via toIntlLocale',
    (lang) => {
      const locale = toIntlLocale(lang)
      expect(() => formatDashboardHitRate(64.25, locale)).not.toThrow()
      expect(formatDashboardHitRate(0, locale)).toContain('0')
    }
  )
})
