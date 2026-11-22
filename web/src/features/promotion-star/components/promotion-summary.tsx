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
import { BadgePercent, Coins, Wallet } from 'lucide-react'
import { lazy, Suspense } from 'react'
import { useTranslation } from 'react-i18next'

import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { StatCard } from '@/features/dashboard/components/ui/stat-card'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

import { getPromotionOverview } from '../api'
import { usePromotionAmount } from '../hooks/use-promotion-amount'
import {
  usePromotionAccess,
  usePromotionQuery,
} from '../hooks/use-promotion-query'
import { promotionTime } from '../lib/format'
import type { PromotionFilters } from '../types'

const PromotionChart = lazy(() => import('./promotion-chart'))

export function PromotionSummary(props: {
  filters: PromotionFilters
  targetId?: number
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const format = usePromotionAmount()
  const { scope } = usePromotionAccess()
  const query = usePromotionQuery(
    ['overview', props.targetId, props.filters],
    (signal) =>
      getPromotionOverview(scope, props.targetId, props.filters, signal)
  )
  if (query.isError) {
    return (
      <ErrorState
        title={t('Failed to load Promotion Star data')}
        onRetry={() => void query.refetch()}
      />
    )
  }
  if (!query.data) return <LoadingState />
  const data = query.data
  return (
    <div className='space-y-4'>
      <div className='flex flex-wrap items-center gap-2 text-sm'>
        <Badge
          variant={data.profile.status === 'active' ? 'secondary' : 'outline'}
        >
          {data.profile.status === 'active' ? t('Active') : t('Suspended')}
        </Badge>
        <Badge variant='outline'>
          {data.pending_review
            ? t('Processing or pending review')
            : t('No pending review')}
        </Badge>
        <span className='text-muted-foreground'>
          {t('Last updated {{time}}', {
            time: promotionTime(data.updated_through, locale),
          })}
        </span>
      </div>
      <div className='grid gap-3 md:grid-cols-3'>
        <StatCard
          title={t('Net wallet consumption')}
          value={format(data.totals.wallet_net)}
          description={t('Direct invitees · selected period')}
          icon={Wallet}
          tone='accent-1'
          details={[
            {
              label: t('Wallet consumption'),
              value: format(data.totals.wallet_consumption),
            },
            {
              label: t('Wallet refunds'),
              value: format(data.totals.wallet_refund),
            },
          ]}
        />
        <StatCard
          title={t('Net commission')}
          value={format(data.totals.commission_net)}
          description={t('Commission ledger only; no wallet payouts')}
          icon={Coins}
          tone='accent-2'
          details={[
            {
              label: t('Net consumption commission'),
              value: format(data.totals.commission),
            },
            {
              label: t('Commission adjustments'),
              value: format(data.totals.adjustment),
            },
          ]}
        />
        <StatCard
          title={t('Current commission rate')}
          value={`${formatNumber(data.profile.rate_bps / 100, locale)}%`}
          description={t(
            'Historical entries retain their original rule and rate.'
          )}
          icon={BadgePercent}
          tone='accent-3'
        />
      </div>
      <Card>
        <CardHeader>
          <CardTitle>{t('Daily promotion summary')}</CardTitle>
        </CardHeader>
        <CardContent>
          <Suspense fallback={<LoadingState />}>
            <PromotionChart daily={data.daily} />
          </Suspense>
        </CardContent>
      </Card>
    </div>
  )
}
