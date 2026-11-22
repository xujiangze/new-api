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
import { PromotionAdminDialog } from '../components/promotion-admin-dialog'
import { PromotionReview } from '../components/promotion-review'
import type { PromotionProfile, PromotionReviewEvent } from '../types'

const profile: PromotionProfile = {
  user_id: 18,
  status: 'active',
  rate_bps: 1250,
  created_at_ms: 1,
  updated_at_ms: 1,
  first_activated_at_ms: 1,
  current_rule_id: 3,
}
let client: QueryClient

function renderAdmin(children: React.ReactNode) {
  return render(
    <QueryClientProvider client={client}>
      <PromotionAccess scope='admin'>{children}</PromotionAccess>
    </QueryClientProvider>
  )
}

function verificationRequirements(scope: string) {
  return {
    data: {
      success: true,
      data: {
        scope,
        methods: [{ method: '2fa', available: true }],
        oauth_providers: [],
        password_encryption_enabled: false,
      },
    },
  }
}

function mockVerification(scope: string) {
  vi.spyOn(api, 'get').mockResolvedValue(verificationRequirements(scope))
  return vi.spyOn(api, 'post').mockImplementation(async (url) => {
    if (url === '/api/verify') {
      return {
        data: {
          success: true,
          data: {
            proof_token: 'single-use-proof',
            scope,
            method: '2fa',
            expires_at: Math.floor(Date.now() / 1000) + 60,
          },
        },
      }
    }
    return { data: { success: true } }
  })
}

async function verify() {
  await userEvent.type(
    await screen.findByLabelText('Authenticator code or backup code'),
    '123456'
  )
  await userEvent.click(screen.getByRole('button', { name: 'Verify' }))
}

beforeEach(() => {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  useAuthStore.getState().auth.setUser({ id: 1, username: 'admin', role: 100 })
})
afterEach(() => {
  cleanup()
  client.clear()
  useAuthStore.getState().auth.reset()
  vi.restoreAllMocks()
})

it('invalid rates cannot proceed to confirmation and zero is accepted as a valid rate', async () => {
  const get = vi.spyOn(api, 'get')
  renderAdmin(<PromotionAdminDialog action='grant' onClose={vi.fn()} />)
  await userEvent.type(screen.getByLabelText('User ID'), '18')
  await userEvent.type(screen.getByLabelText('Commission rate (%)'), '100.01')
  await userEvent.type(screen.getByLabelText('Public reason'), 'Initial grant')
  await userEvent.click(screen.getByRole('button', { name: 'Review change' }))
  expect(
    await screen.findByText(
      'Enter a rate from 0 to 100 with at most two decimal places'
    )
  ).toBeVisible()
  expect(screen.getByLabelText('Commission rate (%)')).toHaveAttribute(
    'aria-invalid',
    'true'
  )
  expect(get).not.toHaveBeenCalled()
  await userEvent.clear(screen.getByLabelText('Commission rate (%)'))
  await userEvent.type(screen.getByLabelText('Commission rate (%)'), '0')
  await userEvent.click(screen.getByRole('button', { name: 'Review change' }))
  expect(await screen.findByRole('alertdialog')).toHaveTextContent('0%')
})

it('rate changes verify the exact target, basis points and reason before sending a nonreplayable PATCH', async () => {
  const post = mockVerification('admin.promotion_star.rate')
  const patch = vi
    .spyOn(api, 'patch')
    .mockResolvedValue({ data: { success: true } })
  const onClose = vi.fn()
  renderAdmin(
    <PromotionAdminDialog action='rate' profile={profile} onClose={onClose} />
  )
  await userEvent.clear(screen.getByLabelText('Commission rate (%)'))
  await userEvent.type(screen.getByLabelText('Commission rate (%)'), '12.34')
  await userEvent.type(
    screen.getByLabelText('Public reason'),
    'Prospective rate change'
  )
  await userEvent.click(screen.getByRole('button', { name: 'Review change' }))
  await userEvent.click(
    await screen.findByRole('button', { name: 'Verify and save' })
  )
  await screen.findByLabelText('Authenticator code or backup code')
  expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
  expect(patch).not.toHaveBeenCalled()
  await verify()
  await waitFor(() => expect(onClose).toHaveBeenCalled())
  expect(post).toHaveBeenCalledWith(
    '/api/verify',
    expect.objectContaining({
      scope: 'admin.promotion_star.rate',
      context: {
        user_id: 18,
        rate_bps: 1234,
        reason: 'Prospective rate change',
      },
    }),
    expect.anything()
  )
  expect(patch).toHaveBeenCalledWith(
    '/api/promotion-star/admin/profiles/18/rate',
    { rate_bps: 1234, reason: 'Prospective rate change' },
    expect.objectContaining({
      headers: { 'X-Security-Proof': 'single-use-proof' },
      singleUseAuthorization: true,
    })
  )
})

