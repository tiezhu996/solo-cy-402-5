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
	CaseTitle   string        `json:"case_title" binding:"required,max=200"`
	OurParties  []OurPartyDTO `json:"our_parties" binding:"required,min=1,dive"`
	OppName     string        `json:"opp_name" binding:"required,max=100"`
	OppIDNumber string        `json:"opp_id_number" binding:"omitempty,max=50"`
}

// ConflictDecisionRequest 管理员复核：放行或驳回，必须填写依据。
type ConflictDecisionRequest struct {
	Basis string `json:"basis" binding:"required,min=2,max=1000"`
}

// ConflictRecheckQuery 按对方姓名/证件号读回唯一结论（同一入口）。
type ConflictRecheckQuery struct {
	OppName     string `form:"opp_name" binding:"omitempty,max=100"`
	OppIDNumber string `form:"opp_id_number" binding:"omitempty,max=50"`
	CheckNo     string `form:"check_no" binding:"omitempty,max=50"`
}
