import { create } from 'zustand'
import { listConflicts } from '@/api/conflict'
import type { ConflictCheck } from '@/types'

interface ConflictState {
  list: ConflictCheck[]
  total: number
  fetchList: (params?: Record<string, unknown>) => Promise<void>
}

export const useConflictStore = create<ConflictState>((set) => ({
  list: [],
  total: 0,
  async fetchList(params = {}) {
    const res: any = await listConflicts(params)
    set({ list: res.data.list, total: res.data.total })
  },
}))
