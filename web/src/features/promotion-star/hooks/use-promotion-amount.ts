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

import { toIntlLocale } from '@/i18n/languages'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { useSystemConfigStore } from '@/stores/system-config-store'

import { promotionQuotaNumber } from '../lib/format'

export function usePromotionAmount() {
  const { t, i18n } = useTranslation()
  useSystemConfigStore((state) => state.config?.currency)
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  return (value: string): string => {
    const amount = promotionQuotaNumber(value)
    if (amount === null) {
      return t('{{amount}} quota units (exact)', { amount: value })
    }
    return formatQuotaWithCurrency(amount, {
      locale,
      abbreviate: false,
      digitsLarge: 4,
      digitsSmall: 6,
    })
  }
}
