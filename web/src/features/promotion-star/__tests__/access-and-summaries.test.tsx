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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  act,
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AxiosError, type AxiosResponse } from 'axios'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { PromotionAccess } from '../components/promotion-access'
import {
  PromotionInvitees,
  PromotionLedger,
} from '../components/promotion-records'
import { PromotionSummary } from '../components/promotion-summary'
import { canViewPromotion, requirePromotionAccess } from '../lib/access'
import type { PromotionOverview } from '../types'

const filters = { start_date: '2026-11-01', end_date: '2026-11-22' }
const totals = {
  wallet_consumption: '1500000',
  wallet_refund: '500000',
  wallet_net: '1000000',
  commission: '100000',
  adjustment: '-25000',
  commission_net: '75000',
}
const profile = {
  user_id: 7,
  status: 'active' as const,
  rate_bps: 1250,
  created_at_ms: 1,
  updated_at_ms: 1,
  first_activated_at_ms: 1,
  current_rule_id: 3,
}
let client: QueryClient

function renderFeature(
  children: React.ReactNode,
  scope: 'self' | 'admin' = 'self'
) {
  return render(
    <QueryClientProvider client={client}>
      <PromotionAccess scope={scope}>{children}</PromotionAccess>
    </QueryClientProvider>
  )
}

beforeEach(() => {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  useAuthStore.getState().auth.setUser({
    id: 7,
    username: 'promoter',
    role: 1,
    promotion_star_enabled: true,
  })
})
afterEach(() => {
  cleanup()
  client.clear()
  useAuthStore.getState().auth.reset()
  vi.restoreAllMocks()
})

it('a personal promotion capability never grants admin access and missing capabilities fail closed', () => {
  const promoter = useAuthStore.getState().auth.user
  expect(canViewPromotion(promoter, 'self')).toBe(true)
  expect(canViewPromotion(promoter, 'admin')).toBe(false)
  expect(() => requirePromotionAccess('admin')).toThrow()
  expect(
    canViewPromotion({ id: 8, username: 'admin', role: 100 }, 'self')
  ).toBe(false)
  useAuthStore.getState().auth.setUser({ id: 8, username: 'legacy', role: 1 })
  const get = vi.spyOn(api, 'get')
  renderFeature(<PromotionInvitees filters={filters} />)
  expect(screen.getByText('Promotion Star access is unavailable')).toBeVisible()
  expect(get).not.toHaveBeenCalled()
  expect(() => requirePromotionAccess('self')).toThrow()
})

it('invitee rows show only safe identity and totals even if the response carries private fields', async () => {
  const get = vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        items: [
          {
            user_id: 18,
            display_name: 'A long but safe direct invitee name',
            ...totals,
            email: 'secret@example.com',
            ip: '10.1.2.3',
            key: 'secret-api-key',
            request_id: 'hidden-request',
            model: 'hidden-model',
            channel: 'hidden-channel',
          },
        ],
        total: 1,
        p: 1,
        page_size: 20,
      },
    },
  })
  renderFeature(<PromotionInvitees filters={filters} />)
  expect(
    await screen.findByText('A long but safe direct invitee name')
  ).toBeVisible()
  expect(screen.getByRole('columnheader', { name: 'User ID' })).toBeVisible()
  expect(
    screen.getByRole('columnheader', { name: 'Net wallet consumption' })
  ).toBeVisible()
  for (const secret of [
    'secret@example.com',
    '10.1.2.3',
    'secret-api-key',
    'hidden-request',
    'hidden-model',
    'hidden-channel',
  ]) {
    expect(screen.queryByText(secret)).not.toBeInTheDocument()
  }
  expect(get).toHaveBeenCalledWith(
    '/api/promotion-star/invitees',
    expect.objectContaining({
      params: { ...filters, p: 1, page_size: 20 },
      disableDuplicate: true,
      signal: expect.any(AbortSignal),
    })
  )
})

it('revoking capability removes visible rows and cached data before another response can render', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        items: [{ user_id: 18, display_name: 'Revoked invitee', ...totals }],
        total: 1,
        p: 1,
        page_size: 20,
      },
    },
  })
  renderFeature(<PromotionInvitees filters={filters} />)
  await screen.findByText('Revoked invitee')
  act(() => {
    useAuthStore.getState().auth.setUser({
      id: 7,
      username: 'promoter',
      role: 1,
      promotion_star_enabled: false,
    })
  })
  expect(screen.queryByText('Revoked invitee')).not.toBeInTheDocument()
  expect(screen.getByText('Promotion Star access is unavailable')).toBeVisible()
  await waitFor(() =>
    expect(
      client
        .getQueryCache()
        .findAll({ queryKey: ['promotion-star', 7, 'self'] })
    ).toHaveLength(0)
  )
})

