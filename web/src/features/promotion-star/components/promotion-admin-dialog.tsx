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
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { SecureVerificationDialog } from '@/features/auth/secure-verification'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

import { usePromotionCommand } from '../hooks/use-promotion-command'
import {
  promotionCommand,
  promotionFormSchema,
  type PromotionFormValues,
} from '../lib/admin-form'
import type {
  PromotionAction,
  PromotionCommand,
  PromotionProfile,
} from '../types'
import { PromotionAmount } from './promotion-amount'

export function PromotionAdminDialog(props: {
  action: PromotionAction
  profile?: PromotionProfile
  onClose: () => void
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const command = usePromotionCommand()
  const [confirmation, setConfirmation] = useState<PromotionCommand | null>(
    null
  )
  const titles = {
    grant: t('Grant Promotion Star'),
    rate: t('Change commission rate'),
    suspend: t('Suspend Promotion Star'),
    adjust: t('Append commission adjustment'),
  }
  const title = titles[props.action]
  const form = useForm<PromotionFormValues>({
    resolver: zodResolver(promotionFormSchema(props.action)),
    defaultValues: {
      user_id: props.profile ? String(props.profile.user_id) : '',
      rate: props.profile ? String(props.profile.rate_bps / 100) : '',
      commission_quota: '',
      reason: '',
      evidence: '',
      idempotency_key: crypto.randomUUID(),
    },
  })
  const fields: {
    name: keyof PromotionFormValues
    label: string
    textarea?: boolean
    disabled?: boolean
    maxLength?: number
  }[] = [{ name: 'user_id', label: t('User ID'), disabled: !!props.profile }]
  if (props.action === 'grant' || props.action === 'rate') {
    fields.push({ name: 'rate', label: t('Commission rate (%)') })
  }
  if (props.action === 'adjust') {
    fields.push({
      name: 'commission_quota',
      label: t('Signed commission amount (quota units)'),
    })
  }
  fields.push({
    name: 'reason',
    label: t('Public reason'),
    textarea: true,
    maxLength: 500,
  })
  if (props.action === 'adjust') {
    fields.push(
      {
        name: 'evidence',
        label: t('Private evidence'),
        textarea: true,
        maxLength: 2000,
      },
      { name: 'idempotency_key', label: t('Idempotency key'), maxLength: 128 }
    )
  }
  return (
    <>
      <Dialog
        open={!confirmation}
        onOpenChange={(open) => {
          if (!open && !confirmation && !command.busy) props.onClose()
        }}
        title={title}
        description={t('Commission ledger only; no wallet payouts')}
        footer={
          <>
            <Button
              variant='outline'
              onClick={props.onClose}
              disabled={command.busy}
            >
              {t('Cancel')}
            </Button>
            <Button
              type='submit'
              form='promotion-admin-form'
              disabled={command.busy}
            >
              {t('Review change')}
            </Button>
          </>
        }
      >
        <Form {...form}>
          <form
            id='promotion-admin-form'
            onSubmit={form.handleSubmit((values) =>
              setConfirmation(promotionCommand(props.action, values))
            )}
            className='space-y-4'
          >
            {fields.map((item) => (
              <FormField
                key={item.name}
                control={form.control}
                name={item.name}
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{item.label}</FormLabel>
                    <FormControl>
                      {item.textarea ? (
                        <Textarea
                          {...field}
                          maxLength={item.maxLength}
                          rows={3}
                        />
                      ) : (
                        <Input
                          {...field}
                          readOnly={item.disabled}
                          maxLength={item.maxLength}
                          inputMode={
                            item.name === 'user_id' ? 'numeric' : 'text'
                          }
                        />
                      )}
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            ))}
            {props.action === 'adjust' && (
              <p className='text-muted-foreground text-sm'>
                {t(
                  'Use a positive amount to add commission or a negative amount to deduct it. Keep the same idempotency key when retrying the same adjustment. Private evidence is visible only to administrators.'
                )}
              </p>
            )}
          </form>
        </Form>
      </Dialog>
      <ConfirmDialog
        open={!!confirmation && !command.verification.isActive}
        onOpenChange={(open) => {
          if (!open && !command.busy) setConfirmation(null)
        }}
        title={title}
        desc={t(
          'Review the target, amount and public reason. This appends a new record and does not change the wallet balance.'
        )}
        confirmText={t('Verify and save')}
        destructive={props.action === 'suspend'}
        isLoading={command.busy}
        handleConfirm={() => {
          if (confirmation) {
            void command.submit(confirmation).then((saved) => {
              if (saved) props.onClose()
            })
          }
        }}
      >
        {confirmation && (
          <dl className='space-y-2 text-sm break-words'>
            <div>
              <dt className='text-muted-foreground'>{t('User ID')}</dt>
              <dd>{confirmation.user_id}</dd>
            </div>
            {'rate_bps' in confirmation.payload && (
              <div>
                <dt className='text-muted-foreground'>
                  {t('Commission rate (%)')}
                </dt>
                <dd>
                  {formatNumber(confirmation.payload.rate_bps / 100, locale)}%
                </dd>
              </div>
            )}
            {confirmation.action === 'adjust' && (
              <>
                <div>
                  <dt className='text-muted-foreground'>{t('Commission')}</dt>
                  <dd>
                    <PromotionAmount
                      value={confirmation.payload.commission_quota}
                    />
                  </dd>
                  <dd>
                    {confirmation.payload.commission_quota} {t('Quota units')}
                  </dd>
                </div>
                <div>
                  <dt className='text-muted-foreground'>
                    {t('Idempotency key')}
                  </dt>
                  <dd>{confirmation.payload.idempotency_key}</dd>
                </div>
              </>
            )}
            <div>
              <dt className='text-muted-foreground'>{t('Public reason')}</dt>
              <dd>{confirmation.payload.reason}</dd>
            </div>
          </dl>
        )}
      </ConfirmDialog>
      <SecureVerificationDialog {...command.verification.dialogProps} />
    </>
  )
}
