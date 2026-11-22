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
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

import { getPromotionInvitees, getPromotionLedger } from '../api'
import {
  usePromotionAccess,
  usePromotionQuery,
} from '../hooks/use-promotion-query'
import type {
  PromotionFilters,
  PromotionInvitee,
  PromotionLedgerRow,
} from '../types'
import { PromotionAmount } from './promotion-amount'
import { PromotionTable } from './promotion-table'

export function PromotionInvitees(props: {
  filters: PromotionFilters
  targetId?: number
}) {
  const { t } = useTranslation()
  const { scope } = usePromotionAccess()
  const [pagination, setPagination] = useState({ pageIndex: 0, pageSize: 20 })
  const params = {
    ...props.filters,
    p: pagination.pageIndex + 1,
    page_size: pagination.pageSize,
  }
  const query = usePromotionQuery(
    ['invitees', props.targetId, params],
    (signal) => getPromotionInvitees(scope, props.targetId, params, signal)
  )
  const columns: ColumnDef<PromotionInvitee>[] = [
    {
      accessorKey: 'user_id',
      header: t('User ID'),
      meta: { label: t('User ID'), mobileTitle: true },
    },
    {
      accessorKey: 'display_name',
      header: t('Name'),
      cell: ({ row }) => (
        <TruncatedCell className='max-w-52'>
          {row.original.display_name || '—'}
        </TruncatedCell>
      ),
      meta: { label: t('Name') },
    },
    {
      accessorKey: 'wallet_consumption',
      header: t('Wallet consumption'),
      cell: ({ row }) => (
        <PromotionAmount value={row.original.wallet_consumption} />
      ),
      meta: { label: t('Wallet consumption') },
    },
    {
      accessorKey: 'wallet_refund',
      header: t('Wallet refunds'),
      cell: ({ row }) => <PromotionAmount value={row.original.wallet_refund} />,
      meta: { label: t('Wallet refunds') },
    },
    {
      accessorKey: 'wallet_net',
      header: t('Net wallet consumption'),
      cell: ({ row }) => <PromotionAmount value={row.original.wallet_net} />,
      meta: { label: t('Net wallet consumption') },
    },
    {
      accessorKey: 'commission_net',
      header: t('Net commission'),
      cell: ({ row }) => (
        <PromotionAmount value={row.original.commission_net} />
      ),
      meta: { label: t('Net commission') },
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

export function PromotionLedger(props: {
  filters: PromotionFilters
  targetId?: number
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const { scope } = usePromotionAccess()
  const [pagination, setPagination] = useState({ pageIndex: 0, pageSize: 20 })
  const params = {
    ...props.filters,
    p: pagination.pageIndex + 1,
    page_size: pagination.pageSize,
  }
  const query = usePromotionQuery(
    ['ledger', props.targetId, params],
    (signal) => getPromotionLedger(scope, props.targetId, params, signal)
  )
  const sourceLabels: Record<string, string> = {
    wallet_usage: t('Wallet consumption'),
    subscription_balance: t('Subscription balance'),
    manual_adjustment: t('Commission adjustments'),
  }
  const kindLabels: Record<string, string> = {
    consumption: t('Consumption'),
    refund: t('Refund'),
    adjustment: t('Adjustment'),
  }
  const columns: ColumnDef<PromotionLedgerRow>[] = [
    {
      accessorKey: 'day',
      header: t('Date'),
      meta: { label: t('Date'), mobileTitle: true },
    },
    {
      accessorKey: 'invitee_user_id',
      header: t('Invitee'),
      cell: ({ row }) => (
        <span>
          {row.original.invitee_user_id || '—'}
          {row.original.invitee_name && (
            <TruncatedCell className='max-w-36'>
              {row.original.invitee_name}
            </TruncatedCell>
          )}
        </span>
      ),
      meta: { label: t('Invitee') },
    },
    {
      accessorKey: 'source',
      header: t('Source'),
      cell: ({ row }) => sourceLabels[row.original.source] ?? t('Unknown'),
      meta: { label: t('Source') },
    },
    {
      accessorKey: 'rule_id',
      header: t('Rule'),
      cell: ({ row }) => row.original.rule_id || '—',
      meta: { label: t('Rule') },
    },
    {
      accessorKey: 'rate_bps',
      header: t('Rate'),
      cell: ({ row }) =>
        `${formatNumber(row.original.rate_bps / 100, locale)}%`,
      meta: { label: t('Rate') },
    },
    {
      accessorKey: 'kind',
      header: t('Kind'),
      cell: ({ row }) => kindLabels[row.original.kind] ?? t('Unknown'),
      meta: { label: t('Kind') },
    },
    {
      accessorKey: 'basis_quota',
      header: t('Commission basis'),
      cell: ({ row }) => <PromotionAmount value={row.original.basis_quota} />,
      meta: { label: t('Commission basis') },
    },
    {
      accessorKey: 'commission_quota',
      header: t('Commission'),
      cell: ({ row }) => (
        <PromotionAmount value={row.original.commission_quota} />
      ),
      meta: { label: t('Commission') },
    },
    {
      accessorKey: 'public_reason',
      header: t('Public reason'),
      cell: ({ row }) => (
        <TruncatedCell className='max-w-52'>
          {row.original.public_reason || '—'}
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
    <div className='space-y-3'>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Daily summaries by invitee, source, rule and kind. No individual request details are shown.'
        )}
      </p>
      <PromotionTable
        columns={columns}
        page={query.data}
        pagination={pagination}
        onPaginationChange={setPagination}
        loading={query.isPending}
        fetching={query.isFetching}
      />
    </div>
  )
}
