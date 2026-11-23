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
import type { StatusData } from '@/lib/status-query'

export interface PopupAnnouncement {
  id?: number
  content: string
  extra: string
  publishDate: string
  type: string
  version: string
}

/** A non-security fingerprint of the displayed version, never the popup flag. */
export function getAnnouncementVersion(
  announcement: Omit<PopupAnnouncement, 'version'>
): string {
  const content = JSON.stringify([
    announcement.id ?? null,
    announcement.content,
    announcement.extra,
    announcement.publishDate,
    announcement.type,
  ])
  let hash = 0xcbf29ce484222325n
  for (let index = 0; index < content.length; index += 1) {
    hash = BigInt.asUintN(
      64,
      (hash ^ BigInt(content.charCodeAt(index))) * 0x100000001b3n
    )
  }
  return `v1-${hash.toString(16).padStart(16, '0')}`
}

export function getPopupAnnouncements(
  status: StatusData | null
): PopupAnnouncement[] {
  if (
    status?.announcements_enabled !== true ||
    !Array.isArray(status.announcements)
  ) {
    return []
  }

  const announcements: PopupAnnouncement[] = []
  for (const item of status.announcements) {
    if (!item || typeof item !== 'object') continue
    const value = item as Record<string, unknown>
    if (
      value.popupOnLogin !== true ||
      typeof value.content !== 'string' ||
      !value.content.trim()
    ) {
      continue
    }
    const announcement = {
      id: typeof value.id === 'number' ? value.id : undefined,
      content: value.content,
      extra: typeof value.extra === 'string' ? value.extra : '',
      publishDate:
        typeof value.publishDate === 'string' ? value.publishDate : '',
      type: typeof value.type === 'string' ? value.type : 'default',
    }
    const version = getAnnouncementVersion(announcement)
    if (!announcements.some((existing) => existing.version === version)) {
      announcements.push({ ...announcement, version })
    }
  }

  return announcements.sort((a, b) => {
    const aDate = Date.parse(a.publishDate) || 0
    const bDate = Date.parse(b.publishDate) || 0
    return bDate - aDate
  })
}
