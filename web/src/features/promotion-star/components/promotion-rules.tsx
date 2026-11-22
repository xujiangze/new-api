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
import type { ColumnDef } from '@tanstack/react-table'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { TruncatedCell } from '@/components/data-table'
import { ErrorState } from '@/components/error-state'
import { Badge } from '@/components/ui/badge'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

import { getPromotionRules } from '../api'
import { usePromotionQuery } from '../hooks/use-promotion-query'
import { promotionTime } from '../lib/format'
import type { PromotionRule } from '../types'
import { PromotionTable } from './promotion-table'

export function PromotionRules(props: { userId: number }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const [pagination, setPagination] = useState({ pageIndex: 0, pageSize: 20 })
  const params = { p: pagination.pageIndex + 1, page_size: pagination.pageSize }
  const query = usePromotionQuery(['rules', props.userId, params], (signal) =>
    getPromotionRules(props.userId, params, signal)
  )
  const columns: ColumnDef<PromotionRule>[] = [
    {
      accessorKey: 'id',
      header: t('Rule'),
      meta: { label: t('Rule'), mobileTitle: true },
    },
    {
      accessorKey: 'effective_at_ms',
      header: t('Effective time'),
      cell: ({ row }) => promotionTime(row.original.effective_at_ms, locale),
      meta: { label: t('Effective time') },
    },
    {
      accessorKey: 'status',
      header: t('Status'),
      cell: ({ row }) => (
        <Badge variant='outline'>
          {row.original.status === 'active' ? t('Active') : t('Suspended')}
        </Badge>
      ),
      meta: { label: t('Status') },
    },
    {
      accessorKey: 'rate_bps',
      header: t('Rate'),
      cell: ({ row }) =>
        `${formatNumber(row.original.rate_bps / 100, locale)}%`,
      meta: { label: t('Rate') },
    },
    {
      accessorKey: 'reason',
      header: t('Public reason'),
      cell: ({ row }) => (
        <TruncatedCell className='max-w-80'>
          {row.original.reason}
        </TruncatedCell>
      ),
      meta: { label: t('Public reason') },
    },
  ]
  if (query.isError) {
    return (
      <ErrorState
        title={t('Failed to load Promotion Star data')}
        onRetry={() => void query.refetch()}
      />
    )
  }
  return (
    <PromotionTable
      columns={columns}
      page={query.data}
      pagination={pagination}
      onPaginationChange={setPagination}
      loading={query.isPending}
      fetching={query.isFetching}
    />
  )
}
