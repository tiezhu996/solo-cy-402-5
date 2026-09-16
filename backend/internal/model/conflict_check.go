package model

import (
	"database/sql/driver"
	"encoding/json"
	"time"
)

// ConflictCheck 新案案源利益冲突检查记录。
// 以归一化对方姓名+证件号为唯一业务键（unique_index），同一新案重复提交收口为同一行的唯一终态。
type ConflictCheck struct {
	ID uint64 `gorm:"primaryKey" json:"id"`
	// CheckNo 仅声明非空；其唯一约束由 migration 包以「存在性探测 + 加锁幂等」方式显式管理，
	// 避免旧库已存在同名/异名唯一约束时 GORM AutoMigrate 重复建索引并导致启动反复退出。
	CheckNo     string       `gorm:"size:50;not null" json:"check_no"`
	CaseTitle   string       `gorm:"size:200;not null;default:''" json:"case_title"`
	OurParties  OurPartyJSON `gorm:"type:jsonb;not null;default:'[]'" json:"our_parties"`
	OppName     string       `gorm:"size:100;not null" json:"opp_name"`
	OppIDNumber string       `gorm:"size:50;not null;default:''" json:"opp_id_number"`
	// 身份收口键：有证件号时取归一化证件号（法定身份标识），否则取归一化姓名。
	// 同一身份只允许一条结论，重复提交据此收口为唯一行/唯一终态。
	IdentityKey string `gorm:"size:100;not null;uniqueIndex:idx_conflict_identity" json:"identity_key"`
	NormOppName string `gorm:"size:100;not null;index" json:"norm_opp_name"`
	NormOppID   string `gorm:"size:50;not null;default:'';index" json:"norm_opp_id"`
	Status      string `gorm:"size:30;not null;default:pending_review;index" json:"status"`
	HitCount    int    `gorm:"not null;default:0" json:"hit_count"`
	// MatchedSnapshot 提交时命中的对方档案快照（含案件与版本），结论绑定此档案版本。
	MatchedSnapshot PartySnapshotJSON `gorm:"type:jsonb;not null;default:'[]'" json:"matched_snapshot"`
	// BoundPartyVersion/BoundFingerprint 结论绑定的档案版本指纹，档案变化即令旧放行失效。
	BoundPartyVersion uint64     `gorm:"not null;default:0" json:"bound_party_version"`
	BoundFingerprint  string     `gorm:"size:64;not null;default:''" json:"bound_fingerprint"`
	ReviewByID        uint64     `gorm:"not null;default:0" json:"review_by_id"`
	ReviewByName      string     `gorm:"size:50;not null;default:''" json:"review_by_name"`
	ReviewBasis       string     `gorm:"type:text" json:"review_basis"`
	ReviewedAt        *time.Time `json:"reviewed_at"`
	InvalidatedReason string     `gorm:"size:200;not null;default:''" json:"invalidated_reason"`
	SubmitByID        uint64     `gorm:"not null;default:0" json:"submit_by_id"`
	SubmitByName      string     `gorm:"size:50;not null;default:''" json:"submit_by_name"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`

	// 以下为非持久化的实时派生字段，由 service 在读取时填充，不从库中映射。
	LiveMatch    bool              `gorm:"-" json:"live_match"`
	LiveSnapshot PartySnapshotJSON `gorm:"-" json:"live_snapshot,omitempty"`
	CanProceed   bool              `gorm:"-" json:"can_proceed"`
	Stale        bool              `gorm:"-" json:"stale"`
	// StaleReason 失效原因分类：profile_changed=档案在未结案件中被改动（旧放行失效，须重审）；
	// cleared=冲突已消除（所冲突案件已结/档案已删除）。
	StaleReason string `gorm:"-" json:"stale_reason,omitempty"`
}

// 失效原因常量（非持久化派生字段取值）。
const (
	StaleReasonProfileChanged = "profile_changed"
	StaleReasonCleared        = "cleared"
)

// TableName 指定表名。
func (ConflictCheck) TableName() string { return "conflict_checks" }

