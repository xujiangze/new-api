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

import { DatePicker } from '@/components/date-picker'
import dayjs from '@/lib/dayjs'

import type { PromotionFilters } from '../types'

export function PromotionDates(props: {
  value: PromotionFilters
  onChange: (value: PromotionFilters) => void
}) {
  const { t } = useTranslation()
  return (
    <div className='flex flex-wrap items-end gap-3'>
      <fieldset className='space-y-1.5'>
        <legend className='text-sm font-medium'>{t('Start date')}</legend>
        <DatePicker
          selected={dayjs(props.value.start_date).toDate()}
          onSelect={(date) => {
            if (date) {
              props.onChange({
                ...props.value,
                start_date: dayjs(date).format('YYYY-MM-DD'),
              })
            }
          }}
        />
      </fieldset>
      <fieldset className='space-y-1.5'>
        <legend className='text-sm font-medium'>{t('End date')}</legend>
        <DatePicker
          selected={dayjs(props.value.end_date).toDate()}
          onSelect={(date) => {
            if (date) {
              props.onChange({
                ...props.value,
                end_date: dayjs(date).format('YYYY-MM-DD'),
              })
            }
          }}
        />
      </fieldset>
      <p className='text-muted-foreground pb-2 text-xs'>
        {t('Inclusive dates · Asia/Shanghai')}
      </p>
    </div>
  )
}
