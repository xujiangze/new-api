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
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import { STATUS_QUERY_KEY, statusQueryOptions } from '@/lib/status-query'

import { AnnouncementsSection } from '../announcements-section'

const entries = [
  {
    id: 1,
    content: '**Popup update**',
    publishDate: '2026-11-23T09:00:00Z',
    type: 'warning',
    extra: 'Read the notes',
    popupOnLogin: true,
    custom: 'keep',
  },
  {
    id: 2,
    content: 'Legacy update',
    publishDate: '2026-11-22T09:00:00Z',
    type: 'default',
    extra: '',
  },
  {
    id: 3,
    content: 'List update',
    publishDate: '2026-11-21T09:00:00Z',
    type: 'default',
    extra: '',
    popupOnLogin: false,
  },
]
const clients: QueryClient[] = []
const animationsDescriptor = Object.getOwnPropertyDescriptor(
  Element.prototype,
  'getAnimations'
)

function renderSettings(data: unknown[] = entries) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  clients.push(client)
  render(
    <QueryClientProvider client={client}>
      <AnnouncementsSection enabled data={JSON.stringify(data)} />
    </QueryClientProvider>
  )
  return client
}

function row(content: string) {
  return screen.getByRole('row', {
    name: new RegExp(content.replaceAll('*', '\\*')),
  })
}

async function selectFilter(label: string) {
  const user = userEvent.setup()
  await user.click(
    screen.getByRole('combobox', { name: 'Announcement display filter' })
  )
  await user.click(await screen.findByRole('option', { name: label }))
}

beforeEach(() => {
  window.localStorage.clear()
  Object.defineProperty(Element.prototype, 'getAnimations', {
    configurable: true,
    value: () => [],
  })
  vi.spyOn(api, 'put').mockResolvedValue({ data: { success: true } })
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: { announcements_enabled: true, announcements: entries },
    },
  })
})

afterEach(() => {
  cleanup()
  if (animationsDescriptor) {
    Object.defineProperty(
      Element.prototype,
      'getAnimations',
      animationsDescriptor
    )
  } else {
    Reflect.deleteProperty(Element.prototype, 'getAnimations')
  }
  clients.splice(0).forEach((client) => client.clear())
  window.localStorage.clear()
})

test('new announcements default to login popups, while editing legacy entries defaults off and preserves existing fields', async () => {
  const user = userEvent.setup()
  const client = renderSettings()
  client.setQueryData(STATUS_QUERY_KEY, {
    announcements_enabled: true,
    announcements: [],
  })
  await user.click(screen.getByRole('button', { name: 'Add Announcement' }))
  let dialog = await screen.findByRole('dialog', { name: 'Add Announcement' })
  expect(
    within(dialog).getByRole('checkbox', { name: 'Popup on login' })
  ).toBeChecked()
  await user.type(
    within(dialog).getByRole('textbox', { name: 'Content' }),
    'New update'
  )
  await user.click(within(dialog).getByRole('button', { name: 'Add' }))
  await waitFor(() =>
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  )
  expect(within(row('New update')).getByText('Login popups')).toBeVisible()

  await user.click(
    within(row('Legacy update')).getByRole('button', { name: 'Edit' })
  )
  dialog = await screen.findByRole('dialog', { name: 'Edit Announcement' })
  expect(
    within(dialog).getByRole('checkbox', { name: 'Popup on login' })
  ).not.toBeChecked()
  await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))
  await user.click(
    within(row('Popup update')).getByRole('button', { name: 'Edit' })
  )
  dialog = await screen.findByRole('dialog')
  await user.click(
    within(dialog).getByRole('checkbox', { name: 'Popup on login' })
  )
  await user.click(within(dialog).getByRole('button', { name: 'Update' }))
  await waitFor(() =>
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  )
  expect(within(row('Popup update')).getByText('List only')).toBeVisible()
  await user.click(screen.getByRole('button', { name: 'Save Settings' }))
  await waitFor(() => expect(api.put).toHaveBeenCalledTimes(1))
  const request = vi.mocked(api.put).mock.calls[0]?.[1] as {
    key: string
    value: string
  }
  expect(request.key).toBe('console_setting.announcements')
  expect(JSON.parse(request.value)).toEqual([
    { ...entries[0], popupOnLogin: false },
    entries[1],
    entries[2],
    expect.objectContaining({
      id: 4,
      content: 'New update',
      popupOnLogin: true,
    }),
  ])
  expect(client.getQueryState(STATUS_QUERY_KEY)?.isInvalidated).toBe(true)
})

test('preview reuses sanitized announcement details and does not save or change the list', async () => {
  const user = userEvent.setup()
  renderSettings([
    { ...entries[0], content: '**Safe preview**<script>bad()</script>' },
  ])
  await user.click(screen.getByRole('button', { name: 'Preview' }))
  const dialog = await screen.findByRole('dialog', {
    name: 'Announcement Details',
  })
  expect(
    within(dialog).getByText('Safe preview', { selector: 'strong' })
  ).toBeVisible()
  expect(within(dialog).getByText('Read the notes')).toBeVisible()
  expect(dialog.querySelector('script')).toBeNull()
  await user.keyboard('{Escape}')
  await waitFor(() =>
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  )
  expect(screen.getByRole('button', { name: 'Save Settings' })).toBeDisabled()
  expect(api.put).not.toHaveBeenCalled()
})

