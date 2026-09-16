package model

import "time"

// PartySide 当事人立场枚举。
const (
	PartySideOur      = "our"      // 本方：客户及联系人
	PartySideOpposing = "opposing" // 对方
)

// CaseParty 案件当事人档案。新案冲突检查时，事务所现存未结案件的「对方」档案即来源于此。
type CaseParty struct {
	ID        uint64 `gorm:"primaryKey" json:"id"`
	CaseID    uint64 `gorm:"not null;index;uniqueIndex:idx_case_party_identity,priority:1" json:"case_id"`
	Side      string `gorm:"size:20;not null;index;uniqueIndex:idx_case_party_identity,priority:2" json:"side"`
	Name      string `gorm:"size:100;not null" json:"name"`
	IDNumber  string `gorm:"size:50;not null;default:''" json:"id_number"`
	Contact   string `gorm:"size:50;not null;default:''" json:"contact"`
	PartyRole string `gorm:"size:50;not null;default:''" json:"party_role"`
	// NormName/NormID 为归一化后的精确匹配键，并承担唯一约束。
	NormName string `gorm:"size:100;not null;index;uniqueIndex:idx_case_party_identity,priority:3" json:"norm_name"`
	NormID   string `gorm:"size:50;not null;default:'';index;uniqueIndex:idx_case_party_identity,priority:4" json:"norm_id"`
	// Version 档案版本号，每次实质字段变更自增。冲突结论绑定提交时的版本。
	Version   uint64    `gorm:"not null;default:1" json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 指定表名。
func (CaseParty) TableName() string { return "case_parties" }
