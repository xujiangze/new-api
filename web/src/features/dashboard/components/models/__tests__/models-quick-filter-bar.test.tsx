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
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, test, vi } from 'vitest'

import type { QuotaDataItem } from '@/features/dashboard/types'

import { ModelsQuickFilterBar } from '../models-quick-filter-bar'

const sampleData: QuotaDataItem[] = [
  {
    created_at: 1767312000,
    model_name: 'claude-sonnet',
    count: 5,
    input_tokens: 10000,
    cache_read_tokens: 8000,
  },
  {
    created_at: 1767312000,
    model_name: 'gpt-5',
    count: 3,
    input_tokens: 4000,
    cache_read_tokens: 1000,
  },
]

describe('ModelsQuickFilterBar', () => {
  test('hides username input for non-admin users and allows selecting models', async () => {
    const user = userEvent.setup()
    const onSelectedModelsChange = vi.fn()

    render(
      <ModelsQuickFilterBar
        rawData={sampleData}
        selectedModels={[]}
        onSelectedModelsChange={onSelectedModelsChange}
        isAdmin={false}
        username=''
        onUsernameSubmit={vi.fn()}
        onClearQuickFilters={vi.fn()}
      />
    )

    expect(
      screen.queryByPlaceholderText('Filter by username')
    ).not.toBeInTheDocument()

    const modelInput = screen.getByPlaceholderText('Filter by models')
    await user.click(modelInput)
    const option = await screen.findByText('claude-sonnet')
    await user.click(option)

    expect(onSelectedModelsChange).toHaveBeenCalledWith(['claude-sonnet'])
  })

  test('lets administrators submit, clear, and reset username and model filters', async () => {
    const user = userEvent.setup()
    const onUsernameSubmit = vi.fn()
    const onClearQuickFilters = vi.fn()

    render(
      <ModelsQuickFilterBar
        rawData={sampleData}
        selectedModels={['claude-sonnet']}
        onSelectedModelsChange={vi.fn()}
        isAdmin
        username='alice'
        onUsernameSubmit={onUsernameSubmit}
        onClearQuickFilters={onClearQuickFilters}
      />
    )

    const usernameInput = screen.getByPlaceholderText('Filter by username')
    expect(usernameInput).toHaveValue('alice')

    await user.clear(usernameInput)
    await user.type(usernameInput, '  bob  {Enter}')
    expect(onUsernameSubmit).toHaveBeenCalledWith('bob')

    await user.click(
      screen.getByRole('button', { name: 'Clear username filter' })
    )
    expect(onUsernameSubmit).toHaveBeenCalledWith('')

    await user.click(screen.getByRole('button', { name: 'Clear filters' }))
    expect(onClearQuickFilters).toHaveBeenCalledTimes(1)
  })
})
