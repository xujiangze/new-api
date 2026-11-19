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
import { RotateCcw, Search, X } from 'lucide-react'
import { useEffect, useMemo, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { MultiSelect, type Option } from '@/components/multi-select'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  computeCacheHitRatePercent,
  formatDashboardHitRate,
  getRowCacheHitMetrics,
} from '@/features/dashboard/lib'
import type { QuotaDataItem } from '@/features/dashboard/types'
import { toIntlLocale } from '@/i18n/languages'

interface ModelsQuickFilterBarProps {
  rawData: QuotaDataItem[]
  selectedModels: string[]
  onSelectedModelsChange: (models: string[]) => void
  isAdmin: boolean
  username: string
  onUsernameSubmit: (username: string) => void
  onClearQuickFilters: () => void
}

export function ModelsQuickFilterBar(props: ModelsQuickFilterBarProps) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const [usernameDraft, setUsernameDraft] = useState(props.username)

  useEffect(() => {
    setUsernameDraft(props.username)
  }, [props.username])

  const modelOptions: Option[] = useMemo(() => {
    const totals = new Map<
      string,
      { inputTokens: number; hitReadTokens: number; count: number }
    >()
    for (const item of props.rawData) {
      const model = item.model_name || 'Unknown'
      const cache = getRowCacheHitMetrics(item)
      const prev = totals.get(model) ?? {
        inputTokens: 0,
        hitReadTokens: 0,
        count: 0,
      }
      totals.set(model, {
        inputTokens: prev.inputTokens + cache.inputTokens,
        hitReadTokens: prev.hitReadTokens + cache.hitReadTokens,
        count: prev.count + (Number(item.count) || 0),
      })
    }

    return [...totals.entries()]
      .sort((a, b) => {
        if (b[1].inputTokens !== a[1].inputTokens) {
          return b[1].inputTokens - a[1].inputTokens
        }
        if (b[1].count !== a[1].count) {
          return b[1].count - a[1].count
        }
        return a[0].localeCompare(b[0])
      })
      .map(([model, agg]) => ({
        value: model,
        label: model,
        hint: formatDashboardHitRate(
          computeCacheHitRatePercent(agg.hitReadTokens, agg.inputTokens),
          locale
        ),
      }))
  }, [props.rawData, locale])

  const handleUsernameFormSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    props.onUsernameSubmit(usernameDraft.trim())
  }

  const handleClearUsername = () => {
    setUsernameDraft('')
    if (props.username) {
      props.onUsernameSubmit('')
    }
  }

  const hasActiveFilter =
    props.selectedModels.length > 0 || Boolean(props.username.trim())

  return (
    <div className='flex flex-col gap-2 sm:flex-row sm:flex-wrap sm:items-center'>
      <div className='min-w-0 flex-1 sm:max-w-md'>
        <MultiSelect
          options={modelOptions}
          selected={props.selectedModels}
          onChange={props.onSelectedModelsChange}
          placeholder={t('Filter by models')}
          emptyText={t('No models found')}
          maxVisibleChips={2}
        />
      </div>

      {props.isAdmin && (
        <form
          onSubmit={handleUsernameFormSubmit}
          className='flex min-w-0 items-center gap-1.5 sm:w-auto'
        >
          <div className='relative min-w-0 flex-1 sm:w-52'>
            <Input
              aria-label={t('Username')}
              placeholder={t('Filter by username')}
              value={usernameDraft}
              onChange={(event) => setUsernameDraft(event.target.value)}
              className='h-9 pr-7 text-sm'
            />
            {usernameDraft && (
              <button
                type='button'
                onClick={handleClearUsername}
                aria-label={t('Clear username filter')}
                className='text-muted-foreground hover:text-foreground absolute top-1/2 right-2 -translate-y-1/2 rounded-sm p-0.5'
              >
                <X className='size-3.5' aria-hidden='true' />
              </button>
            )}
          </div>
          <Button type='submit' variant='outline' size='sm' className='h-9'>
            <Search className='size-3.5 sm:mr-1.5' aria-hidden='true' />
            <span className='hidden sm:inline'>{t('Search')}</span>
          </Button>
        </form>
      )}

      {hasActiveFilter && (
        <Button
          type='button'
          variant='ghost'
          size='sm'
          onClick={props.onClearQuickFilters}
          className='h-9 self-start sm:self-auto'
        >
          <RotateCcw className='mr-1.5 size-3.5' aria-hidden='true' />
          {t('Clear filters')}
        </Button>
      )}
    </div>
  )
}
