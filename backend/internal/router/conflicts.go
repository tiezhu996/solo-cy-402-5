package router

import (
	"cylawcase/internal/constants"
	"cylawcase/internal/middleware"

	"github.com/gin-gonic/gin"
)

// registerConflictRoutes 案源利益冲突检查路由。
// 说明：首段全部使用静态段（lookup/id/cases/parties），避免与通配参数在路由树中同级冲突。
func (r *Router) registerConflictRoutes(g *gin.RouterGroup) {
	conflicts := g.Group("/conflict-checks")
	conflicts.Use(middleware.AuthRequired(r.cfg))

	// 提交新案冲突检查；列表（可按 status 筛选）。
	conflicts.POST("", r.conflict.Submit)
	conflicts.GET("", r.conflict.List)

	// 从同一入口按对方姓名/证件号或检查单号读回唯一结论。
	conflicts.GET("/lookup", r.conflict.Read)

	// 按记录 ID 读回唯一结论（含实时版本复核）。
	conflicts.GET("/id/:id", r.conflict.Get)

	// 管理员复核：放行/驳回均须填写依据。
	conflicts.POST("/id/:id/release", middleware.RequireRole(constants.RoleAdmin), r.conflict.Release)
	conflicts.POST("/id/:id/reject", middleware.RequireRole(constants.RoleAdmin), r.conflict.Reject)

	// 案件当事人档案：本方/对方登记与查询。
	conflicts.POST("/cases/:id/parties", r.conflict.AddParty)
	conflicts.GET("/cases/:id/parties", r.conflict.ListParties)
	// 更新对方身份字段会令旧放行失效，仅管理员可操作。
	conflicts.PUT("/parties/:partyId", middleware.RequireRole(constants.RoleAdmin), r.conflict.UpdateParty)
}
