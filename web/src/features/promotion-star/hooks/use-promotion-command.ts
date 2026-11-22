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
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  useSecureVerification,
  type VerificationOperation,
} from '@/features/auth/secure-verification'
import { handleServerError } from '@/lib/handle-server-error'
import { AuthOperationError } from '@/lib/secure-verification'
import { getServerErrorStatus } from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'

import { executePromotionCommand } from '../api'
import { canViewPromotion } from '../lib/access'
import type { PromotionCommand } from '../types'
import { usePromotionAccess } from './use-promotion-query'

export function usePromotionCommand() {
  const { t } = useTranslation()
  const access = usePromotionAccess()
  const client = useQueryClient()
  const verification = useSecureVerification()
  const cancelVerification = verification.cancel
  const operation = useRef<AbortController | null>(null)
  const [busy, setBusy] = useState(false)
  const mutation = useMutation({
    mutationFn: (input: {
      command: PromotionCommand
      proof: string
      signal: AbortSignal
    }) => executePromotionCommand(input.command, input.proof, input.signal),
    retry: false,
    gcTime: 0,
    meta: { errorToast: false },
  })

  useEffect(
    () => () => {
      operation.current?.abort()
      operation.current = null
      cancelVerification()
    },
    [cancelVerification]
  )

  const submit = async (command: PromotionCommand): Promise<boolean> => {
    const user = useAuthStore.getState().auth.user
    if (
      operation.current ||
      user?.id !== access.actorId ||
      !canViewPromotion(user, 'admin')
    ) {
      return false
    }
    const current = new AbortController()
    operation.current = current
    setBusy(true)
    let request: VerificationOperation
    if (command.action === 'review') {
      request = {
        scope: 'admin.promotion_star.review',
        context: { event_key: command.event_key, ...command.payload },
      }
    } else if (command.action === 'adjust') {
      request = {
        scope: 'admin.promotion_star.adjust',
        context: { user_id: command.user_id, ...command.payload },
      }
    } else if (command.action === 'suspend') {
      request = {
        scope: 'admin.promotion_star.suspend',
        context: { user_id: command.user_id, ...command.payload },
      }
    } else {
      request = {
        scope:
          command.action === 'grant'
            ? 'admin.promotion_star.grant'
            : 'admin.promotion_star.rate',
        context: { user_id: command.user_id, ...command.payload },
      }
    }
    try {
      const proof = await verification.requestVerification({
        ...request,
        title: t('Verify Promotion Star change'),
        description: t(
          'Confirm the exact change for user {{id}}. This does not transfer wallet funds.',
          { id: command.user_id }
        ),
      })
      const actor = useAuthStore.getState().auth.user
      if (
        !proof ||
        operation.current !== current ||
        current.signal.aborted ||
        actor?.id !== access.actorId ||
        !canViewPromotion(actor, 'admin')
      ) {
        return false
      }
      await mutation.mutateAsync({
        command,
        proof: proof.proof_token,
        signal: current.signal,
      })
      if (operation.current !== current || current.signal.aborted) return false
      await client.invalidateQueries({
        queryKey: ['promotion-star', access.actorId],
      })
      if (operation.current !== current) return false
      toast.success(t('Promotion Star change saved'))
      return true
    } catch (error) {
      const authError = AuthOperationError.from(error)
      const revoked = [
        'PROMOTION_STAR_FORBIDDEN',
        'AUTH_SESSION_REQUIRED',
        'AUTH_SESSION_INVALID',
        'AUTH_SESSION_REVOKED',
        'SECURITY_ACTION_FORBIDDEN',
      ].includes(authError.code ?? '')
      if (getServerErrorStatus(error) === 403 && revoked) access.deny()
      if (operation.current === current && !current.signal.aborted) {
        handleServerError(authError)
      }
      return false
    } finally {
      // Proofs and private evidence must not remain in a completed mutation cache.
      mutation.reset()
      if (operation.current === current) {
        operation.current = null
        setBusy(false)
      }
    }
  }
  return { submit, busy, verification }
}
