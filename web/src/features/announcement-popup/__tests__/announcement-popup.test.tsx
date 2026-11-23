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
import { StrictMode, useState } from 'react'
import { toast } from 'sonner'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { useStatus } from '@/hooks/use-status'
import { api } from '@/lib/api'
import { initializeFrontendCache } from '@/lib/frontend-cache'
import { createAppQueryClient } from '@/lib/query-client'
import {
  STATUS_QUERY_KEY,
  statusQueryOptions,
  type StatusData,
} from '@/lib/status-query'
import {
  ANNOUNCEMENT_POPUP_STORAGE_KEY,
  useAnnouncementPopupStore,
} from '@/stores/announcement-popup-store'
import { useAuthStore } from '@/stores/auth-store'

import { AnnouncementPopupGate } from '../announcement-popup-gate'

const update = {
  id: 7,
  content: 'Platform update',
  extra: 'Please check your integrations.',
  publishDate: '2026-11-23T09:00:00Z',
  type: 'warning',
  popupOnLogin: true,
}
const status: StatusData = {
  announcements_enabled: true,
  announcements: [update],
}
const clients: QueryClient[] = []

function signIn(id: number) {
  useAuthStore.getState().auth.setUser({ id, username: `user-${id}`, role: 1 })
}

function ConsoleFixture(props: { sharedStatus?: boolean }) {
  const [page, setPage] = useState('Overview')
  const [visible, setVisible] = useState(true)
  return (
    <>
      <button type='button' onClick={() => setPage('Usage')}>
        Change page
      </button>
      <button type='button' onClick={() => setVisible((value) => !value)}>
        Re-enter console
      </button>
      <main>{page}</main>
      {props.sharedStatus && <SharedStatus />}
      {visible && <AnnouncementPopupGate />}
    </>
  )
}

function SharedStatus() {
  const { status: sharedStatus } = useStatus()
  return <span>{String(sharedStatus?.system_name ?? '')}</span>
}

function renderConsole(
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } }),
  sharedStatus = false
) {
  clients.push(client)
  return {
    client,
    ...render(
      <StrictMode>
        <QueryClientProvider client={client}>
          <ConsoleFixture sharedStatus={sharedStatus} />
        </QueryClientProvider>
      </StrictMode>
    ),
  }
}

async function reenterConsole() {
  const user = userEvent.setup()
  await user.click(screen.getByRole('button', { name: 'Re-enter console' }))
  await user.click(screen.getByRole('button', { name: 'Re-enter console' }))
}

beforeEach(() => {
  window.localStorage.clear()
  useAnnouncementPopupStore.setState({ confirmedVersions: {} })
  signIn(11)
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: status },
  })
})

afterEach(() => {
  cleanup()
  clients.splice(0).forEach((client) => client.clear())
  useAuthStore.getState().auth.reset()
  useAnnouncementPopupStore.setState({ confirmedVersions: {} })
  window.localStorage.clear()
})

test('unread announcements merge into one date-sorted popup without scheduling future dates', async () => {
  vi.mocked(api.get).mockResolvedValue({
    data: {
      success: true,
      data: {
        announcements_enabled: true,
        Notice: 'The legacy notice must not be included',
        announcements: [
          update,
          {
            ...update,
            id: 8,
            content: 'Newest update',
            publishDate: '2099-01-01T09:00:00Z',
          },
          {
            ...update,
            id: 9,
            content: 'Legacy list entry',
            popupOnLogin: undefined,
          },
          { ...update, id: 10, content: 'Disabled popup', popupOnLogin: false },
        ],
      },
    },
  })
  renderConsole()

  const dialog = await screen.findByRole('dialog', { name: 'Announcements' })
  expect(screen.getAllByRole('dialog')).toHaveLength(1)
  expect(
    within(dialog)
      .getAllByRole('article')
      .map((article) => article.textContent)
  ).toEqual([
    expect.stringContaining('Newest update'),
    expect.stringContaining('Platform update'),
  ])
  expect(screen.queryByText('Legacy list entry')).not.toBeInTheDocument()
  expect(screen.queryByText('Disabled popup')).not.toBeInTheDocument()
  expect(
    screen.queryByText('The legacy notice must not be included')
  ).not.toBeInTheDocument()
})

