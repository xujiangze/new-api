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
import { create } from 'zustand'
import { createJSONStorage, persist } from 'zustand/middleware'

export const ANNOUNCEMENT_POPUP_STORAGE_KEY = 'announcement-popup-storage'

type ConfirmedVersions = Record<string, string[]>

interface AnnouncementPopupState {
  confirmedVersions: ConfirmedVersions
  confirm: (accountId: number, versions: string[]) => void
}

export const useAnnouncementPopupStore = create<AnnouncementPopupState>()(
  persist(
    (set) => ({
      confirmedVersions: {},
      confirm: (accountId, versions) => {
        set((state) => ({
          confirmedVersions: {
            ...state.confirmedVersions,
            [accountId]: [
              ...new Set([
                ...(state.confirmedVersions[accountId] ?? []),
                ...versions,
              ]),
            ],
          },
        }))
      },
    }),
    {
      name: ANNOUNCEMENT_POPUP_STORAGE_KEY,
      partialize: (state) => ({ confirmedVersions: state.confirmedVersions }),
      storage: createJSONStorage(() => ({
        getItem: (key) => {
          try {
            return window.localStorage.getItem(key)
          } catch {
            return null
          }
        },
        setItem: (key, value) => {
          try {
            window.localStorage.setItem(key, value)
          } catch {
            // Keep the in-memory confirmation when browser storage is unavailable.
          }
        },
        removeItem: (key) => {
          try {
            window.localStorage.removeItem(key)
          } catch {
            // Storage availability must never prevent the console from opening.
          }
        },
      })),
      merge: (persisted, current) => {
        const saved = persisted as Partial<AnnouncementPopupState> | undefined
        const confirmedVersions: ConfirmedVersions = {}
        if (
          saved?.confirmedVersions &&
          typeof saved.confirmedVersions === 'object'
        ) {
          for (const [accountId, versions] of Object.entries(
            saved.confirmedVersions
          )) {
            if (/^[1-9]\d*$/.test(accountId) && Array.isArray(versions)) {
              confirmedVersions[accountId] = versions.filter(
                (version): version is string =>
                  typeof version === 'string' &&
                  /^v1-[0-9a-f]{16}$/.test(version)
              )
            }
          }
        }
        return { ...current, confirmedVersions }
      },
    }
  )
)
