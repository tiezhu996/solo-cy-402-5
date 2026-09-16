// 利益冲突检查状态/命中方式枚举（与后端 backend/internal/constants/conflict.go 保持一致）
export const ConflictStatus = {
  NO_CONFLICT: 'no_conflict',
  PENDING_REVIEW: 'pending_review',
  RELEASED: 'released',
  REJECTED: 'rejected',
  INVALIDATED: 'invalidated',
} as const

export const ConflictStatusText: Record<string, string> = {
  [ConflictStatus.NO_CONFLICT]: '无冲突·可办理',
  [ConflictStatus.PENDING_REVIEW]: '命中·待复核',
  [ConflictStatus.RELEASED]: '已放行',
  [ConflictStatus.REJECTED]: '已驳回',
  [ConflictStatus.INVALIDATED]: '已失效',
}

export const ConflictStatusColor: Record<string, string> = {
  [ConflictStatus.NO_CONFLICT]: 'green',
  [ConflictStatus.PENDING_REVIEW]: 'orange',
  [ConflictStatus.RELEASED]: 'blue',
  [ConflictStatus.REJECTED]: 'red',
  [ConflictStatus.INVALIDATED]: 'default',
}

export const ConflictStatusOptions = Object.entries(ConflictStatusText).map(([value, label]) => ({
  value,
  label,
}))

export const PartySideText: Record<string, string> = {
  our: '本方',
  opposing: '对方',
}

export const MatchedByText: Record<string, string> = {
  id_number: '证件号命中',
  name: '姓名命中',
}