it('expired proof leaves the confirmation available for a new verification without revoking admin access', async () => {
  const post = mockVerification('admin.promotion_star.rate')
  const patch = vi
    .spyOn(api, 'patch')
    .mockRejectedValueOnce(
      new AxiosError('Expired', undefined, undefined, undefined, {
        status: 403,
        data: { code: 'SECURITY_PROOF_EXPIRED' },
      } as AxiosResponse)
    )
    .mockResolvedValue({ data: { success: true } })
  const onClose = vi.fn()
  renderAdmin(
    <PromotionAdminDialog action='rate' profile={profile} onClose={onClose} />
  )
  await userEvent.type(
    screen.getByLabelText('Public reason'),
    'Retry rate change'
  )
  await userEvent.click(screen.getByRole('button', { name: 'Review change' }))
  await userEvent.click(
    await screen.findByRole('button', { name: 'Verify and save' })
  )
  await verify()
  await waitFor(() => expect(patch).toHaveBeenCalledTimes(1))
  expect(await screen.findByRole('alertdialog')).toBeVisible()
  expect(
    screen.queryByText('Promotion Star access is unavailable')
  ).not.toBeInTheDocument()
  await waitFor(() =>
    expect(
      screen.getByRole('button', { name: 'Verify and save' })
    ).toBeEnabled()
  )
  await userEvent.click(screen.getByRole('button', { name: 'Verify and save' }))
  await verify()
  await waitFor(() => expect(onClose).toHaveBeenCalled())
  expect(post.mock.calls.filter(([url]) => url === '/api/verify')).toHaveLength(
    2
  )
  expect(patch).toHaveBeenCalledTimes(2)
})

it('cancelling security verification returns to confirmation without changing access', async () => {
  const post = mockVerification('admin.promotion_star.suspend')
  renderAdmin(
    <PromotionAdminDialog
      action='suspend'
      profile={profile}
      onClose={vi.fn()}
    />
  )
  await userEvent.type(
    screen.getByLabelText('Public reason'),
    'Paused partnership'
  )
  await userEvent.click(screen.getByRole('button', { name: 'Review change' }))
  await userEvent.click(
    await screen.findByRole('button', { name: 'Verify and save' })
  )
  await screen.findByLabelText('Authenticator code or backup code')
  await userEvent.keyboard('{Escape}')
  expect(await screen.findByRole('alertdialog')).toBeVisible()
  expect(post).not.toHaveBeenCalled()
})

it('a signed adjustment preserves exact decimals and binds private evidence and idempotency in the proof', async () => {
  const post = mockVerification('admin.promotion_star.adjust')
  renderAdmin(
    <PromotionAdminDialog action='adjust' profile={profile} onClose={vi.fn()} />
  )
  await userEvent.type(
    screen.getByLabelText('Signed commission amount (quota units)'),
    '-12500.0001'
  )
  await userEvent.type(
    screen.getByLabelText('Public reason'),
    'Duplicate commission correction'
  )
  await userEvent.type(
    screen.getByLabelText('Private evidence'),
    'Internal reconciliation note'
  )
  const key = (screen.getByLabelText('Idempotency key') as HTMLInputElement)
    .value
  await userEvent.click(screen.getByRole('button', { name: 'Review change' }))
  expect(await screen.findByRole('alertdialog')).toHaveTextContent(
    '-12500.0001'
  )
  await userEvent.click(screen.getByRole('button', { name: 'Verify and save' }))
  await verify()
  const payload = {
    commission_quota: '-12500.0001',
    reason: 'Duplicate commission correction',
    evidence: 'Internal reconciliation note',
    idempotency_key: key,
  }
  await waitFor(() =>
    expect(post).toHaveBeenCalledWith(
      '/api/promotion-star/admin/profiles/18/adjustments',
      payload,
      expect.objectContaining({ singleUseAuthorization: true })
    )
  )
  expect(post).toHaveBeenCalledWith(
    '/api/verify',
    expect.objectContaining({ context: { user_id: 18, ...payload } }),
    expect.anything()
  )
})

