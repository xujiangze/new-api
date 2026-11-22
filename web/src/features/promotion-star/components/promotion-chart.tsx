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
import { useTranslation } from 'react-i18next'
import { CartesianGrid, Line, LineChart, XAxis, YAxis } from 'recharts'

import { EmptyState } from '@/components/empty-state'
import {
  ChartContainer,
  ChartLegend,
  ChartLegendContent,
  ChartTooltip,
  ChartTooltipContent,
} from '@/components/ui/chart'

import { usePromotionAmount } from '../hooks/use-promotion-amount'
import { promotionQuotaNumber } from '../lib/format'
import type { PromotionOverview } from '../types'

export default function PromotionChart(props: {
  daily: PromotionOverview['daily']
}) {
  const { t } = useTranslation()
  const format = usePromotionAmount()
  const data = props.daily.map((day) => ({
    date: day.date,
    wallet_net: promotionQuotaNumber(day.wallet_net),
    commission_net: promotionQuotaNumber(day.commission_net),
  }))
  if (!data.length) {
    return <EmptyState title={t('No promotion activity in this period')} />
  }
  const config = {
    wallet_net: { label: t('Net wallet consumption'), color: 'var(--chart-1)' },
    commission_net: { label: t('Net commission'), color: 'var(--chart-2)' },
  }
  return (
    <div className='space-y-2'>
      <ChartContainer
        config={config}
        className='h-72 w-full'
        aria-label={t('Daily promotion summary')}
      >
        <LineChart data={data} accessibilityLayer>
          <CartesianGrid vertical={false} />
          <XAxis dataKey='date' tickLine={false} axisLine={false} />
          <YAxis
            tickFormatter={(value: number) => format(String(value))}
            width={92}
          />
          <ChartTooltip
            content={
              <ChartTooltipContent
                formatter={(value, name) => (
                  <span>
                    {config[name as keyof typeof config]?.label}:{' '}
                    {format(String(value))}
                  </span>
                )}
              />
            }
          />
          <ChartLegend content={<ChartLegendContent />} />
          <Line
            type='monotone'
            dataKey='wallet_net'
            stroke='var(--color-wallet_net)'
            strokeWidth={2}
            dot={false}
            isAnimationActive={false}
          />
          <Line
            type='monotone'
            dataKey='commission_net'
            stroke='var(--color-commission_net)'
            strokeWidth={2}
            dot={false}
            isAnimationActive={false}
          />
        </LineChart>
      </ChartContainer>
      {data.some(
        (day) => day.wallet_net === null || day.commission_net === null
      ) && (
        <p className='text-muted-foreground text-sm'>
          {t(
            'Amounts outside the chart range remain available in the summary tables.'
          )}
        </p>
      )}
    </div>
  )
}
