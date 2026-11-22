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
export type PromotionScope = 'self' | 'admin'
export type PromotionSection = 'overview' | 'invitees' | 'ledger'
export type PromotionAction = 'grant' | 'rate' | 'suspend' | 'adjust'

export interface PromotionProfile {
  user_id: number
  display_name?: string
  status: 'active' | 'suspended'
  rate_bps: number
  created_at_ms: number
  updated_at_ms: number
  first_activated_at_ms: number
  current_rule_id: number
}

/** Exact quota decimals, never used for client-side accounting. */
export interface PromotionTotals {
  wallet_consumption: string
  wallet_refund: string
  wallet_net: string
  commission: string
  adjustment: string
  commission_net: string
}

export interface PromotionOverview {
  profile: PromotionProfile
  totals: PromotionTotals
  daily: Array<PromotionTotals & { date: string }>
  updated_through: number | null
  pending_review: boolean
}

export interface PromotionInvitee extends PromotionTotals {
  user_id: number
  display_name: string
}

/** Summary grain only: date × invitee × source × rule × kind. */
export interface PromotionLedgerRow {
  id: number
  day: string
  user_id: number
  invitee_user_id: number
  invitee_name?: string
  source: string
  kind: string
  rule_id: number
  rate_bps: number
  basis_quota: string
  commission_quota: string
  public_reason: string
}

export interface PromotionRule {
  id: number
  user_id: number
  rate_bps: number
  effective_at_ms: number
  status: 'active' | 'suspended'
  reason: string
}

export interface PromotionReviewEvent {
  id: number
  event_key: string
  user_id: number
  invitee_user_id: number
  source: string
  state: string
  quota: string
  settled_at_ms: number
  updated_at_ms: number
  can_confirm?: boolean
  review_reason: string
}

export interface PromotionPage<T> {
  items: T[]
  total: number
  p: number
  page_size: number
}

export interface PromotionFilters {
  start_date: string
  end_date: string
}

export interface PromotionPagination {
  p: number
  page_size: number
}

export type PromotionCommand =
  | {
      action: 'review'
      user_id: number
      event_id: number
      event_key: string
      payload: {
        state: 'pending' | 'ignored'
        reason: string
        expected_updated_at_ms: number
        expected_quota: string
        expected_settled_at_ms: number
      }
    }
  | {
      action: 'grant' | 'rate'
      user_id: number
      payload: { rate_bps: number; reason: string }
    }
  | { action: 'suspend'; user_id: number; payload: { reason: string } }
  | {
      action: 'adjust'
      user_id: number
      payload: {
        commission_quota: string
        reason: string
        evidence: string
        idempotency_key: string
      }
    }
