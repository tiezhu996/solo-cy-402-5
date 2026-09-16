package constants

// messages.go 同时承载前端提示文案、后端返回文案与日志文案。
const (
	MsgOK                    = "ok"
	MsgInvalidParams         = "参数不合法"
	MsgUnauthorized          = "未登录或登录已过期"
	MsgForbidden             = "没有权限执行该操作"
	MsgNotFound              = "资源不存在"
	MsgTooManyRequests       = "请求过于频繁，请稍后再试"
	MsgInternalError         = "服务器内部错误"
	MsgUsernameExists        = "用户名已存在"
	MsgInvalidCredentials    = "用户名或密码错误"
	MsgCaseStatusConflict    = "案件状态流转冲突"
	MsgBillingStatusConflict = "账单状态流转冲突"
	MsgUploadTooLarge        = "上传文件过大"
	MsgUnsupportedFileType   = "不支持的文件类型"
	MsgLoginSuccess          = "登录成功"
	MsgClientCreated         = "客户创建成功"
	MsgCaseCreated           = "案件创建成功"
	MsgDocumentUploaded      = "文档上传成功"
	MsgBillingCreated        = "账单创建成功"
	MsgBillingPaid           = "账单已标记支付"
	MsgBillingInvoiced       = "账单已开票"
	MsgBillingVoided         = "账单已作废"

	// 利益冲突检查文案（前端提示/后端返回/日志共用，修改一处影响全链路）
	MsgConflictHit            = "命中事务所现存未结案件的对方档案，已保存为待复核，需管理员放行"
	MsgConflictNoHit          = "未命中现存未结案件对方档案，可正常办理"
	MsgConflictReleased       = "管理员已填写依据并放行"
	MsgConflictRejected       = "管理员已驳回，该新案不得办理"
	MsgConflictBasisRequired  = "放行或驳回必须填写审查依据"
	MsgConflictStale          = "绑定的对方档案已发生变化，原放行结论已失效，需重新复核"
	MsgConflictDecisionClosed = "当前冲突结论已终态，不能重复审查"
)
