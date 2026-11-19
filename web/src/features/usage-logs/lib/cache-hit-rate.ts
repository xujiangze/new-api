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
import type { UsageLog } from '../data/schema'
import type { LogOtherData } from '../types'

export interface UsageLogCacheMetrics {
  cacheReadTokens: number
  cacheWriteTokens: number
  totalInputTokens: number
  /** Cache hit rate percentage in [0, 100], or null when totalInputTokens <= 0. */
  hitRate: number | null
}

function toNonNegativeNumber(value: unknown): number {
  const num = Number(value)
  return Number.isFinite(num) && num > 0 ? num : 0
}

/**
 * Compute normalized cache tokens and token-level cache hit rate for a usage log.
 *
 * Denominator rules match backend billing normalization:
 * - Explicit `input_tokens_total` / `billing_tokens.len` when present (> 0)
 * - Anthropic/Claude (`usage_semantic === 'anthropic'`, or legacy `claude === true`):
 *   `prompt_tokens + cache_read + cache_write`
 * - OpenAI / Gemini / other providers: `prompt_tokens` (already includes cached tokens)
 */
export function getUsageLogCacheMetrics(
  log: Pick<UsageLog, 'prompt_tokens'>,
  other: LogOtherData | null | undefined
): UsageLogCacheMetrics {
  const promptTokens = toNonNegativeNumber(log.prompt_tokens)
  const cacheReadTokens = toNonNegativeNumber(other?.cache_tokens)

  const explicitCacheWrite = toNonNegativeNumber(other?.cache_write_tokens)
  let cacheWriteTokens = explicitCacheWrite
  if (cacheWriteTokens === 0) {
    const cacheWrite5m = toNonNegativeNumber(other?.cache_creation_tokens_5m)
    const cacheWrite1h = toNonNegativeNumber(other?.cache_creation_tokens_1h)
    const cacheCreation = toNonNegativeNumber(other?.cache_creation_tokens)
    const splitWrite = cacheWrite5m + cacheWrite1h
    cacheWriteTokens =
      splitWrite > 0 ? Math.max(cacheCreation, splitWrite) : cacheCreation
  }

  const explicitTotalInput = toNonNegativeNumber(other?.input_tokens_total)
  const tieredInputLen = toNonNegativeNumber(other?.billing_tokens?.len)
  const isAnthropicSemantic =
    other?.usage_semantic === 'anthropic' ||
    (other?.usage_semantic == null && other?.claude === true)

  let totalInputTokens: number
  if (explicitTotalInput > 0) {
    totalInputTokens = explicitTotalInput
  } else if (tieredInputLen > 0) {
    totalInputTokens = tieredInputLen
  } else if (isAnthropicSemantic) {
    totalInputTokens = promptTokens + cacheReadTokens + cacheWriteTokens
  } else {
    totalInputTokens = promptTokens
  }

  if (totalInputTokens <= 0) {
    return {
      cacheReadTokens,
      cacheWriteTokens,
      totalInputTokens: 0,
      hitRate: null,
    }
  }

  const rawRate = (cacheReadTokens / totalInputTokens) * 100
  const hitRate = Math.min(100, Math.max(0, rawRate))

  return {
    cacheReadTokens,
    cacheWriteTokens,
    totalInputTokens,
    hitRate,
  }
}

export function formatCacheHitRate(
  rate: number | null | undefined,
  locale?: Intl.LocalesArgument
): string {
  if (rate == null || !Number.isFinite(rate)) return '-'
  const clamped = Math.min(100, Math.max(0, rate))
  return Intl.NumberFormat(locale, {
    style: 'percent',
    maximumFractionDigits: 1,
  }).format(clamped / 100)
}

export function formatCacheHitRateCsv(rate: number | null | undefined): string {
  if (rate == null || !Number.isFinite(rate)) return ''
  const clamped = Math.min(100, Math.max(0, rate))
  return `${Number(clamped.toFixed(1))}%`
}
