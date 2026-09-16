// Package migration 管理启动期数据库迁移。
//
// 背景：conflict_checks.check_no 的唯一约束最初同时由 init.sql（具名约束
// uni_conflict_check_no）与 GORM 的无名 uniqueIndex（自动生成索引名）声明。旧库已存在
// 该约束但名称对不上时，GORM AutoMigrate 每次启动都误判“索引缺失”并重复创建，建约束失败
// 即令进程退出，造成容器反复重启；两个实例并发启动时还存在 DDL 的 TOCTOU 竞争。
//
// 因此该唯一约束改由本包显式、幂等地管理：
//   - 以「列上是否已存在任意唯一约束/唯一索引」为准，名字无关，旧库/新库都兼容；
//   - 已存在则跳过（不多余调整、不退出），确实缺失才创建（新库必建出）；
//   - Postgres 用事务级 advisory lock 串行化，两个实例同时启动也只会各成功一次；
//   - 只做 DDL，绝不改写既有检查记录，也不依赖清库或人工补约束。
package migration

import (
	"errors"
	"fmt"
	"log/slog"

	"cylawcase/internal/model"

	"gorm.io/gorm"
)

// advisoryLockKey 冲突检查迁移专用的稳定锁键（自定义，避免与其他业务冲突）。
const advisoryLockKey int64 = 4365840231

// models 为全部需要 AutoMigrate 的实体。
var models = []any{
	&model.User{}, &model.Client{}, &model.Case{}, &model.Document{}, &model.Billing{}, &model.AuditLog{},
	&model.CaseParty{}, &model.ConflictCheck{}, &model.ConflictCheckParty{},
}

// Run 执行启动迁移，按方言选择加锁策略，保证可重复执行且并发安全。
func Run(db *gorm.DB, logger *slog.Logger) error {
	if db.Dialector.Name() == postgresDialect {
		return runPostgres(db, logger)
	}
	return runGeneric(db, logger)
}

// runPostgres 在单个事务内：取事务级 advisory 锁 → AutoMigrate → 幂等补冲突唯一约束。
// 锁在事务提交/回滚时自动释放，从而串行化多个实例的启动迁移，且不留会话级锁。
func runPostgres(db *gorm.DB, logger *slog.Logger) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", advisoryLockKey).Error; err != nil {
			return fmt.Errorf("acquire migration advisory lock: %w", err)
		}
		if err := tx.AutoMigrate(models...); err != nil {
			return fmt.Errorf("auto migrate: %w", err)
		}
		if err := ensureConflictCheckNoUniquePostgres(tx, logger); err != nil {
			return err
		}
		logger.Info("postgres migration completed")
		return nil
	})
}

// runGeneric 供非 Postgres（如测试用 SQLite）使用：AutoMigrate 后用 IF NOT EXISTS 幂等补约束。
func runGeneric(db *gorm.DB, logger *slog.Logger) error {
	if err := db.AutoMigrate(models...); err != nil {
		return fmt.Errorf("auto migrate: %w", err)
	}
	if err := db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_conflict_check_no_unique ON conflict_checks(check_no)").Error; err != nil {
		return fmt.Errorf("ensure conflict check_no unique: %w", err)
	}
	logger.Info("database migration completed")
	return nil
}

// ensureConflictCheckNoUniquePostgres 校验并补齐 conflict_checks.check_no 的唯一约束。
//
// 判定不依赖具体约束名：只要 check_no 列上已存在任一唯一索引（含具名唯一约束、裸唯一索引），
// 即视为满足要求并跳过；只有真的不存在时才创建具名约束，从而兼容旧库与新库。
func ensureConflictCheckNoUniquePostgres(tx *gorm.DB, logger *slog.Logger) error {
	const table, column = "conflict_checks", "check_no"

	exists, err := hasUniqueOnColumn(tx, table, column)
	if err != nil {
		return fmt.Errorf("detect check_no unique: %w", err)
	}
	if exists {
		logger.Info("conflict check_no unique constraint already present, skip", "table", table)
		return nil
	}

	// 真缺失时先排查重复数据；存在重复则不能安全建约束，且按要求不改写既有记录，直接报错。
	var dupCount int64
	if err := tx.Raw(
		"SELECT COUNT(*) FROM (SELECT check_no FROM conflict_checks GROUP BY check_no HAVING COUNT(*) > 1) d",
	).Scan(&dupCount).Error; err != nil {
		return fmt.Errorf("count duplicate check_no: %w", err)
	}
	if dupCount > 0 {
		return fmt.Errorf("cannot create unique constraint: %d duplicate check_no rows exist; data fix required", dupCount)
	}

	if err := tx.Exec(
		"ALTER TABLE conflict_checks ADD CONSTRAINT uni_conflict_check_no UNIQUE (check_no)",
	).Error; err != nil {
		return fmt.Errorf("create check_no unique constraint: %w", err)
	}
	logger.Info("conflict check_no unique constraint created", "table", table, "constraint", "uni_conflict_check_no")
	return nil
}

// hasUniqueOnColumn 判断某表某列上是否已存在唯一索引（唯一约束在 pg_index 中同样表现为 indisunique）。
func hasUniqueOnColumn(tx *gorm.DB, table, column string) (bool, error) {
	const sql = `
SELECT EXISTS (
  SELECT 1
  FROM pg_index i
  JOIN pg_class c ON c.oid = i.indrelid
  JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum = ANY(i.indkey)
  WHERE c.relname = ? AND a.attname = ? AND i.indisunique
    AND array_length(i.indkey, 1) = 1
  LIMIT 1
)`
	var exists bool
	if err := tx.Raw(sql, table, column).Scan(&exists).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	return exists, nil
}

const postgresDialect = "postgres"
