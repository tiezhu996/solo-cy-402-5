package constants

// 统一错误码：0 表示成功，其余按模块分段。
const (
	CodeOK                    = 0
	CodeBadRequest            = 40000
	CodeUnauthorized          = 40100
	CodeForbidden             = 40300
	CodeNotFound              = 40400
	CodeConflict              = 40900
	CodeTooManyRequests       = 42900
	CodeValidationFailed      = 42200
	CodeInternalError         = 50000
	CodeUserExists            = 40001
	CodeInvalidCredentials    = 40101
	CodeCaseStatusConflict    = 40901
	CodeBillingStatusConflict = 40902
	CodeUploadTooLarge        = 41300
	CodeUnsupportedType       = 41500

	// 利益冲突检查分段 46xxx
	CodeConflictHit           = 40903 // 命中现存未结案件对方档案，须待复核
	CodeConflictStateConflict = 40904 // 当前状态不允许该流转
	CodeConflictBasisRequired = 42210 // 放行/驳回必须填写依据
	CodeConflictDecisionStale = 40905 // 绑定档案已变化，旧结论失效
	CodePartyNotFound         = 40410
)
