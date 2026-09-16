package dto

// OurPartyDTO 提交新案时录入的本方（客户/联系人）。
type OurPartyDTO struct {
	Name      string `json:"name" binding:"required,max=100"`
	IDNumber  string `json:"id_number" binding:"omitempty,max=50"`
	Contact   string `json:"contact" binding:"omitempty,max=50"`
	PartyRole string `json:"party_role" binding:"omitempty,max=50"`
}

// ConflictSubmitRequest 新案案源利益冲突提交。
type ConflictSubmitRequest struct {
	// CaseKey 新案业务幂等键（新案编号）：同一新案重复提交收口到一条；不同新案各自独立。
	CaseKey     string        `json:"case_key" binding:"required,max=100"`
	CaseTitle   string        `json:"case_title" binding:"required,max=200"`
	OurParties  []OurPartyDTO `json:"our_parties" binding:"required,min=1,dive"`
	OppName     string        `json:"opp_name" binding:"required,max=100"`
	OppIDNumber string        `json:"opp_id_number" binding:"omitempty,max=50"`
}

// ConflictDecisionRequest 管理员复核：放行或驳回，必须填写依据。
type ConflictDecisionRequest struct {
	Basis string `json:"basis" binding:"required,min=2,max=1000"`
}

// ConflictRecheckQuery 读回结论（同一入口）：优先 case_key 精确读回该案。
type ConflictRecheckQuery struct {
	CaseKey     string `form:"case_key" binding:"omitempty,max=100"`
	OppName     string `form:"opp_name" binding:"omitempty,max=100"`
	OppIDNumber string `form:"opp_id_number" binding:"omitempty,max=50"`
	CheckNo     string `form:"check_no" binding:"omitempty,max=50"`
}
