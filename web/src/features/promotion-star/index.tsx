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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout/components/section-page-layout'
import { Alert, AlertDescription } from '@/components/ui/alert'
import dayjs from '@/lib/dayjs'

import { PromotionAccess } from './components/promotion-access'
import { PromotionDates } from './components/promotion-dates'
import {
  PromotionInvitees,
  PromotionLedger,
} from './components/promotion-records'
import { PromotionSummary } from './components/promotion-summary'
import { initialPromotionDates } from './lib/format'
import type { PromotionSection } from './types'

export function PromotionWorkspace(props: {
  section: PromotionSection
  targetId?: number
}) {
  const { t } = useTranslation()
  const [filters, setFilters] = useState(initialPromotionDates)
  const invalidRange = filters.start_date > filters.end_date
  const excessiveRange =
    dayjs(filters.end_date).diff(dayjs(filters.start_date), 'day') >= 366
  const key = `${props.targetId}:${filters.start_date}:${filters.end_date}`
  return (
    <div className='space-y-4'>
      <PromotionDates value={filters} onChange={setFilters} />
      {invalidRange || excessiveRange ? (
        <Alert variant='destructive'>
          <AlertDescription>
            {invalidRange
              ? t('End time must be after start time')
              : t('Select a date range of at most 366 days')}
          </AlertDescription>
        </Alert>
      ) : (
        <>
          {props.section === 'overview' && (
            <PromotionSummary filters={filters} targetId={props.targetId} />
          )}
          {props.section === 'invitees' && (
            <PromotionInvitees
              key={key}
              filters={filters}
              targetId={props.targetId}
            />
          )}
          {props.section === 'ledger' && (
            <PromotionLedger
              key={key}
              filters={filters}
              targetId={props.targetId}
            />
          )}
        </>
      )}
    </div>
  )
}

export function PromotionStar(props: { section: PromotionSection }) {
  const { t } = useTranslation()
  const titles = {
    overview: t('Overview'),
    invitees: t('Direct invitees'),
    ledger: t('Commission ledger'),
  }
  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Promotion Star')} · {titles[props.section]}
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <PromotionAccess scope='self'>
          <PromotionWorkspace section={props.section} />
        </PromotionAccess>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