it('an actor change during verification prevents the old administrator operation', async () => {
  const post = mockVerification('admin.promotion_star.suspend')
  renderAdmin(
    <PromotionAdminDialog
      action='suspend'
      profile={profile}
      onClose={vi.fn()}
    />
  )
  await userEvent.type(screen.getByLabelText('Public reason'), 'Pause')
  await userEvent.click(screen.getByRole('button', { name: 'Review change' }))
  await userEvent.click(
    await screen.findByRole('button', { name: 'Verify and save' })
  )
  await screen.findByLabelText('Authenticator code or backup code')
  act(() => {
    useAuthStore.getState().auth.setUser({
      id: 4,
      username: 'new-user',
      role: 1,
      promotion_star_enabled: true,
    })
  })
  expect(
    await screen.findByText('Promotion Star access is unavailable')
  ).toBeVisible()
  expect(
    screen.queryByLabelText('Authenticator code or backup code')
  ).not.toBeInTheDocument()
  expect(post).not.toHaveBeenCalled()
})

it('unreliable review amounts and provisional settlements cannot be confirmed', async () => {
  const entries: PromotionReviewEvent[] = [
    {
      id: 1,
      event_key: 'private-event-a',
      user_id: 18,
      invitee_user_id: 21,
      source: 'wallet_usage',
      state: 'review',
      quota: '500000',
      settled_at_ms: 1795305600000,
      updated_at_ms: 1795305600100,
      review_reason: 'Unreliable amount',
      can_confirm: false,
    },
    {
      id: 2,
      event_key: 'private-event-b',
      user_id: 18,
      invitee_user_id: 22,
      source: 'wallet_usage',
      state: 'provisional',
      quota: '0',
      settled_at_ms: 0,
      updated_at_ms: 1795305600100,
      review_reason: 'Awaiting provider',
    },
  ]
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: { items: entries, total: 2, p: 1, page_size: 20 },
    },
  })
  renderAdmin(<PromotionReview />)
  const row = await screen.findByRole('row', { name: /Unreliable amount/ })
  expect(
    within(row).getByRole('button', { name: 'Confirm reconciliation' })
  ).toBeDisabled()
  expect(
    within(row).getByRole('button', { name: 'Exclude from commission' })
  ).toBeEnabled()
  const provisional = screen.getByRole('row', { name: /Awaiting provider/ })
  expect(within(provisional).queryByRole('button')).not.toBeInTheDocument()
  expect(screen.queryByText('private-event-a')).not.toBeInTheDocument()
})

it('review confirmation binds the immutable economic snapshot in proof and mutation payload', async () => {
  const scope = 'admin.promotion_star.review'
  const post = mockVerification(scope)
  vi.mocked(api.get).mockImplementation(async (url) => {
    if (url === '/api/verify/methods') return verificationRequirements(scope)
    return {
      data: {
        success: true,
        data: {
          items: [
            {
              id: 5,
              event_key: 'private-event-key',
              user_id: 18,
              invitee_user_id: 21,
              source: 'wallet_usage',
              state: 'review',
              quota: '500000.25',
              settled_at_ms: 1795305600000,
              updated_at_ms: 1795305600100,
              review_reason: 'Ready for review',
              can_confirm: true,
            },
          ],
          total: 1,
          p: 1,
          page_size: 20,
        },
      },
    }
  })
  renderAdmin(<PromotionReview />)
  await userEvent.click(
    await screen.findByRole('button', { name: 'Confirm reconciliation' })
  )
  await userEvent.type(
    screen.getByLabelText('Review reason'),
    'Verified settlement'
  )
  await userEvent.click(screen.getByRole('button', { name: 'Verify and save' }))
  await verify()
  const payload = {
    state: 'pending',
    reason: 'Verified settlement',
    expected_updated_at_ms: 1795305600100,
    expected_quota: '500000.25',
    expected_settled_at_ms: 1795305600000,
  }
  await waitFor(() =>
    expect(post).toHaveBeenCalledWith(
      '/api/promotion-star/admin/review-events/5/resolve',
      payload,
      expect.objectContaining({ singleUseAuthorization: true })
    )
  )
  expect(post).toHaveBeenCalledWith(
    '/api/verify',
    expect.objectContaining({
      scope,
      context: { event_key: 'private-event-key', ...payload },
    }),
    expect.anything()
  )
})
