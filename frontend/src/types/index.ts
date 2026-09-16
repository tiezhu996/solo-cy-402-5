export interface User {
  id: number
  username: string
  real_name: string
  role: string
  license_no: string
  email: string
  phone: string
  avatar: string
  created_at: string
}

export interface Client {
  id: number
  name: string
  id_number: string
  contact: string
  address: string
  remark: string
  created_at: string
}

export interface CaseItem {
  id: number
  case_no: string
  title: string
  case_type: string
  status: string
  accept_date: string | null
  close_date: string | null
  summary: string
  client_id: number
  lead_lawyer_id: number
  co_lawyer_ids: number[]
  created_at: string
}

export interface DocumentItem {
  id: number
  title: string
  file_type: string
  file_url: string
  upload_time: string
  case_id: number
  uploader_id: number
  created_at: string
}

export interface Billing {
  id: number
  bill_no: string
  billing_type: string
  amount: number | string
  status: string
  case_id: number
  client_id: number
  invoice_info: string
  created_at: string
}

export interface AuditLog {
  id: number
  operator_id: number
  operator_name: string
  action: string
  entity_type: string
  entity_id: string
  detail: string
  ip: string
  created_at: string
}

export interface PageResult<T> {
  list: T[]
  total: number
  page: number
  page_size: number
}

export interface OurParty {
  name: string
  id_number?: string
  contact?: string
  party_role?: string
}

export interface PartySnapshot {
  party_id: number
  case_id: number
  case_no: string
  case_title: string
  case_status: string
  side: string
  name: string
  id_number: string
  contact: string
  party_role: string
  version: number
  fingerprint: string
  matched_by: string
}

export interface ConflictCheck {
  id: number
  check_no: string
  case_title: string
  our_parties: OurParty[]
  opp_name: string
  opp_id_number: string
  identity_key: string
  norm_opp_name: string
  norm_opp_id: string
  status: string
  hit_count: number
  matched_snapshot: PartySnapshot[]
  bound_party_version: number
  bound_fingerprint: string
  review_by_id: number
  review_by_name: string
  review_basis: string
  reviewed_at: string | null
  invalidated_reason: string
  submit_by_id: number
  submit_by_name: string
  created_at: string
  updated_at: string
  live_match: boolean
  live_snapshot?: PartySnapshot[]
  can_proceed: boolean
  stale: boolean
  has_new_hit: boolean
}

export interface CaseParty {
  id: number
  case_id: number
  side: string
  name: string
  id_number: string
  contact: string
  party_role: string
  norm_name: string
  norm_id: string
  version: number
  created_at: string
  updated_at: string
}
