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
import { zodResolver } from '@hookform/resolvers/zod'
import type { ColumnDef } from '@tanstack/react-table'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { TruncatedCell } from '@/components/data-table'
import { ErrorState } from '@/components/error-state'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Textarea } from '@/components/ui/textarea'
import { SecureVerificationDialog } from '@/features/auth/secure-verification'
import { toIntlLocale } from '@/i18n/languages'

import { getPromotionReviewEvents } from '../api'
import { usePromotionCommand } from '../hooks/use-promotion-command'
import { usePromotionQuery } from '../hooks/use-promotion-query'
import { promotionTime } from '../lib/format'
import type { PromotionReviewEvent } from '../types'
import { PromotionAmount } from './promotion-amount'
import { PromotionTable } from './promotion-table'

const reviewSchema = z.object({
  reason: z
    .string()
    .trim()
    .min(1, 'A public reason is required')
    .max(500, 'Use at most 500 characters'),
})

function ReviewDialog(props: {
  event: PromotionReviewEvent
  state: 'pending' | 'ignored'
  onClose: () => void
}) {
  const { t } = useTranslation()
  const command = usePromotionCommand()
  const form = useForm<z.infer<typeof reviewSchema>>({
    resolver: zodResolver(reviewSchema),
    defaultValues: { reason: '' },
  })
  const title =
    props.state === 'pending'
      ? t('Confirm reconciliation')
      : t('Exclude from commission')
  return (
    <>
      <ConfirmDialog
        open={!command.verification.isActive}
        onOpenChange={(open) => {
          if (!open && !command.busy) props.onClose()
        }}
        title={title}
        desc={t(
          'This preserves the original amount and settlement time. Unknown settlement outcomes can only be excluded; use a separate commission adjustment if needed.'
        )}
        isLoading={command.busy}
        destructive={props.state === 'ignored'}
        confirmText={t('Verify and save')}
        handleConfirm={() =>
          void form.handleSubmit(async (values) => {
            const saved = await command.submit({
              action: 'review',
              user_id: props.event.user_id,
              event_id: props.event.id,
              event_key: props.event.event_key,
              payload: {
                state: props.state,
                reason: values.reason,
                expected_updated_at_ms: props.event.updated_at_ms,
                expected_quota: props.event.quota,
                expected_settled_at_ms: props.event.settled_at_ms,
              },
            })
            if (saved) props.onClose()
          })()
        }
      >
        <p className='text-sm'>
          {t('User ID')}: {props.event.user_id} ·{' '}
          <PromotionAmount value={props.event.quota} />
        </p>
        <Form {...form}>
          <FormField
            control={form.control}
            name='reason'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Review reason')}</FormLabel>
                <FormControl>
                  <Textarea
                    {...field}
                    maxLength={500}
                    readOnly={command.busy}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
        </Form>
      </ConfirmDialog>
      <SecureVerificationDialog {...command.verification.dialogProps} />
    </>
  )
}

export function PromotionReview() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const [pagination, setPagination] = useState({ pageIndex: 0, pageSize: 20 })
  const [review, setReview] = useState<{
    event: PromotionReviewEvent
    state: 'pending' | 'ignored'
  } | null>(null)
  const params = { p: pagination.pageIndex + 1, page_size: pagination.pageSize }
  const query = usePromotionQuery(['review-events', params], (signal) =>
    getPromotionReviewEvents(params, signal)
  )
  const states: Record<string, string> = {
    review: t('Pending review'),
    pending: t('Processing'),
    processed: t('Processed'),
    ignored: t('Excluded'),
    provisional: t('Awaiting settlement'),
  }
  const sources: Record<string, string> = {
    wallet_usage: t('Wallet consumption'),
    subscription_balance: t('Subscription balance'),
    manual_adjustment: t('Commission adjustments'),
  }
  const columns: ColumnDef<PromotionReviewEvent>[] = [
    {
      accessorKey: 'user_id',
      header: t('User ID'),
      meta: { label: t('User ID'), mobileTitle: true },
    },
    {
      accessorKey: 'invitee_user_id',
      header: t('Invitee'),
      meta: { label: t('Invitee') },
    },
    {
      accessorKey: 'source',
      header: t('Source'),
      cell: ({ row }) => sources[row.original.source] ?? t('Unknown'),
      meta: { label: t('Source') },
    },
    {
      accessorKey: 'state',
      header: t('Status'),
      cell: ({ row }) => states[row.original.state] ?? t('Unknown'),
      meta: { label: t('Status') },
    },
    {
      accessorKey: 'quota',
      header: t('Amount'),
      cell: ({ row }) => <PromotionAmount value={row.original.quota} />,
      meta: { label: t('Amount') },
    },
    {
      accessorKey: 'settled_at_ms',
      header: t('Settlement time'),
      cell: ({ row }) => promotionTime(row.original.settled_at_ms, locale),
      meta: { label: t('Settlement time') },
    },
    {
      accessorKey: 'review_reason',
      header: t('Review reason'),
      cell: ({ row }) => {
        const reason = row.original.review_reason
        let label = reason || '—'
        if (reason === 'Funding requires reconciliation') {
          label = t('Funding requires reconciliation')
        }
        if (reason === 'Realtime wallet debits require reconciliation') {
          label = t('Realtime wallet debits require reconciliation')
        }
        if (reason === 'Wallet reservation requires reconciliation') {
          label = t('Wallet reservation requires reconciliation')
        }
        return <TruncatedCell className='max-w-64'>{label}</TruncatedCell>
      },
      meta: { label: t('Review reason') },
    },
    {
      id: 'actions',
      header: t('Actions'),
      cell: ({ row }) =>
        row.original.state === 'review' && (
          <div className='flex flex-wrap gap-2'>
            <Button
              variant='outline'
              size='sm'
              disabled={row.original.can_confirm !== true}
              onClick={() =>
                setReview({ event: row.original, state: 'pending' })
              }
            >
              {t('Confirm reconciliation')}
            </Button>
            <Button
              variant='outline'
              size='sm'
              onClick={() =>
                setReview({ event: row.original, state: 'ignored' })
              }
            >
              {t('Exclude from commission')}
            </Button>
          </div>
        ),
      meta: { label: t('Actions') },
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
          'Unknown settlement outcomes cannot be confirmed. Exclude them and append a separate adjustment after investigation.'
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
      {review && (
        <ReviewDialog
          key={`${review.event.id}:${review.state}`}
          {...review}
          onClose={() => setReview(null)}
        />
      )}
    </div>
  )
}
