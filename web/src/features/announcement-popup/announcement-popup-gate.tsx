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
import { useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { RichContent } from '@/components/rich-content'
import { Button } from '@/components/ui/button'
import { toIntlLocale } from '@/i18n/languages'
import { getAnnouncementColorClass } from '@/lib/colors'
import { formatTimestampRelative, formatTimestampToDate } from '@/lib/format'
import { statusQueryOptions } from '@/lib/status-query'
import { cn } from '@/lib/utils'
import { useAnnouncementPopupStore } from '@/stores/announcement-popup-store'
import { useAuthStore } from '@/stores/auth-store'

import {
  getPopupAnnouncements,
  type PopupAnnouncement,
} from './lib/announcements'

export function AnnouncementPopupGate() {
  const accountId = useAuthStore((state) => state.auth.user?.id)
  return accountId ? (
    <AccountAnnouncementPopup key={accountId} accountId={accountId} />
  ) : null
}

function AccountAnnouncementPopup(props: { accountId: number }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const queryClient = useQueryClient()
  const [announcements, setAnnouncements] = useState<PopupAnnouncement[]>([])

  useEffect(() => {
    let cancelled = false
    // Await a new shared request, not a five-minute cache hit or local placeholder.
    void queryClient
      .fetchQuery({
        ...statusQueryOptions,
        staleTime: 0,
      })
      .then((status) => {
        if (cancelled) return
        const confirmed =
          useAnnouncementPopupStore.getState().confirmedVersions[
            props.accountId
          ] ?? []
        setAnnouncements(
          getPopupAnnouncements(status).filter(
            (announcement) => !confirmed.includes(announcement.version)
          )
        )
      })
      .catch(() => {
        // An optional announcement must not interrupt normal console use.
      })
    return () => {
      cancelled = true
    }
  }, [props.accountId, queryClient])

  const acknowledge = () => {
    if (useAuthStore.getState().auth.user?.id !== props.accountId) return
    useAnnouncementPopupStore.getState().confirm(
      props.accountId,
      announcements.map((announcement) => announcement.version)
    )
    setAnnouncements([])
  }

  if (announcements.length === 0) return null

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) setAnnouncements([])
      }}
      title={t('Announcements')}
      description={t(
        'Please review the latest announcements. Confirming only marks these versions as read in this browser.'
      )}
      titleClassName='pr-8'
      contentClassName='sm:max-w-2xl'
      footer={<Button onClick={acknowledge}>{t('I understand')}</Button>}
    >
      <div
        role='region'
        aria-label={t('Announcement content')}
        tabIndex={0}
        className='max-h-[min(60dvh,32rem)] space-y-6 overflow-y-auto overscroll-contain pr-2 [overflow-wrap:anywhere] outline-offset-2'
      >
        {announcements.map((announcement) => (
          <article
            key={announcement.version}
            className='space-y-3 border-b pb-5 last:border-0 last:pb-0'
          >
            <h3 className='text-muted-foreground flex items-center gap-2 text-sm'>
              <span
                aria-hidden
                className={cn(
                  'inline-block size-2 shrink-0 rounded-full',
                  getAnnouncementColorClass(announcement.type)
                )}
              />
              {t('Published:')}{' '}
              <time
                dateTime={announcement.publishDate}
                title={formatTimestampToDate(
                  Date.parse(announcement.publishDate),
                  'milliseconds'
                )}
              >
                {formatTimestampRelative(
                  Date.parse(announcement.publishDate),
                  'milliseconds',
                  locale
                )}
              </time>
            </h3>
            <RichContent breaks content={announcement.content} />
            {announcement.extra && (
              <RichContent
                breaks
                content={announcement.extra}
                className='text-muted-foreground'
              />
            )}
          </article>
        ))}
      </div>
    </Dialog>
  )
}