test('filter changes reset selection and select-all/batch deletion affect only visible announcements after confirmation', async () => {
  const user = userEvent.setup()
  renderSettings()
  await user.click(screen.getByRole('checkbox', { name: 'Select all' }))
  expect(screen.getByRole('button', { name: 'Delete (3)' })).toBeEnabled()
  await selectFilter('Login popups')
  expect(screen.getByRole('button', { name: 'Delete (0)' })).toBeDisabled()
  expect(screen.queryByText('Legacy update')).not.toBeInTheDocument()
  await user.click(screen.getByRole('checkbox', { name: 'Select all' }))
  await user.click(screen.getByRole('button', { name: 'Delete (1)' }))
  let confirm = await screen.findByRole('alertdialog')
  expect(
    within(confirm).getByText(
      'This announcement will be removed from the list.'
    )
  ).toBeVisible()
  await user.click(within(confirm).getByRole('button', { name: 'Cancel' }))
  expect(row('Popup update')).toBeVisible()
  await user.click(screen.getByRole('button', { name: 'Delete (1)' }))
  confirm = await screen.findByRole('alertdialog')
  await user.click(within(confirm).getByRole('button', { name: 'Delete' }))
  await waitFor(() =>
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
  )
  expect(screen.getByRole('checkbox', { name: 'Select all' })).toHaveAttribute(
    'aria-disabled',
    'true'
  )
  await selectFilter('List only')
  expect(row('Legacy update')).toBeVisible()
  expect(row('List update')).toBeVisible()
  expect(screen.queryByText('Popup update')).not.toBeInTheDocument()
  await user.click(
    within(row('Legacy update')).getByRole('checkbox', {
      name: 'Select announcement',
    })
  )
  expect(
    screen.getByRole('checkbox', { name: 'Select all' })
  ).toBePartiallyChecked()
  await user.click(screen.getByRole('button', { name: 'Save Settings' }))
  await waitFor(() => expect(api.put).toHaveBeenCalledTimes(1))
  const request = vi.mocked(api.put).mock.calls[0]?.[1] as { value: string }
  expect(JSON.parse(request.value)).toEqual([entries[1], entries[2]])
})

test('filtered deletion keeps hidden announcements even when legacy IDs are duplicated', async () => {
  const user = userEvent.setup()
  renderSettings([entries[0], { ...entries[1], id: 1 }])
  await selectFilter('Login popups')
  await user.click(screen.getByRole('checkbox', { name: 'Select all' }))
  await user.click(screen.getByRole('button', { name: 'Delete (1)' }))
  const confirmation = await screen.findByRole('alertdialog')
  await user.click(within(confirmation).getByRole('button', { name: 'Delete' }))
  await selectFilter('List only')
  expect(row('Legacy update')).toBeVisible()
  await user.click(screen.getByRole('button', { name: 'Save Settings' }))
  await waitFor(() => expect(api.put).toHaveBeenCalledTimes(1))
  const request = vi.mocked(api.put).mock.calls[0]?.[1] as { value: string }
  expect(JSON.parse(request.value)).toEqual([{ ...entries[1], id: 1 }])
})

test('single deletion requires shared confirmation and cancelling retains the announcement', async () => {
  const user = userEvent.setup()
  renderSettings()
  await user.click(
    within(row('List update')).getByRole('button', { name: 'Open menu' })
  )
  await user.click(await screen.findByRole('menuitem', { name: 'Delete' }))
  const confirm = await screen.findByRole('alertdialog')
  await user.click(within(confirm).getByRole('button', { name: 'Cancel' }))
  expect(row('List update')).toBeVisible()
  expect(api.put).not.toHaveBeenCalled()
})

test('saving announcements and the global switch invalidates and refreshes the shared status cache', async () => {
  const user = userEvent.setup()
  const client = renderSettings()
  client.setQueryData(STATUS_QUERY_KEY, {
    announcements_enabled: true,
    announcements: [],
  })
  await user.click(screen.getByRole('switch', { name: 'Enabled' }))
  await waitFor(() =>
    expect(api.put).toHaveBeenCalledWith('/api/option/', {
      key: 'console_setting.announcements_enabled',
      value: false,
    })
  )
  expect(client.getQueryState(STATUS_QUERY_KEY)?.isInvalidated).toBe(true)
  await act(async () => {
    await client.fetchQuery(statusQueryOptions)
  })
  expect(client.getQueryData(STATUS_QUERY_KEY)).toEqual({
    announcements_enabled: true,
    announcements: entries,
  })
})

test('one hundred entries disable adding another announcement', () => {
  renderSettings(
    Array.from({ length: 100 }, (_, index) => ({
      ...entries[0],
      id: index + 1,
      content: `Entry ${index + 1}`,
    }))
  )
  expect(
    screen.getByRole('button', { name: 'Add Announcement' })
  ).toBeDisabled()
})

test('failed saves retain pending changes for a retry', async () => {
  const user = userEvent.setup()
  vi.mocked(api.put).mockResolvedValue({
    data: { success: false, message: 'Save rejected' },
  })
  renderSettings()
  await user.click(
    within(row('List update')).getByRole('button', { name: 'Edit' })
  )
  const dialog = await screen.findByRole('dialog')
  await user.type(
    within(dialog).getByRole('textbox', { name: 'Content' }),
    ' changed'
  )
  await user.click(within(dialog).getByRole('button', { name: 'Update' }))
  await user.click(screen.getByRole('button', { name: 'Save Settings' }))
  await waitFor(() => expect(api.put).toHaveBeenCalledTimes(1))
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Save Settings' })).toBeEnabled()
  )
})
