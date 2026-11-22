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
import { useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { ErrorState } from '@/components/error-state'
import { useAuthStore } from '@/stores/auth-store'

import {
  AccessContext,
  type PromotionAccessContext,
} from '../hooks/use-promotion-query'
import { canViewPromotion } from '../lib/access'
import type { PromotionScope } from '../types'

function ActorBoundary(
  props: Omit<PromotionAccessContext, 'deny'> & { children: ReactNode }
) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const user = useAuthStore((state) => state.auth.user)
  const [denied, setDenied] = useState(false)
  const actorId = props.actorId
  const scope = props.scope
  const deny = useCallback(() => {
    setDenied(true)
    const auth = useAuthStore.getState().auth
    if (scope === 'self' && auth.user?.id === actorId) {
      auth.setUser({ ...auth.user, promotion_star_enabled: false })
    }
    void client.cancelQueries({ queryKey: ['promotion-star', actorId, scope] })
    client.removeQueries({ queryKey: ['promotion-star', actorId, scope] })
  }, [actorId, client, scope])

  useEffect(() => {
    const clear = () => {
      void client.cancelQueries({
        queryKey: ['promotion-star', actorId, scope],
      })
      client.removeQueries({ queryKey: ['promotion-star', actorId, scope] })
    }
    const unsubscribe = useAuthStore.subscribe((state) => {
      if (
        state.auth.user?.id !== actorId ||
        !canViewPromotion(state.auth.user, scope)
      ) {
        clear()
      }
    })
    return () => {
      unsubscribe()
      clear()
    }
  }, [actorId, client, scope])

  if (denied || user?.id !== actorId || !canViewPromotion(user, scope)) {
    return (
      <ErrorState
        title={t('Promotion Star access is unavailable')}
        description={t(
          'Your permission may have changed. Refresh your account before trying again.'
        )}
      />
    )
  }

  return (
    <AccessContext value={{ actorId, scope, deny }}>
      {props.children}
    </AccessContext>
  )
}

export function PromotionAccess(props: {
  scope: PromotionScope
  children: ReactNode
}) {
  const { t } = useTranslation()
  const actorId = useAuthStore((state) => state.auth.user?.id)
  if (actorId === undefined) {
    return <ErrorState title={t('Promotion Star access is unavailable')} />
  }
  return (
    <ActorBoundary
      key={`${actorId}:${props.scope}`}
      actorId={actorId}
      scope={props.scope}
    >
      {props.children}
    </ActorBoundary>
  )
}
