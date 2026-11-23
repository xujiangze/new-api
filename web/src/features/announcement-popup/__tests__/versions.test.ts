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
import { expect, test } from 'vitest'

import { getPopupAnnouncements } from '../lib/announcements'

const entry = {
  id: 7,
  content: 'Service update',
  extra: 'Read more',
  publishDate: '2026-11-23T09:00:00Z',
  type: 'default',
  popupOnLogin: true,
}

test('content, notes and displayed metadata edits change the version but popup toggles and unrelated fields do not', () => {
  const read = (value: Record<string, unknown>) =>
    getPopupAnnouncements({
      announcements_enabled: true,
      announcements: [value],
    })[0]?.version
  const original = read(entry)
  expect(original).toMatch(/^v1-[0-9a-f]{16}$/)
  expect(read({ ...entry, unrelated: 'metadata', popupOnLogin: true })).toBe(
    original
  )
  expect(read({ ...entry, popupOnLogin: false })).toBeUndefined()
  expect(read({ ...entry, content: 'Revised update' })).not.toBe(original)
  expect(read({ ...entry, extra: 'Changed notes' })).not.toBe(original)
  expect(read({ ...entry, publishDate: '2026-11-24T09:00:00Z' })).not.toBe(
    original
  )
  expect(read({ ...entry, type: 'warning' })).not.toBe(original)
  expect(read({ ...entry, id: 8 })).not.toBe(original)
})

test('missing IDs use stable content versions independent of list order and object property order', () => {
  const legacy = { ...entry, id: undefined }
  const original = getPopupAnnouncements({
    announcements_enabled: true,
    announcements: [legacy],
  })[0]?.version
  const reordered = getPopupAnnouncements({
    announcements_enabled: true,
    announcements: [
      { ...entry, id: 8, content: 'Other update' },
      {
        type: legacy.type,
        publishDate: legacy.publishDate,
        extra: legacy.extra,
        popupOnLogin: true,
        content: legacy.content,
      },
    ],
  })
  expect(reordered.find((item) => item.id === undefined)?.version).toBe(
    original
  )
})
