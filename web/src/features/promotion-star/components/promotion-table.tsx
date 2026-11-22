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
import type {
  ColumnDef,
  OnChangeFn,
  PaginationState,
} from '@tanstack/react-table'
import { useTranslation } from 'react-i18next'

import { DataTablePage, useDataTable } from '@/components/data-table'

import type { PromotionPage } from '../types'

export function PromotionTable<T>(props: {
  columns: ColumnDef<T>[]
  page: PromotionPage<T> | undefined
  pagination: PaginationState
  onPaginationChange: OnChangeFn<PaginationState>
  loading: boolean
  fetching: boolean
}) {
  const { t } = useTranslation()
  const { table } = useDataTable({
    columns: props.columns,
    data: props.page?.items ?? [],
    totalCount: props.page?.total ?? 0,
    pagination: props.pagination,
    onPaginationChange: (updater) => {
      if (!props.fetching) props.onPaginationChange(updater)
    },
    manualPagination: true,
    manualFiltering: true,
    enableSorting: false,
    enableRowSelection: false,
  })
  return (
    <DataTablePage
      table={table}
      columns={props.columns}
      toolbarProps={null}
      isLoading={props.loading}
      isFetching={props.fetching}
      emptyTitle={t('No records')}
      fixedHeight={false}
      paginationInFooter={false}
    />
  )
}
