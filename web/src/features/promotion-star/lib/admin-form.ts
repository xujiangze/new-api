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
import { z } from 'zod'

import type { PromotionAction, PromotionCommand } from '../types'

export interface PromotionFormValues {
  user_id: string
  rate: string
  commission_quota: string
  reason: string
  evidence: string
  idempotency_key: string
}

/** Decimal percentage input is converted to basis points using integer digits. */
export function promotionRateBps(value: string): number | null {
  if (!/^(?:0|[1-9]\d{0,2})(?:\.\d{1,2})?$/.test(value)) return null
  const [whole, fraction = ''] = value.split('.')
  const bps = Number(whole) * 100 + Number(fraction.padEnd(2, '0'))
  return bps <= 10000 ? bps : null
}

export function promotionFormSchema(action: PromotionAction) {
  return z
    .object({
      user_id: z
        .string()
        .regex(/^[1-9]\d*$/, 'Enter a valid user ID')
        .refine(
          (value) => Number.isSafeInteger(Number(value)),
          'Enter a valid user ID'
        ),
      rate: z.string(),
      commission_quota: z.string(),
      reason: z
        .string()
        .trim()
        .min(1, 'A public reason is required')
        .max(500, 'Use at most 500 characters'),
      evidence: z.string().trim().max(2000, 'Use at most 2000 characters'),
      idempotency_key: z.string(),
    })
    .superRefine((values, ctx) => {
      if (
        (action === 'grant' || action === 'rate') &&
        promotionRateBps(values.rate) === null
      ) {
        ctx.addIssue({
          code: 'custom',
          path: ['rate'],
          message: 'Enter a rate from 0 to 100 with at most two decimal places',
        })
      }
      if (action === 'adjust') {
        if (
          !/^-?\d+(?:\.\d+)?$/.test(values.commission_quota) ||
          !/[1-9]/.test(values.commission_quota) ||
          values.commission_quota.length > 128
        ) {
          ctx.addIssue({
            code: 'custom',
            path: ['commission_quota'],
            message: 'Enter a nonzero signed quota amount',
          })
        }
        if (!values.evidence) {
          ctx.addIssue({
            code: 'custom',
            path: ['evidence'],
            message: 'Private evidence is required',
          })
        }
        if (
          !values.idempotency_key.trim() ||
          values.idempotency_key.length > 128
        ) {
          ctx.addIssue({
            code: 'custom',
            path: ['idempotency_key'],
            message: 'Use an idempotency key of 1 to 128 characters',
          })
        }
      }
    })
}

export function promotionCommand(
  action: PromotionAction,
  values: PromotionFormValues
): PromotionCommand {
  const user_id = Number(values.user_id)
  const reason = values.reason
  if (action === 'adjust') {
    return {
      action,
      user_id,
      payload: {
        commission_quota: values.commission_quota,
        reason,
        evidence: values.evidence,
        idempotency_key: values.idempotency_key,
      },
    }
  }
  if (action === 'suspend') return { action, user_id, payload: { reason } }
  const rate_bps = promotionRateBps(values.rate)
  if (rate_bps === null) throw new Error('Invalid commission rate')
  return { action, user_id, payload: { rate_bps, reason } }
}
