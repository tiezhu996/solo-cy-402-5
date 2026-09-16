package repository

import (
	"errors"
	"fmt"

	"cylawcase/internal/model"

	"gorm.io/gorm"
)

// CasePartyRepository 案件当事人档案仓储。
type CasePartyRepository struct {
	db *gorm.DB
}

// NewCasePartyRepository 构造当事人档案仓储。
func NewCasePartyRepository(db *gorm.DB) *CasePartyRepository {
	return &CasePartyRepository{db: db}
}

// WithTx 返回绑定到指定事务的仓储副本。
func (r *CasePartyRepository) WithTx(tx *gorm.DB) *CasePartyRepository {
	return &CasePartyRepository{db: tx}
}

// Create 新增当事人档案。
func (r *CasePartyRepository) Create(p *model.CaseParty) error {
	if err := r.db.Create(p).Error; err != nil {
		return fmt.Errorf("create case party: %w", err)
	}
	return nil
}

// FindByID 按 ID 查询当事人档案。
func (r *CasePartyRepository) FindByID(id uint64) (*model.CaseParty, error) {
	var p model.CaseParty
	if err := r.db.First(&p, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("find case party by id: %w", err)
	}
	return &p, nil
}

// ListByCase 查询某案件下的全部当事人。
func (r *CasePartyRepository) ListByCase(caseID uint64) ([]model.CaseParty, error) {
	var list []model.CaseParty
	if err := r.db.Where("case_id = ?", caseID).Order("side ASC, id ASC").Find(&list).Error; err != nil {
		return nil, fmt.Errorf("list case parties by case: %w", err)
	}
	return list, nil
}

// Save 保存当事人档案（全字段更新）。
func (r *CasePartyRepository) Save(p *model.CaseParty) error {
	if err := r.db.Save(p).Error; err != nil {
		return fmt.Errorf("save case party: %w", err)
	}
	return nil
}

// BumpVersion 原子地将档案版本号自增，用于实质字段变更后令旧放行失效。
func (r *CasePartyRepository) BumpVersion(id uint64) error {
	if err := r.db.Model(&model.CaseParty{}).Where("id = ?", id).
		UpdateColumn("version", gorm.Expr("version + 1")).Error; err != nil {
		return fmt.Errorf("bump case party version: %w", err)
	}
	return nil
}

// FindCandidatesByPartyIDs 按档案 ID 联表查询现状（含已结案件），用于读回时区分
// 「档案被改动」与「冲突已消除（案件已结/档案删除）」。
func (r *CasePartyRepository) FindCandidatesByPartyIDs(partyIDs []uint64) ([]model.PartyCandidate, error) {
	if len(partyIDs) == 0 {
		return nil, nil
	}
	var rows []model.PartyCandidate
	q := r.db.Table("case_parties AS p").
		Select("p.id AS party_id, p.case_id, c.case_no, c.title AS case_title, c.status AS case_status, "+
			"p.side, p.name, p.id_number, p.contact, p.party_role, p.version, p.norm_name, p.norm_id").
		Joins("JOIN cases c ON c.id = p.case_id").
		Where("p.id IN ?", partyIDs)
	if err := q.Order("p.id ASC").Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("find candidates by party ids: %w", err)
	}
	return rows, nil
}

// FindOpposingCandidates 在事务所现存未结案件的对方档案中，按归一化证件号或姓名精确检索候选。
// normID 非空时证件号或姓名任一命中即可；为空时仅按姓名命中。仅返回未结案件。
func (r *CasePartyRepository) FindOpposingCandidates(normName, normID string) ([]model.PartyCandidate, error) {
	var rows []model.PartyCandidate
	q := r.db.Table("case_parties AS p").
		Select("p.id AS party_id, p.case_id, c.case_no, c.title AS case_title, c.status AS case_status, "+
			"p.side, p.name, p.id_number, p.contact, p.party_role, p.version, p.norm_name, p.norm_id").
		Joins("JOIN cases c ON c.id = p.case_id").
		Where("p.side = ?", model.PartySideOpposing).
		Where("c.status NOT IN ?", []string{"closed", "archived"})
	if normID != "" {
		q = q.Where("p.norm_id = ? OR p.norm_name = ?", normID, normName)
	} else {
		q = q.Where("p.norm_name = ?", normName)
	}
	if err := q.Order("p.id ASC").Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("find opposing candidates: %w", err)
	}
	return rows, nil
}
