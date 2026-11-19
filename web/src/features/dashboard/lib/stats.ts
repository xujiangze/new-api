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
import type { QuotaDataItem } from '@/features/dashboard/types'

/**
 * Safe division: handles NaN and Infinity cases
 */
export function safeDivide(
  value: number,
  divisor: number,
  precision: number = 3
): number {
  const result = value / divisor
  if (Number.isNaN(result) || !Number.isFinite(result)) return 0
  const factor = Math.pow(10, precision)
  return Math.round(result * factor) / factor
}

function toNonNegativeNumber(value: unknown): number {
  const num = Number(value)
  return Number.isFinite(num) && num > 0 ? num : 0
}

/**
 * Extract cache hit rate numerator and denominator from a QuotaData row.
 * Rows written before `input_tokens` existed have `input_tokens === 0` and do
 * not contribute `cache_read_tokens` to `hitReadTokens`, preventing historical
 * data from inflating post-upgrade hit rates across upgrade boundaries.
 */
export function getRowCacheHitMetrics(item: QuotaDataItem): {
  inputTokens: number
  cacheReadTokens: number
  cacheCreationTokens: number
  hitReadTokens: number
} {
  const inputTokens = toNonNegativeNumber(item.input_tokens)
  const cacheReadTokens = toNonNegativeNumber(item.cache_read_tokens)
  const cacheCreationTokens = toNonNegativeNumber(item.cache_creation_tokens)

  let hitReadTokens = 0
  if (inputTokens > 0) {
    const rawHitRead =
      item.cache_hit_read_tokens != null
        ? toNonNegativeNumber(item.cache_hit_read_tokens)
        : cacheReadTokens
    hitReadTokens = Math.min(rawHitRead, inputTokens)
  }

  return {
    inputTokens,
    cacheReadTokens,
    cacheCreationTokens,
    hitReadTokens,
  }
}

export function computeCacheHitRatePercent(
  hitReadTokens: number,
  inputTokens: number
): number {
  if (inputTokens <= 0 || hitReadTokens <= 0) return 0
  return Math.min(100, Math.max(0, (hitReadTokens / inputTokens) * 100))
}

/**
 * Calculate aggregated statistics from quota data
 */
export function calculateDashboardStats(data: QuotaDataItem[]) {
  const totals = data.reduce(
    (acc, item) => {
      const cache = getRowCacheHitMetrics(item)
      return {
        totalQuota: acc.totalQuota + (Number(item.quota) || 0),
        totalCount: acc.totalCount + (Number(item.count) || 0),
        totalTokens: acc.totalTokens + (Number(item.token_used) || 0),
        totalInputTokens: acc.totalInputTokens + cache.inputTokens,
        totalCacheReadTokens: acc.totalCacheReadTokens + cache.cacheReadTokens,
        totalCacheCreationTokens:
          acc.totalCacheCreationTokens + cache.cacheCreationTokens,
        totalCacheHitReadTokens:
          acc.totalCacheHitReadTokens + cache.hitReadTokens,
      }
    },
    {
      totalQuota: 0,
      totalCount: 0,
      totalTokens: 0,
      totalInputTokens: 0,
      totalCacheReadTokens: 0,
      totalCacheCreationTokens: 0,
      totalCacheHitReadTokens: 0,
    }
  )

  return {
    ...totals,
    cacheHitRate: computeCacheHitRatePercent(
      totals.totalCacheHitReadTokens,
      totals.totalInputTokens
    ),
  }
}
