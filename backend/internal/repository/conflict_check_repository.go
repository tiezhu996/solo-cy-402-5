package repository

import (
	"errors"
	"fmt"

	"cylawcase/internal/model"

	"gorm.io/gorm"
)

// ConflictCheckRepository 利益冲突检查仓储。
type ConflictCheckRepository struct {
	db *gorm.DB
}

// NewConflictCheckRepository 构造冲突检查仓储。
func NewConflictCheckRepository(db *gorm.DB) *ConflictCheckRepository {
	return &ConflictCheckRepository{db: db}
}

// WithTx 返回绑定到指定事务的仓储副本。
func (r *ConflictCheckRepository) WithTx(tx *gorm.DB) *ConflictCheckRepository {
	return &ConflictCheckRepository{db: tx}
}

// Create 新增冲突检查记录。
func (r *ConflictCheckRepository) Create(chk *model.ConflictCheck) error {
	if err := r.db.Create(chk).Error; err != nil {
		return fmt.Errorf("create conflict check: %w", err)
	}
	return nil
}

// FindByID 按 ID 查询。
func (r *ConflictCheckRepository) FindByID(id uint64) (*model.ConflictCheck, error) {
	var chk model.ConflictCheck
	if err := r.db.First(&chk, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("find conflict check by id: %w", err)
	}
	return &chk, nil
}

// FindByCheckNo 按检查单号查询。
func (r *ConflictCheckRepository) FindByCheckNo(checkNo string) (*model.ConflictCheck, error) {
	var chk model.ConflictCheck
	if err := r.db.Where("check_no = ?", checkNo).First(&chk).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("find conflict check by no: %w", err)
	}
	return &chk, nil
}

// FindByIdentity 按身份收口键查询唯一记录（重复提交收口键）。
func (r *ConflictCheckRepository) FindByIdentity(identityKey string) (*model.ConflictCheck, error) {
	var chk model.ConflictCheck
	if err := r.db.Where("identity_key = ?", identityKey).
		First(&chk).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("find conflict check by identity: %w", err)
	}
	return &chk, nil
}

// Save 全字段保存。
func (r *ConflictCheckRepository) Save(chk *model.ConflictCheck) error {
	if err := r.db.Save(chk).Error; err != nil {
		return fmt.Errorf("save conflict check: %w", err)
	}
	return nil
}

// List 分页查询，可按状态筛选。
func (r *ConflictCheckRepository) List(page, pageSize int, status string) ([]model.ConflictCheck, int64, error) {
	var list []model.ConflictCheck
	var total int64
	q := r.db.Model(&model.ConflictCheck{})
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count conflict checks: %w", err)
	}
	if err := q.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, fmt.Errorf("list conflict checks: %w", err)
	}
	return list, total, nil
}

// ReplaceBindings 以提交时命中快照重建「结论—对方档案版本」绑定关系（事务内调用）。
func (r *ConflictCheckRepository) ReplaceBindings(checkID uint64, snaps model.PartySnapshotJSON) error {
	if err := r.db.Where("check_id = ?", checkID).Delete(&model.ConflictCheckParty{}).Error; err != nil {
		return fmt.Errorf("delete conflict bindings: %w", err)
	}
	links := make([]model.ConflictCheckParty, 0, len(snaps))
	for _, sp := range snaps {
		links = append(links, model.ConflictCheckParty{
			CheckID: checkID, PartyID: sp.PartyID,
			BoundVersion: sp.Version, BoundFingerprint: sp.Fingerprint,
		})
	}
	if len(links) == 0 {
		return nil
	}
	if err := r.db.Create(&links).Error; err != nil {
		return fmt.Errorf("create conflict bindings: %w", err)
	}
	return nil
}

// ListBindings 查询结论绑定的档案版本关系。
func (r *ConflictCheckRepository) ListBindings(checkID uint64) ([]model.ConflictCheckParty, error) {
	var links []model.ConflictCheckParty
	if err := r.db.Where("check_id = ?", checkID).Find(&links).Error; err != nil {
		return nil, fmt.Errorf("list conflict bindings: %w", err)
	}
	return links, nil
}

// InvalidateReleasedByPartyIDs 主动吊销：当指定对方档案变化时，将绑定该档案、
// 当前处于 released 的冲突检查置为 invalidated（关系子查询，数据库无关）。须在事务内调用。
func (r *ConflictCheckRepository) InvalidateReleasedByPartyIDs(partyIDs []uint64, reason string) (int64, error) {
	if len(partyIDs) == 0 {
		return 0, nil
	}
	sub := r.db.Model(&model.ConflictCheckParty{}).
		Select("check_id").
		Where("party_id IN ?", partyIDs)
	res := r.db.Model(&model.ConflictCheck{}).
		Where("status = ?", "released").
		Where("id IN (?)", sub).
		Updates(map[string]any{
			"status":             "invalidated",
			"invalidated_reason": reason,
		})
	if res.Error != nil {
		return 0, fmt.Errorf("invalidate released by party: %w", res.Error)
	}
	return res.RowsAffected, nil
}
