import request from '@/utils/request'
import type { ConflictCheck } from '@/types'

export interface ConflictSubmitPayload {
  case_key: string
  case_title: string
  our_parties: { name: string; id_number?: string; contact?: string; party_role?: string }[]
  opp_name: string
  opp_id_number?: string
}

// 提交新案冲突检查（同一对方身份重复提交收口为唯一终态）。
export function submitConflict(data: ConflictSubmitPayload) {
  return request.post('/conflict-checks', data)
}

// 分页查询（可按 status 筛选）。
export function listConflicts(params: Record<string, unknown>) {
  return request.get('/conflict-checks', { params })
}

// 按记录 ID 读回唯一结论（读回时做实时版本复核）。
export function getConflict(id: number) {
  return request.get(`/conflict-checks/id/${id}`)
}

// 从同一入口按新案编号（精确）或对方姓名/证件号或检查单号读回结论。
export function lookupConflict(params: {
  case_key?: string
  opp_name?: string
  opp_id_number?: string
  check_no?: string
}) {
  return request.get('/conflict-checks/lookup', { params })
}

// 管理员填写依据后放行。
export function releaseConflict(id: number, basis: string) {
  return request.post(`/conflict-checks/id/${id}/release`, { basis })
}

// 管理员填写依据后驳回。
export function rejectConflict(id: number, basis: string) {
  return request.post(`/conflict-checks/id/${id}/reject`, { basis })
}

// 案件当事人档案登记。
export function addCaseParty(caseId: number, data: Record<string, unknown>) {
  return request.post(`/conflict-checks/cases/${caseId}/parties`, data)
}

export function listCaseParties(caseId: number) {
  return request.get(`/conflict-checks/cases/${caseId}/parties`)
}

export type { ConflictCheck }
