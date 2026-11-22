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
import { act, cleanup, render, screen } from '@testing-library/react'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { afterEach, expect, it } from 'vitest'

import en from '@/i18n/locales/en.json'
import fr from '@/i18n/locales/fr.json'
import ja from '@/i18n/locales/ja.json'
import ru from '@/i18n/locales/ru.json'
import vi from '@/i18n/locales/vi.json'
import zhTW from '@/i18n/locales/zh-TW.json'
import zh from '@/i18n/locales/zh.json'

import { PromotionAmount } from '../components/promotion-amount'
import { promotionFormSchema, promotionRateBps } from '../lib/admin-form'

const resources = { en, fr, ja, ru, vi, zhCN: zh, zhTW }

afterEach(cleanup)

it('percent input accepts two decimal places without rounding invalid rates into valid ones', () => {
  expect(promotionRateBps('0')).toBe(0)
  expect(promotionRateBps('12.34')).toBe(1234)
  expect(promotionRateBps('100.00')).toBe(10000)
  for (const value of ['100.01', '-1', '1.234', '1e2', 'NaN', 'Infinity']) {
    expect(promotionRateBps(value)).toBeNull()
  }
})

it('adjustment validation requires nonzero exact decimals, a public reason, evidence and an idempotency key', () => {
  const values = {
    user_id: '18',
    rate: '',
    commission_quota: '-0.0001',
    reason: 'Correction',
    evidence: 'Investigation note',
    idempotency_key: 'fixed-adjustment-key',
  }
  expect(promotionFormSchema('adjust').safeParse(values).success).toBe(true)
  for (const patch of [
    { commission_quota: '0' },
    { commission_quota: '1e3' },
    { evidence: '' },
    { reason: ' ' },
    { idempotency_key: '' },
  ]) {
    expect(
      promotionFormSchema('adjust').safeParse({ ...values, ...patch }).success
    ).toBe(false)
  }
})

it.each(['en', 'zhCN', 'zhTW', 'fr', 'ja', 'ru', 'vi', 'invalid-locale-code'])(
  'amount display works safely with %s interface language',
  async (language) => {
    const i18n = createInstance()
    await i18n.init({ lng: language, fallbackLng: 'en', resources })
    render(
      <I18nextProvider i18n={i18n}>
        <PromotionAmount value='500000' />
      </I18nextProvider>
    )
    expect(screen.getByTitle('500000')).toHaveTextContent(/1/)
  }
)

it('oversized quota retains exact text and reacts to language changes instead of rounding through float', async () => {
  const i18n = createInstance()
  await i18n.init({ lng: 'en', fallbackLng: 'en', resources })
  render(
    <I18nextProvider i18n={i18n}>
      <PromotionAmount value='9007199254740993.125' />
    </I18nextProvider>
  )
  expect(screen.getByTitle('9007199254740993.125')).toHaveTextContent(
    '9007199254740993.125 quota units (exact)'
  )
  await act(async () => {
    await i18n.changeLanguage('zhTW')
  })
  expect(screen.getByTitle('9007199254740993.125')).toHaveTextContent(
    '9007199254740993.125 額度單位（精確值）'
  )
})