test('confirming persists only displayed versions, survives cache upgrades, and skips them on re-entry', async () => {
  const user = userEvent.setup()
  const { client } = renderConsole()
  const dialog = await screen.findByRole('dialog')
  act(() =>
    client.setQueryData(STATUS_QUERY_KEY, {
      ...status,
      announcements: [{ ...update, content: 'Revised update' }],
    })
  )
  expect(within(dialog).getByText('Platform update')).toBeVisible()
  expect(screen.queryByText('Revised update')).not.toBeInTheDocument()
  await user.click(within(dialog).getByRole('button', { name: 'I understand' }))
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  const stored = window.localStorage.getItem(ANNOUNCEMENT_POPUP_STORAGE_KEY)
  expect(stored).toBeTruthy()
  expect(stored).not.toContain('Platform update')
  expect(stored).not.toContain('Please check your integrations')
  window.localStorage.setItem('newapi:default:cache-version', 'older-build')
  initializeFrontendCache()
  expect(window.localStorage.getItem(ANNOUNCEMENT_POPUP_STORAGE_KEY)).toBe(
    stored
  )
  useAnnouncementPopupStore.setState({ confirmedVersions: {} })
  window.localStorage.setItem(ANNOUNCEMENT_POPUP_STORAGE_KEY, stored ?? '')
  await useAnnouncementPopupStore.persist.rehydrate()
  await reenterConsole()
  await waitFor(() => expect(api.get).toHaveBeenCalledTimes(2))
  await act(async () => {
    await client.fetchQuery(statusQueryOptions)
  })
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()

  vi.mocked(api.get).mockResolvedValue({
    data: {
      success: true,
      data: {
        ...status,
        announcements: [{ ...update, content: 'Revised update' }],
      },
    },
  })
  await reenterConsole()
  expect(await screen.findByText('Revised update')).toBeVisible()
})

test('a background status refresh or child-page change never opens another popup during the visit', async () => {
  const user = userEvent.setup()
  const { client } = renderConsole()
  await screen.findByRole('dialog')
  await user.click(screen.getByRole('button', { name: 'I understand' }))
  act(() =>
    client.setQueryData(STATUS_QUERY_KEY, {
      ...status,
      announcements: [{ ...update, id: 10, content: 'Another update' }],
    })
  )
  await user.click(screen.getByRole('button', { name: 'Change page' }))
  expect(screen.getByRole('main')).toHaveTextContent('Usage')
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  expect(api.get).toHaveBeenCalledTimes(1)
})

test.each(['close button', 'Escape', 'backdrop'] as const)(
  'closing with %s does not confirm and the next visit reminds again',
  async (method) => {
    const user = userEvent.setup()
    renderConsole()
    const dialog = await screen.findByRole('dialog')
    if (method === 'Escape') {
      await user.keyboard('{Escape}')
    } else if (method === 'backdrop') {
      const backdrop = document.querySelector<HTMLElement>(
        '[data-slot=dialog-overlay]'
      )
      if (!backdrop) throw new Error('The modal backdrop is missing')
      await user.click(backdrop)
    } else {
      await user.click(within(dialog).getByRole('button', { name: 'Close' }))
    }
    await waitFor(() =>
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    )
    expect(useAnnouncementPopupStore.getState().confirmedVersions).toEqual({})
    await reenterConsole()
    expect(await screen.findByText('Platform update')).toBeVisible()
  }
)

test('switching accounts isolates confirmations, and signing out removes the popup immediately', async () => {
  const user = userEvent.setup()
  const { client } = renderConsole()
  await screen.findByRole('dialog')
  await user.click(screen.getByRole('button', { name: 'I understand' }))
  act(() => signIn(22))
  expect(await screen.findByText('Platform update')).toBeVisible()
  act(() => useAuthStore.getState().auth.reset())
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  act(() => signIn(11))
  await act(async () => {
    await client.fetchQuery(statusQueryOptions)
  })
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
})

test('a pending response for the previous account cannot display after account switch or sign-out', async () => {
  let resolveResponse: (value: { data: unknown }) => void = () => {
    throw new Error('Response not initialized')
  }
  const response = new Promise<{ data: unknown }>((resolve) => {
    resolveResponse = resolve
  })
  vi.mocked(api.get).mockReturnValue(response)
  const { client } = renderConsole()
  await waitFor(() => expect(api.get).toHaveBeenCalledTimes(1))
  act(() => signIn(22))
  act(() => useAuthStore.getState().auth.reset())
  await act(async () => {
    resolveResponse({ data: { success: true, data: status } })
    await response
  })
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  act(() => signIn(22))
  vi.mocked(api.get).mockResolvedValue({
    data: { success: true, data: status },
  })
  await act(async () => {
    await client.fetchQuery(statusQueryOptions)
  })
  expect(await screen.findByText('Platform update')).toBeVisible()
})

