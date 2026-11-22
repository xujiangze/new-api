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
import dayjs from '@/lib/dayjs'

import type { PromotionFilters } from '../types'

/** Convert for display only; oversized values retain their exact quota text. */
export function promotionQuotaNumber(value: string): number | null {
  if (!/^-?\d+(?:\.\d+)?$/.test(value)) return null
  const amount = Number(value)
  const significantDigits = value
    .replace(/^-?0*/, '')
    .replace('.', '')
    .replace(/0+$/, '').length
  if (significantDigits > 15) return null
  if (!Number.isFinite(amount) || Math.abs(amount) > Number.MAX_SAFE_INTEGER) {
    return null
  }
  const integer = value.split('.')[0]
  if (BigInt(integer) !== BigInt(Math.trunc(amount))) return null
  return amount
}

export function initialPromotionDates(): PromotionFilters {
  const today = new Intl.DateTimeFormat('en-CA', {
    timeZone: 'Asia/Shanghai',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).format(new Date())
  return {
    start_date: dayjs(today).subtract(29, 'day').format('YYYY-MM-DD'),
    end_date: today,
  }
}

export function promotionTime(
  value: number | null,
  locale: string | undefined
): string {
  if (!value || !Number.isFinite(value)) return '—'
  return new Intl.DateTimeFormat(locale, {
    timeZone: 'Asia/Shanghai',
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(new Date(value))
}
