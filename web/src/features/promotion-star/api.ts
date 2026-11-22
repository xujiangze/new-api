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
import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

import type {
  PromotionCommand,
  PromotionFilters,
  PromotionInvitee,
  PromotionLedgerRow,
  PromotionOverview,
  PromotionPage,
  PromotionPagination,
  PromotionProfile,
  PromotionReviewEvent,
  PromotionRule,
  PromotionScope,
} from './types'

const ROOT = '/api/promotion-star'

interface Response<T> {
  success: boolean
  data: T
  message?: string
}

/** All reads stay inside the promotion capability boundary. */
async function readPromotion<T>(
  path: string,
  params: object,
  signal: AbortSignal
): Promise<T> {
  const response = await api.get<Response<T>>(`${ROOT}${path}`, {
    params,
    signal,
    // React Query owns deduplication and aborts within actor + scope.
    disableDuplicate: true,
  })
  return requireServerSuccess(response.data).data
}

export function getPromotionOverview(
  scope: PromotionScope,
  targetId: number | undefined,
  filters: PromotionFilters,
  signal: AbortSignal
): Promise<PromotionOverview> {
  const path =
    scope === 'self' ? '/overview' : `/admin/profiles/${targetId}/overview`
  return readPromotion(path, filters, signal)
}

export function getPromotionInvitees(
  scope: PromotionScope,
  targetId: number | undefined,
  filters: PromotionFilters & PromotionPagination,
  signal: AbortSignal
): Promise<PromotionPage<PromotionInvitee>> {
  const path =
    scope === 'self' ? '/invitees' : `/admin/profiles/${targetId}/invitees`
  return readPromotion(path, filters, signal)
}

export function getPromotionLedger(
  scope: PromotionScope,
  targetId: number | undefined,
  filters: PromotionFilters & PromotionPagination,
  signal: AbortSignal
): Promise<PromotionPage<PromotionLedgerRow>> {
  const path =
    scope === 'self' ? '/ledger' : `/admin/profiles/${targetId}/ledger`
  return readPromotion(path, filters, signal)
}

export function getPromotionProfiles(
  pagination: PromotionPagination,
  signal: AbortSignal
): Promise<PromotionPage<PromotionProfile>> {
  return readPromotion('/admin/profiles', pagination, signal)
}

export function getPromotionRules(
  userId: number,
  pagination: PromotionPagination,
  signal: AbortSignal
): Promise<PromotionPage<PromotionRule>> {
  return readPromotion(`/admin/profiles/${userId}/rules`, pagination, signal)
}

export function getPromotionReviewEvents(
  pagination: PromotionPagination,
  signal: AbortSignal
): Promise<PromotionPage<PromotionReviewEvent>> {
  return readPromotion('/admin/review-events', pagination, signal)
}

export async function executePromotionCommand(
  command: PromotionCommand,
  proofToken: string,
  signal: AbortSignal
): Promise<void> {
  const action = command.action === 'adjust' ? 'adjustments' : command.action
  const path =
    command.action === 'review'
      ? `${ROOT}/admin/review-events/${command.event_id}/resolve`
      : `${ROOT}/admin/profiles/${command.user_id}/${action}`
  const config = {
    headers: { 'X-Security-Proof': proofToken },
    singleUseAuthorization: true,
    signal,
  }
  const response =
    command.action === 'rate'
      ? await api.patch<Response<unknown>>(path, command.payload, config)
      : await api.post<Response<unknown>>(path, command.payload, config)
  requireServerSuccess(response.data)
}