test('entry waits for fresh shared status rather than memory or localStorage placeholders and deduplicates requests', async () => {
  let resolveResponse: (value: { data: unknown }) => void = () => {
    throw new Error('Response not initialized')
  }
  const response = new Promise<{ data: unknown }>((resolve) => {
    resolveResponse = resolve
  })
  vi.mocked(api.get).mockReturnValue(response)
  window.localStorage.setItem('status', JSON.stringify(status))
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  client.setQueryData(STATUS_QUERY_KEY, status)
  renderConsole(client, true)
  await waitFor(() => expect(api.get).toHaveBeenCalledTimes(1))
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  await act(async () => {
    resolveResponse({
      data: {
        success: true,
        data: { system_name: 'Fresh status', announcements_enabled: false },
      },
    })
    await response
  })
  expect(await screen.findByText('Fresh status')).toBeVisible()
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  expect(api.get).toHaveBeenCalledTimes(1)
})

test.each([
  {
    name: 'globally disabled',
    data: { ...status, announcements_enabled: false },
  },
  { name: 'empty list', data: { ...status, announcements: [] } },
  {
    name: 'legacy data',
    data: {
      ...status,
      announcements: [{ ...update, popupOnLogin: undefined }],
    },
  },
  {
    name: 'invalid content',
    data: {
      ...status,
      announcements: [null, { popupOnLogin: true, content: 9 }],
    },
  },
])('$name leaves the console usable without a popup', async ({ data }) => {
  vi.mocked(api.get).mockResolvedValue({ data: { success: true, data } })
  const { client } = renderConsole()
  await act(async () => {
    await client.fetchQuery(statusQueryOptions)
  })
  expect(screen.getByRole('main')).toBeVisible()
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
})

test('a failed optional status read does not toast, redirect, use cached announcements, or retry on navigation', async () => {
  const redirect = vi.fn()
  const notify = vi.spyOn(toast, 'error')
  const client = createAppQueryClient(redirect)
  client.setDefaultOptions({ queries: { retry: false } })
  client.setQueryData(STATUS_QUERY_KEY, status)
  let rejectResponse: (reason: unknown) => void = () => {
    throw new Error('Response not initialized')
  }
  const response = new Promise<{ data: unknown }>((_resolve, reject) => {
    rejectResponse = reject
  })
  vi.mocked(api.get).mockReturnValue(response)
  renderConsole(client, true)
  await waitFor(() => expect(api.get).toHaveBeenCalledTimes(1))
  await userEvent
    .setup()
    .click(screen.getByRole('button', { name: 'Change page' }))
  await act(async () => {
    rejectResponse({ response: { status: 500 } })
    await response.catch(() => undefined)
  })
  await waitFor(() =>
    expect(client.getQueryState(STATUS_QUERY_KEY)?.status).toBe('error')
  )
  expect(redirect).not.toHaveBeenCalled()
  expect(notify).not.toHaveBeenCalled()
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  await userEvent
    .setup()
    .click(screen.getByRole('button', { name: 'Change page' }))
  expect(screen.getByRole('main')).toHaveTextContent('Usage')
  expect(api.get).toHaveBeenCalledTimes(1)
})

test('unavailable localStorage does not block acknowledgement or the console', async () => {
  const originalSetItem = Storage.prototype.setItem
  vi.spyOn(Storage.prototype, 'setItem').mockImplementation(function (
    this: Storage,
    key,
    value
  ) {
    if (key === ANNOUNCEMENT_POPUP_STORAGE_KEY) {
      throw new Error('Storage unavailable')
    }
    originalSetItem.call(this, key, value)
  })
  renderConsole()
  await screen.findByRole('dialog')
  await userEvent
    .setup()
    .click(screen.getByRole('button', { name: 'I understand' }))
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  expect(screen.getByRole('main')).toBeVisible()
  await reenterConsole()
  await waitFor(() => expect(api.get).toHaveBeenCalledTimes(2))
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
})

test('long content has a bounded keyboard-focusable scroll region and modal focus stays inside until Escape', async () => {
  const user = userEvent.setup()
  vi.mocked(api.get).mockResolvedValue({
    data: {
      success: true,
      data: {
        ...status,
        announcements: [
          {
            ...update,
            content: `[Release details](https://example.com)\n\n${'Long update '.repeat(30)}`,
          },
        ],
      },
    },
  })
  renderConsole()
  const dialog = await screen.findByRole('dialog', { name: 'Announcements' })
  const region = within(dialog).getByRole('region', {
    name: 'Announcement content',
  })
  expect(region).toHaveClass(
    'overflow-y-auto',
    'overscroll-contain',
    'max-h-[min(60dvh,32rem)]'
  )
  expect(region).toHaveAttribute('tabindex', '0')
  await waitFor(() =>
    expect(dialog.contains(document.activeElement)).toBe(true)
  )
  await user.tab()
  expect(dialog.contains(document.activeElement)).toBe(true)
  await user.tab({ shift: true })
  expect(dialog.contains(document.activeElement)).toBe(true)
  await user.keyboard('{Escape}')
  await waitFor(() =>
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  )
  await user.tab()
  expect(screen.getByRole('button', { name: 'Change page' })).toHaveFocus()
})
