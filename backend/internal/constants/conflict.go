package constants

// ConflictStatus 利益冲突检查状态枚举。
const (
	ConflictStatusNoConflict    = "no_conflict"    // 未命中，可直接办理
	ConflictStatusPendingReview = "pending_review" // 命中，仅可保存为待复核
	ConflictStatusReleased      = "released"       // 管理员填写依据后放行（仍绑定档案版本）
	ConflictStatusRejected      = "rejected"       // 管理员驳回，不得办理
	ConflictStatusInvalidated   = "invalidated"    // 绑定档案变化，旧放行/结论失效
)

// ConflictStatusValues 全部冲突检查状态值。
var ConflictStatusValues = []string{
	ConflictStatusNoConflict, ConflictStatusPendingReview, ConflictStatusReleased,
	ConflictStatusRejected, ConflictStatusInvalidated,
}

// IsValidConflictStatus 校验冲突检查状态。
func IsValidConflictStatus(s string) bool {
	for _, v := range ConflictStatusValues {
		if v == s {
			return true
		}
	}
	return false
}

// 命中方式。
const (
	MatchedByIDNumber = "id_number"
	MatchedByName     = "name"
)
