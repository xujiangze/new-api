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

import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout/components/section-page-layout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

import { getPromotionProfiles } from './api'
import { PromotionAccess } from './components/promotion-access'
import { PromotionAdminDialog } from './components/promotion-admin-dialog'
import { PromotionReview } from './components/promotion-review'
import { PromotionRules } from './components/promotion-rules'
import { PromotionTable } from './components/promotion-table'
import { usePromotionQuery } from './hooks/use-promotion-query'
import { PromotionWorkspace } from './index'
import type { PromotionAction, PromotionProfile } from './types'

function AdminProfiles() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const [pagination, setPagination] = useState({ pageIndex: 0, pageSize: 20 })
  const [selectedId, setSelectedId] = useState<number | null>(null)
  const [dialog, setDialog] = useState<{
    action: PromotionAction
    profile?: PromotionProfile
  } | null>(null)
  const params = { p: pagination.pageIndex + 1, page_size: pagination.pageSize }
  const query = usePromotionQuery(['profiles', params], (signal) =>
    getPromotionProfiles(params, signal)
  )
  const columns: ColumnDef<PromotionProfile>[] = [
    {
      accessorKey: 'user_id',
      header: t('User ID'),
      meta: { label: t('User ID'), mobileTitle: true },
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
      header: t('Current commission rate'),
      cell: ({ row }) =>
        `${formatNumber(row.original.rate_bps / 100, locale)}%`,
      meta: { label: t('Current commission rate') },
    },
    {
      id: 'actions',
      header: t('Actions'),
      cell: ({ row }) => (
        <div className='flex flex-wrap gap-2'>
          <Button
            variant='outline'
            size='sm'
            onClick={() => setSelectedId(row.original.user_id)}
          >
            {t('View summaries')}
          </Button>
          <Button
            variant='outline'
            size='sm'
            onClick={() => setDialog({ action: 'rate', profile: row.original })}
          >
            {t('Change commission rate')}
          </Button>
          {row.original.status === 'active' ? (
            <Button
              variant='outline'
              size='sm'
              onClick={() =>
                setDialog({ action: 'suspend', profile: row.original })
              }
            >
              {t('Suspend')}
            </Button>
          ) : (
            <Button
              variant='outline'
              size='sm'
              onClick={() =>
                setDialog({ action: 'grant', profile: row.original })
              }
            >
              {t('Re-enable')}
            </Button>
          )}
          <Button
            variant='outline'
            size='sm'
            onClick={() =>
              setDialog({ action: 'adjust', profile: row.original })
            }
          >
            {t('Append commission adjustment')}
          </Button>
        </div>
      ),
      meta: { label: t('Actions') },
    },
  ]
  return (
    <div className='space-y-4'>
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <p className='text-muted-foreground text-sm'>
          {t(
            'Promotion Star is a personal capability, not an administrator role.'
          )}
        </p>
        <Button onClick={() => setDialog({ action: 'grant' })}>
          {t('Grant Promotion Star')}
        </Button>
      </div>
      {query.isError ? (
        <ErrorState
          title={t('Failed to load Promotion Star data')}
          onRetry={() => void query.refetch()}
        />
      ) : (
        <PromotionTable
          columns={columns}
          page={query.data}
          pagination={pagination}
          onPaginationChange={setPagination}
          loading={query.isPending}
          fetching={query.isFetching}
        />
      )}
      {selectedId !== null && (
        <Card>
          <CardHeader>
            <CardTitle>
              {t('Promotion summaries for user {{id}}', { id: selectedId })}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <Tabs key={selectedId} defaultValue='overview' className='gap-4'>
              <div className='overflow-x-auto'>
                <TabsList aria-label={t('Promotion summaries')}>
                  <TabsTrigger value='overview'>{t('Overview')}</TabsTrigger>
                  <TabsTrigger value='invitees'>
                    {t('Direct invitees')}
                  </TabsTrigger>
                  <TabsTrigger value='ledger'>
                    {t('Commission ledger')}
                  </TabsTrigger>
                  <TabsTrigger value='rules'>{t('Rule history')}</TabsTrigger>
                </TabsList>
              </div>
              <TabsContent value='overview'>
                <PromotionWorkspace section='overview' targetId={selectedId} />
              </TabsContent>
              <TabsContent value='invitees'>
                <PromotionWorkspace section='invitees' targetId={selectedId} />
              </TabsContent>
              <TabsContent value='ledger'>
                <PromotionWorkspace section='ledger' targetId={selectedId} />
              </TabsContent>
              <TabsContent value='rules'>
                <PromotionRules userId={selectedId} />
              </TabsContent>
            </Tabs>
          </CardContent>
        </Card>
      )}
      {dialog && (
        <PromotionAdminDialog
          key={`${dialog.action}:${dialog.profile?.user_id}`}
          {...dialog}
          onClose={() => setDialog(null)}
        />
      )}
    </div>
  )
}

export function PromotionStarAdmin() {
  const { t } = useTranslation()
  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Promotion Star management')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <PromotionAccess scope='admin'>
          <Tabs defaultValue='profiles' className='gap-4'>
            <TabsList aria-label={t('Promotion Star management')}>
              <TabsTrigger value='profiles'>
                {t('Promotion profiles')}
              </TabsTrigger>
              <TabsTrigger value='review'>{t('Reconciliation')}</TabsTrigger>
            </TabsList>
            <TabsContent value='profiles'>
              <AdminProfiles />
            </TabsContent>
            <TabsContent value='review'>
              <PromotionReview />
            </TabsContent>
          </Tabs>
        </PromotionAccess>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