// ConflictCheckParty 冲突结论与提交时命中的对方档案之间的绑定关系。
// 每条记录把结论锚定到某个档案的具体版本；档案版本推进/删除/案件已结即令结论失效。
// 用关系表（而非 jsonb 包含查询）来做失效吊销，便于索引且数据库无关。
type ConflictCheckParty struct {
	ID               uint64 `gorm:"primaryKey" json:"id"`
	CheckID          uint64 `gorm:"not null;uniqueIndex:idx_check_party,priority:1;index" json:"check_id"`
	PartyID          uint64 `gorm:"not null;uniqueIndex:idx_check_party,priority:2;index" json:"party_id"`
	BoundVersion     uint64 `gorm:"not null;default:1" json:"bound_version"`
	BoundFingerprint string `gorm:"size:64;not null;default:''" json:"bound_fingerprint"`
}

// TableName 指定表名。
func (ConflictCheckParty) TableName() string { return "conflict_check_parties" }

// OurParty 提交时录入的本方（客户/联系人）。
type OurParty struct {
	Name      string `json:"name"`
	IDNumber  string `json:"id_number,omitempty"`
	Contact   string `json:"contact,omitempty"`
	PartyRole string `json:"party_role,omitempty"`
}

// OurPartyJSON 本方列表 JSON 存储类型。
type OurPartyJSON []OurParty

// Value 实现 driver.Valuer。
func (o OurPartyJSON) Value() (driver.Value, error) {
	if o == nil {
		return []byte("[]"), nil
	}
	return json.Marshal([]OurParty(o))
}

// PartySnapshot 命中对方档案在提交瞬间的快照项。
type PartySnapshot struct {
	PartyID     uint64 `json:"party_id"`
	CaseID      uint64 `json:"case_id"`
	CaseNo      string `json:"case_no"`
	CaseTitle   string `json:"case_title"`
	CaseStatus  string `json:"case_status"`
	Side        string `json:"side"`
	Name        string `json:"name"`
	IDNumber    string `json:"id_number"`
	Contact     string `json:"contact"`
	PartyRole   string `json:"party_role"`
	Version     uint64 `json:"version"`
	Fingerprint string `json:"fingerprint"`
	MatchedBy   string `json:"matched_by"` // id_number / name
}

// PartySnapshotJSON 快照列表 JSON 存储类型，直接透传原始 JSON 而非 base64。
type PartySnapshotJSON []PartySnapshot

// Value 实现 driver.Valuer。
func (s PartySnapshotJSON) Value() (driver.Value, error) {
	if s == nil {
		return []byte("[]"), nil
	}
	return json.Marshal([]PartySnapshot(s))
}

// MarshalJSON 以 JSON 数组输出快照。
func (s PartySnapshotJSON) MarshalJSON() ([]byte, error) {
	if s == nil {
		return []byte("[]"), nil
	}
	return json.Marshal([]PartySnapshot(s))
}

// Scan 实现 sql.Scanner。
func (s *PartySnapshotJSON) Scan(src any) error {
	if src == nil {
		*s = nil
		return nil
	}
	var raw []byte
	switch v := src.(type) {
	case []byte:
		raw = v
	case string:
		raw = []byte(v)
	default:
		return nil
	}
	if len(raw) == 0 {
		*s = PartySnapshotJSON{}
		return nil
	}
	var out []PartySnapshot
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	*s = PartySnapshotJSON(out)
	return nil
}

// MarshalJSON 以 JSON 数组输出本方列表。
func (o OurPartyJSON) MarshalJSON() ([]byte, error) {
	if o == nil {
		return []byte("[]"), nil
	}
	return json.Marshal([]OurParty(o))
}

// Scan 实现 sql.Scanner。
func (o *OurPartyJSON) Scan(src any) error {
	if src == nil {
		*o = nil
		return nil
	}
	var raw []byte
	switch v := src.(type) {
	case []byte:
		raw = v
	case string:
		raw = []byte(v)
	default:
		return nil
	}
	if len(raw) == 0 {
		*o = OurPartyJSON{}
		return nil
	}
	var out []OurParty
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	*o = OurPartyJSON(out)
	return nil
}

// PartyCandidate 冲突匹配候选行：对方档案 + 所在未结案件。
type PartyCandidate struct {
	PartyID    uint64
	CaseID     uint64
	CaseNo     string
	CaseTitle  string
	CaseStatus string
	Side       string
	Name       string
	IDNumber   string
	Contact    string
	PartyRole  string
	Version    uint64
	NormName   string
	NormID     string
}