it('switching actor cancels the former request and never displays its late result', async () => {
  let resolveFirst!: (value: { data: object }) => void
  const first = new Promise<{ data: object }>((resolve) => {
    resolveFirst = resolve
  })
  const get = vi
    .spyOn(api, 'get')
    .mockReturnValueOnce(first)
    .mockResolvedValue({
      data: {
        success: true,
        data: {
          items: [
            { user_id: 19, display_name: 'New actor invitee', ...totals },
          ],
          total: 1,
          p: 1,
          page_size: 20,
        },
      },
    })
  renderFeature(<PromotionInvitees filters={filters} />)
  await waitFor(() => expect(get).toHaveBeenCalledTimes(1))
  const signal = get.mock.calls[0][1]?.signal
  act(() => {
    useAuthStore.getState().auth.setUser({
      id: 9,
      username: 'new-promoter',
      role: 1,
      promotion_star_enabled: true,
    })
  })
  await screen.findByText('New actor invitee')
  await act(async () =>
    resolveFirst({
      data: {
        success: true,
        data: {
          items: [
            { user_id: 18, display_name: 'Old private invitee', ...totals },
          ],
          total: 1,
        },
      },
    })
  )
  expect(signal?.aborted).toBe(true)
  expect(screen.queryByText('Old private invitee')).not.toBeInTheDocument()
  expect(
    client.getQueryCache().findAll({ queryKey: ['promotion-star', 7] })
  ).toHaveLength(0)
})

it('a revoked self API response clears cached summaries and removes the capability', async () => {
  vi.spyOn(api, 'get').mockRejectedValue(
    new AxiosError('forbidden', undefined, undefined, undefined, {
      status: 403,
      data: { code: 'PROMOTION_STAR_FORBIDDEN' },
    } as AxiosResponse)
  )
  renderFeature(<PromotionInvitees filters={filters} />)
  expect(
    await screen.findByText('Promotion Star access is unavailable')
  ).toBeVisible()
  expect(useAuthStore.getState().auth.user?.promotion_star_enabled).toBe(false)
  expect(
    client.getQueryCache().findAll({ queryKey: ['promotion-star', 7] })
  ).toHaveLength(0)
})

it('failed business responses show an error rather than empty success and retry the same scope', async () => {
  vi.spyOn(api, 'get')
    .mockResolvedValueOnce({ data: { success: false, message: 'Unavailable' } })
    .mockResolvedValue({
      data: {
        success: true,
        data: { items: [], total: 0, p: 1, page_size: 20 },
      },
    })
  renderFeature(<PromotionLedger filters={filters} />)
  await screen.findByText('Failed to load Promotion Star data')
  expect(screen.queryByText('No records')).not.toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: 'Retry' }))
  expect(await screen.findByText('No records')).toBeVisible()
})

it('ledger keeps the summary grain and does not expose individual request drilldowns', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        items: [
          {
            id: 1,
            day: '2026-11-22',
            user_id: 7,
            invitee_user_id: 18,
            source: 'wallet_usage',
            kind: 'refund',
            rule_id: 3,
            rate_bps: 1250,
            basis_quota: '-500000',
            commission_quota: '-62500',
            public_reason: 'Corrected duplicate charge',
            request_id: 'private-request',
          },
        ],
        total: 1,
        p: 1,
        page_size: 20,
      },
    },
  })
  renderFeature(<PromotionLedger filters={filters} />)
  const row = await screen.findByRole('row', {
    name: /Corrected duplicate charge/,
  })
  expect(within(row).getByText('2026-11-22')).toBeVisible()
  expect(within(row).getByText('12.5%')).toBeVisible()
  expect(screen.queryByText('private-request')).not.toBeInTheDocument()
  expect(within(row).queryByRole('button')).not.toBeInTheDocument()
})

it('admin summaries use the dated overview endpoint rather than the undated profile endpoint', async () => {
  useAuthStore.getState().auth.setUser({ id: 1, username: 'admin', role: 100 })
  const data: PromotionOverview = {
    profile,
    totals,
    daily: [],
    pending_review: false,
    updated_through: null,
  }
  const get = vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url !== '/api/promotion-star/admin/profiles/7/overview') {
      return { data: { success: false, message: 'Invalid parameters' } }
    }
    return { data: { success: true, data } }
  })
  renderFeature(<PromotionSummary filters={filters} targetId={7} />, 'admin')
  expect(await screen.findByText('No pending review')).toBeVisible()
  expect(get).toHaveBeenCalledWith(
    '/api/promotion-star/admin/profiles/7/overview',
    expect.objectContaining({ params: filters })
  )
})

it('overview displays server totals, last-update semantics, and pending processing without treating it as completed', async () => {
  const data: PromotionOverview = {
    profile,
    totals,
    daily: [],
    pending_review: true,
    updated_through: 1795305600000,
  }
  vi.spyOn(api, 'get').mockResolvedValue({ data: { success: true, data } })
  renderFeature(<PromotionSummary filters={filters} />)
  expect(await screen.findByText('Processing or pending review')).toBeVisible()
  expect(screen.getByText(/Last updated/)).toBeVisible()
  expect(screen.getByText('Net consumption commission')).toBeVisible()
  expect(screen.getByText('$0.15')).toBeVisible()
  expect(
    await screen.findByText('No promotion activity in this period')
  ).toBeVisible()
})
