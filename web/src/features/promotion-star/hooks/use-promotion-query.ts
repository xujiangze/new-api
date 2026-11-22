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
import { useQuery } from '@tanstack/react-query'
import { createContext, useContext } from 'react'

import { getServerErrorStatus } from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'

import { canViewPromotion } from '../lib/access'
import type { PromotionScope } from '../types'

export interface PromotionAccessContext {
  actorId: number
  scope: PromotionScope
  deny: () => void
}

export const AccessContext = createContext<PromotionAccessContext | null>(null)

export function usePromotionAccess(): PromotionAccessContext {
  const access = useContext(AccessContext)
  if (!access) {
    throw new Error('Promotion queries require a capability boundary')
  }
  return access
}

export function usePromotionQuery<T>(
  key: readonly unknown[],
  query: (signal: AbortSignal) => Promise<T>,
  enabled = true
) {
  const access = usePromotionAccess()
  return useQuery({
    queryKey: ['promotion-star', access.actorId, access.scope, ...key],
    queryFn: async ({ signal }) => {
      const user = useAuthStore.getState().auth.user
      if (
        user?.id !== access.actorId ||
        !canViewPromotion(user, access.scope)
      ) {
        throw new DOMException('Permission changed', 'AbortError')
      }
      try {
        const data = await query(signal)
        const current = useAuthStore.getState().auth.user
        if (
          signal.aborted ||
          current?.id !== access.actorId ||
          !canViewPromotion(current, access.scope)
        ) {
          throw new DOMException('Permission changed', 'AbortError')
        }
        return data
      } catch (error) {
        if (getServerErrorStatus(error) === 403) access.deny()
        throw error
      }
    },
    enabled,
    retry: false,
    gcTime: 0,
    refetchOnWindowFocus: 'always',
  })
}
