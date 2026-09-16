package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"cylawcase/internal/constants"
	"cylawcase/internal/dto"
	"cylawcase/internal/middleware"
	"cylawcase/internal/repository"
	"cylawcase/internal/service"
	"cylawcase/internal/util"

	"github.com/gin-gonic/gin"
)

// ConflictHandler 案源利益冲突检查 HTTP 处理器。
type ConflictHandler struct {
	svc      *service.ConflictService
	partySvc *service.CasePartyService
	logger   *slog.Logger
}

// NewConflictHandler 构造冲突检查处理器。
func NewConflictHandler(svc *service.ConflictService, partySvc *service.CasePartyService, logger *slog.Logger) *ConflictHandler {
	return &ConflictHandler{svc: svc, partySvc: partySvc, logger: logger}
}

// Submit 提交新案冲突检查（同一对方身份重复提交收口为唯一终态）。
func (h *ConflictHandler) Submit(c *gin.Context) {
	var req dto.ConflictSubmitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, constants.CodeBadRequest, "ConflictCheck submit: "+err.Error())
		return
	}
	chk, err := h.svc.Submit(req, middleware.GetUserID(c), middleware.GetUsername(c))
	if err != nil {
		h.wrapError(c, err, "ConflictCheck[opp_name="+req.OppName+"] submit failed")
		return
	}
	OKWithMessage(c, conflictSubmitMessage(chk.Status), chk)
}

// conflictSubmitMessage 按收口后的状态给出提示文案。
func conflictSubmitMessage(status string) string {
	switch status {
	case constants.ConflictStatusPendingReview:
		return constants.MsgConflictHit
	case constants.ConflictStatusReleased:
		return constants.MsgConflictReleased
	case constants.ConflictStatusRejected:
		return constants.MsgConflictRejected
	case constants.ConflictStatusInvalidated:
		return constants.MsgConflictStale
	default:
		return constants.MsgConflictNoHit
	}
}

// Get 按 ID 读回唯一结论（含实时版本复核）。
func (h *ConflictHandler) Get(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Fail(c, http.StatusBadRequest, constants.CodeBadRequest, "ConflictCheck[id] get: invalid id")
		return
	}
	chk, err := h.svc.Get(id)
	if err != nil {
		h.wrapError(c, err, "ConflictCheck get failed")
		return
	}
	OK(c, chk)
}

// Read 从同一入口按对方姓名/证件号或检查单号读回唯一结论。
func (h *ConflictHandler) Read(c *gin.Context) {
	var q dto.ConflictRecheckQuery
	_ = c.ShouldBindQuery(&q)
	chk, err := h.svc.ReadConclusion(q.CaseKey, q.OppName, q.OppIDNumber, q.CheckNo)
	if err != nil {
		h.wrapError(c, err, "ConflictCheck read failed")
		return
	}
	OK(c, chk)
}

// List 分页查询（管理员默认看待复核）。
func (h *ConflictHandler) List(c *gin.Context) {
	var q dto.PageQuery
	_ = c.ShouldBindQuery(&q)
	q.Normalize()
	list, total, err := h.svc.List(q.Page, q.PageSize, c.Query("status"))
	if err != nil {
		h.wrapError(c, err, "ConflictCheck list failed")
		return
	}
	OK(c, pageResponse(list, total, q.Page, q.PageSize))
}

// Release 管理员填写依据后放行。
func (h *ConflictHandler) Release(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Fail(c, http.StatusBadRequest, constants.CodeBadRequest, "ConflictCheck[id] release: invalid id")
		return
	}
	var req dto.ConflictDecisionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, constants.CodeConflictBasisRequired, "ConflictCheck release: "+err.Error())
		return
	}
	chk, err := h.svc.Release(id, req.Basis, middleware.GetUserID(c), middleware.GetUsername(c))
	if err != nil {
		h.wrapError(c, err, "ConflictCheck release failed")
		return
	}
	OKWithMessage(c, constants.MsgConflictReleased, chk)
}

// Reject 管理员填写依据后驳回。
func (h *ConflictHandler) Reject(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Fail(c, http.StatusBadRequest, constants.CodeBadRequest, "ConflictCheck[id] reject: invalid id")
		return
	}
	var req dto.ConflictDecisionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, constants.CodeConflictBasisRequired, "ConflictCheck reject: "+err.Error())
		return
	}
	chk, err := h.svc.Reject(id, req.Basis, middleware.GetUserID(c), middleware.GetUsername(c))
	if err != nil {
		h.wrapError(c, err, "ConflictCheck reject failed")
		return
	}
	OKWithMessage(c, constants.MsgConflictRejected, chk)
}

// AddParty 为案件登记当事人档案。
func (h *ConflictHandler) AddParty(c *gin.Context) {
	caseID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Fail(c, http.StatusBadRequest, constants.CodeBadRequest, "CaseParty[case_id] add: invalid case id")
		return
	}
	var req struct {
		Side      string `json:"side" binding:"required,oneof=our opposing"`
		Name      string `json:"name" binding:"required"`
		IDNumber  string `json:"id_number"`
		Contact   string `json:"contact"`
		PartyRole string `json:"party_role"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, constants.CodeBadRequest, "CaseParty add: "+err.Error())
		return
	}
	p, err := h.partySvc.Add(caseID, req.Side, req.Name, req.IDNumber, req.Contact, req.PartyRole)
	if err != nil {
		h.wrapError(c, err, "CaseParty add failed")
		return
	}
	OK(c, p)
}

// UpdateParty 更新当事人档案（身份字段变更会令旧放行失效）。
func (h *ConflictHandler) UpdateParty(c *gin.Context) {
	partyID, err := strconv.ParseUint(c.Param("partyId"), 10, 64)
	if err != nil {
		Fail(c, http.StatusBadRequest, constants.CodeBadRequest, "CaseParty[id] update: invalid party id")
		return
	}
	var req struct {
		Name      string `json:"name"`
		IDNumber  string `json:"id_number"`
		Contact   string `json:"contact"`
		PartyRole string `json:"party_role"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, constants.CodeBadRequest, "CaseParty update: "+err.Error())
		return
	}
	p, err := h.partySvc.Update(partyID, req.Name, req.IDNumber, req.Contact, req.PartyRole)
	if err != nil {
		h.wrapError(c, err, "CaseParty update failed")
		return
	}
	OK(c, p)
}

// ListParties 查询案件当事人档案。
func (h *ConflictHandler) ListParties(c *gin.Context) {
	caseID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Fail(c, http.StatusBadRequest, constants.CodeBadRequest, "CaseParty[case_id] list: invalid case id")
		return
	}
	list, err := h.partySvc.ListByCase(caseID)
	if err != nil {
		h.wrapError(c, err, "CaseParty list failed")
		return
	}
	OK(c, list)
}

func (h *ConflictHandler) wrapError(c *gin.Context, err error, ctx string) {
	var appErr *util.AppError
	if errors.As(err, &appErr) {
		c.Set("audit_detail", appErr.Message)
		h.logger.Warn("conflict handler error", "context", ctx, "error", appErr.Error())
		Fail(c, appErrorStatus(appErr.Code), appErr.Code, appErr.Message)
		return
	}
	if errors.Is(err, repository.ErrNotFound) {
		Fail(c, http.StatusNotFound, constants.CodeNotFound, constants.MsgNotFound)
		return
	}
	h.logger.Error("conflict handler error", "context", ctx, "error", err.Error())
	Fail(c, http.StatusInternalServerError, constants.CodeInternalError, constants.MsgInternalError)
}
