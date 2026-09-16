package service

import (
	"errors"
	"log/slog"
	"strings"

	"cylawcase/internal/conflict"
	"cylawcase/internal/constants"
	"cylawcase/internal/model"
	"cylawcase/internal/repository"
	"cylawcase/internal/util"

	"gorm.io/gorm"
)

// CasePartyService 案件当事人档案业务逻辑。
type CasePartyService struct {
	repo     *repository.CasePartyRepository
	chkRepo  *repository.ConflictCheckRepository
	caseRepo *repository.CaseRepository
	db       *gorm.DB
	logger   *slog.Logger
}

// NewCasePartyService 构造当事人档案服务。
func NewCasePartyService(repo *repository.CasePartyRepository, chkRepo *repository.ConflictCheckRepository,
	caseRepo *repository.CaseRepository, db *gorm.DB, logger *slog.Logger) *CasePartyService {
	return &CasePartyService{repo: repo, chkRepo: chkRepo, caseRepo: caseRepo, db: db, logger: logger}
}

// Add 为案件新增当事人档案。
func (s *CasePartyService) Add(caseID uint64, side, name, idNumber, contact, partyRole string) (*model.CaseParty, error) {
	if side != model.PartySideOur && side != model.PartySideOpposing {
		return nil, util.NewAppError(constants.CodeValidationFailed, "CaseParty[side="+side+"] add: invalid side")
	}
	if strings.TrimSpace(name) == "" {
		return nil, util.NewAppError(constants.CodeValidationFailed, "CaseParty[name] add: name required")
	}
	if _, err := s.caseRepo.FindByID(caseID); err != nil {
		return nil, util.Wrap(err, "CaseParty[case_id=%d] add: case not found", caseID)
	}
	p := &model.CaseParty{
		CaseID: caseID, Side: side, Name: strings.TrimSpace(name),
		IDNumber: strings.TrimSpace(idNumber), Contact: strings.TrimSpace(contact),
		PartyRole: strings.TrimSpace(partyRole), Version: 1,
	}
	p.NormName = conflict.Normalize(p.Name)
	p.NormID = conflict.Normalize(p.IDNumber)
	if err := s.repo.Create(p); err != nil {
		s.logger.Error(constants.LogPartyUpsertSuccess, "error", err.Error())
		return nil, util.Wrap(err, "CaseParty[case_id=%d] add failed", caseID)
	}
	s.logger.Info(constants.LogPartyUpsertSuccess, "party_id", p.ID, "case_id", caseID, "side", side)
	return p, nil
}

// Update 更新当事人档案。若对方实质身份字段（姓名/证件号）变更，则版本号自增，
// 并在同一事务内将绑定该档案的旧「放行」结论吊销为 invalidated。
func (s *CasePartyService) Update(id uint64, name, idNumber, contact, partyRole string) (*model.CaseParty, error) {
	p, err := s.repo.FindByID(id)
	if err != nil {
		return nil, util.Wrap(err, "CaseParty[id=%d] update find failed", id)
	}

	name = strings.TrimSpace(name)
	idNumber = strings.TrimSpace(idNumber)
	identityChanged := false
	if name != "" && conflict.Normalize(name) != p.NormName {
		identityChanged = true
		p.Name = name
		p.NormName = conflict.Normalize(name)
	}
	if idNumber != "" && conflict.Normalize(idNumber) != p.NormID {
		identityChanged = true
		p.IDNumber = idNumber
		p.NormID = conflict.Normalize(idNumber)
	}
	if contact != "" {
		p.Contact = strings.TrimSpace(contact)
	}
	if partyRole != "" {
		p.PartyRole = strings.TrimSpace(partyRole)
	}

	if !identityChanged {
		if err := s.repo.Save(p); err != nil {
			return nil, util.Wrap(err, "CaseParty[id=%d] update save failed", id)
		}
		return p, nil
	}

	// 身份字段变更：档案版本推进 + 旧放行失效，必须在同一事务中原子完成。
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.repo.WithTx(tx).Save(p); err != nil {
			return err
		}
		if err := s.repo.WithTx(tx).BumpVersion(p.ID); err != nil {
			return err
		}
		n, err := s.chkRepo.WithTx(tx).InvalidateReleasedByPartyIDs(
			[]uint64{p.ID}, constants.MsgConflictStale)
		if err != nil {
			return err
		}
		if n > 0 {
			s.logger.Info(constants.LogPartyVersionBump, "party_id", p.ID, "invalidated", n)
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, util.NewAppError(constants.CodePartyNotFound, "CaseParty[id] update: not found")
		}
		s.logger.Error(constants.LogPartyUpdateSuccess, "error", err.Error())
		return nil, util.Wrap(err, "CaseParty[id=%d] identity update failed", id)
	}
	// 重新读取以取得自增后的版本号。
	updated, err := s.repo.FindByID(id)
	if err == nil {
		p = updated
	}
	s.logger.Info(constants.LogPartyUpdateSuccess, "party_id", id, "version", p.Version)
	return p, nil
}

// ListByCase 查询案件当事人。
func (s *CasePartyService) ListByCase(caseID uint64) ([]model.CaseParty, error) {
	return s.repo.ListByCase(caseID)
}
