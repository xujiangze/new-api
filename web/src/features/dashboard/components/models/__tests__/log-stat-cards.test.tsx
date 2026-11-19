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
import { render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import type { QuotaDataItem } from '@/features/dashboard/types'

import { LogStatCards } from '../log-stat-cards'

const mockGetUserQuotaDates = vi.fn()

vi.mock('@/features/dashboard/api', () => ({
  getUserQuotaDates: (...args: unknown[]) => mockGetUserQuotaDates(...args),
}))

const mockRows: QuotaDataItem[] = [
  {
    created_at: 1767312000,
    model_name: 'claude-sonnet',
    count: 10,
    quota: 500000,
    token_used: 12000,
    input_tokens: 10000,
    cache_read_tokens: 8000,
    cache_creation_tokens: 1000,
  },
  {
    created_at: 1767312000,
    model_name: 'gpt-5',
    count: 5,
    quota: 250000,
    token_used: 6000,
    input_tokens: 10000,
    cache_read_tokens: 0,
    cache_creation_tokens: 0,
  },
]

describe('LogStatCards', () => {
  beforeEach(() => {
    mockGetUserQuotaDates.mockReset()
    mockGetUserQuotaDates.mockResolvedValue({ success: true, data: mockRows })
  })

  test('renders Cache Hit Rate card and updates metrics client-side when selectedModels changes', async () => {
    const filters = { time_granularity: 'hour' as const }
    const { rerender } = render(
      <LogStatCards filters={filters} selectedModels={[]} />
    )

    const hitRateCard = await screen.findByRole('group', {
      name: 'Cache Hit Rate',
    })
    await waitFor(() => {
      expect(within(hitRateCard).getByText('40%')).toBeVisible()
    })
    expect(mockGetUserQuotaDates).toHaveBeenCalledTimes(1)

    // Filtering to claude-sonnet updates Cache Hit Rate to 80% without refetching.
    rerender(
      <LogStatCards filters={filters} selectedModels={['claude-sonnet']} />
    )
    expect(within(hitRateCard).getByText('80%')).toBeVisible()
    expect(mockGetUserQuotaDates).toHaveBeenCalledTimes(1)

    // Filtering to gpt-5 updates Cache Hit Rate to 0%.
    rerender(<LogStatCards filters={filters} selectedModels={['gpt-5']} />)
    expect(within(hitRateCard).getByText('0%')).toBeVisible()
    expect(mockGetUserQuotaDates).toHaveBeenCalledTimes(1)
  })
})
