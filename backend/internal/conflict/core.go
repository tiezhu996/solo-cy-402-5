package conflict

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"cylawcase/internal/constants"
	"cylawcase/internal/model"
)

// Normalize 归一化姓名/证件号：去除所有空白并统一大写，作为精确匹配键。
// 姓名与证件号是法律身份标识，因此采用精确相等而非模糊包含。
func Normalize(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '　' {
			continue
		}
		b.WriteRune(r)
	}
	return strings.ToUpper(b.String())
}

// Fingerprint 计算档案实质字段（姓名+证件号）的版本指纹。
// 版本号相同但指纹变化、或指纹相同但版本号推进，都视为档案已变化。
func Fingerprint(name, idNumber string) string {
	sum := sha256.Sum256([]byte(Normalize(name) + "|" + Normalize(idNumber)))
	return hex.EncodeToString(sum[:])
}

// IsOpenCase 判断案件是否未结（closed/archived 为已结，不参与冲突命中）。
func IsOpenCase(status string) bool {
	return status != "closed" && status != "archived"
}

// IdentityKey 计算对方身份收口键：有证件号时以归一化证件号为准（法定身份标识），
// 否则退化为归一化姓名。同一身份重复提交据此收口为唯一结论。
func IdentityKey(normName, normID string) string {
	if normID != "" {
		return "id:" + normID
	}
	return "name:" + normName
}

// MatchHit 描述一次命中。
type MatchHit struct {
	Candidate model.PartyCandidate
	MatchedBy string // id_number / name
}

// Match 在事务所现存未结案件的对方档案候选集中按证件号或姓名命中。
// 证件号精确命中优先；无证件号时退化为姓名精确命中。返回全部命中（可跨多案）。
func Match(oppName, oppIDNumber string, candidates []model.PartyCandidate) []MatchHit {
	nName := Normalize(oppName)
	nID := Normalize(oppIDNumber)
	var idHits, nameHits []MatchHit
	for _, c := range candidates {
		if c.Side != model.PartySideOpposing || !IsOpenCase(c.CaseStatus) {
			continue
		}
		if nID != "" && c.NormID != "" && c.NormID == nID {
			idHits = append(idHits, MatchHit{Candidate: c, MatchedBy: "id_number"})
			continue
		}
		if nName != "" && c.NormName == nName {
			nameHits = append(nameHits, MatchHit{Candidate: c, MatchedBy: "name"})
		}
	}
	if len(idHits) > 0 {
		return idHits
	}
	return nameHits
}

// ToSnapshot 将命中转换为提交时绑定的档案快照。
func ToSnapshot(h MatchHit) model.PartySnapshot {
	c := h.Candidate
	return model.PartySnapshot{
		PartyID:     c.PartyID,
		CaseID:      c.CaseID,
		CaseNo:      c.CaseNo,
		CaseTitle:   c.CaseTitle,
		CaseStatus:  c.CaseStatus,
		Side:        c.Side,
		Name:        c.Name,
		IDNumber:    c.IDNumber,
		Contact:     c.Contact,
		PartyRole:   c.PartyRole,
		Version:     c.Version,
		Fingerprint: Fingerprint(c.Name, c.IDNumber),
		MatchedBy:   h.MatchedBy,
	}
}

// SnapshotStale 判断提交时绑定的快照是否已与现状不符。
// 命中档案被删除、版本推进、身份字段变更（指纹变化）或案件已结，均判为过期。
func SnapshotStale(snap model.PartySnapshot, current *model.PartyCandidate, currentCaseStatus string) bool {
	if current == nil {
		return true
	}
	if snap.Version != current.Version {
		return true
	}
	if snap.Fingerprint != Fingerprint(current.Name, current.IDNumber) {
		return true
	}
	if currentCaseStatus != "" && !IsOpenCase(currentCaseStatus) {
		return true
	}
	return false
}

// Binding 计算命中集合的聚合版本（取最小版本，任一推进即不符）与组合指纹。
func Binding(snaps model.PartySnapshotJSON) (uint64, string) {
	if len(snaps) == 0 {
		return 0, ""
	}
	minVersion := snaps[0].Version
	parts := make([]string, 0, len(snaps))
	for _, sp := range snaps {
		if sp.Version < minVersion {
			minVersion = sp.Version
		}
		parts = append(parts, fmt.Sprintf("%d:%s", sp.PartyID, sp.Fingerprint))
	}
	sort.Strings(parts)
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return minVersion, hex.EncodeToString(sum[:])
}

// BindingUnchanged 判断放行后重复提交时，命中集合的版本与组合指纹是否均未变。
func BindingUnchanged(boundVersion uint64, boundFP string, snaps model.PartySnapshotJSON) bool {
	v, fp := Binding(snaps)
	return v == boundVersion && fp == boundFP
}

// CollapseStatus 同一新案重复提交时，把既有状态按实时命中收口到唯一终态。
// hit=本次是否命中，bindingOK=放行所绑定档案版本/指纹是否仍一致。返回收口后的状态。
func CollapseStatus(current string, hit, bindingOK bool) string {
	switch current {
	case constants.ConflictStatusReleased:
		// 有效放行且仍命中同一档案：保持唯一放行。
		if hit && bindingOK {
			return constants.ConflictStatusReleased
		}
		// 仍命中但档案已变/出现新冲突：旧放行不得沿用，回到待复核。
		if hit {
			return constants.ConflictStatusPendingReview
		}
		// 不再有任何未结冲突（案件已结/档案删除）：冲突消除，收口为无冲突。
		return constants.ConflictStatusNoConflict
	case constants.ConflictStatusRejected:
		// 驳回为终态，重复提交不翻案。
		return constants.ConflictStatusRejected
	case constants.ConflictStatusPendingReview:
		if hit {
			return constants.ConflictStatusPendingReview
		}
		return constants.ConflictStatusNoConflict
	default: // no_conflict / invalidated
		if hit {
			return constants.ConflictStatusPendingReview
		}
		return constants.ConflictStatusNoConflict
	}
}

// CanDecide 仅待复核状态允许管理员放行/驳回。
func CanDecide(status string) bool {
	return status == constants.ConflictStatusPendingReview
}
