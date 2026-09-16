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
	"strings"

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

// runPostgres 在单个事务内：取事务级 advisory 锁 → 旧库补齐 case_key 列并回填 →
// AutoMigrate（新库直接建全；旧库把 case_key 置 NOT NULL 并建唯一索引）→
// 去除 identity_key 唯一性 → 幂等补 check_no 唯一约束。
// 锁在事务提交/回滚时自动释放，从而串行化多个实例的启动迁移，且不留会话级锁。
func runPostgres(db *gorm.DB, logger *slog.Logger) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", advisoryLockKey).Error; err != nil {
			return fmt.Errorf("acquire migration advisory lock: %w", err)
		}
		// 仅当表已存在（旧库）且缺列时才补列回填；全新库此时尚未建表，直接交给 AutoMigrate。
		if err := backfillCaseKeyOnLegacyPostgres(tx); err != nil {
			return err
		}
		if err := tx.AutoMigrate(models...); err != nil {
			return fmt.Errorf("auto migrate: %w", err)
		}
		// 旧库回填后再置 NOT NULL（新库 AutoMigrate 已建好，此语句为幂等空操作）。
		if err := tx.Exec("ALTER TABLE conflict_checks ALTER COLUMN case_key SET NOT NULL").Error; err != nil {
			return fmt.Errorf("set case_key not null: %w", err)
		}
		if err := dropUniqueOnColumnPostgres(tx, "conflict_checks", "identity_key"); err != nil {
			return err
		}
		// 删除唯一约束后补回普通索引，供按对方身份读回使用（同一对方可对应多个新案）。
		if err := tx.Exec("CREATE INDEX IF NOT EXISTS idx_conflict_identity ON conflict_checks (identity_key)").Error; err != nil {
			return fmt.Errorf("create identity_key index: %w", err)
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

// backfillCaseKeyOnLegacyPostgres 仅在旧库（conflict_checks 已存在但缺 case_key 列）上：
// 新增可空列并为既有结论回填稳定的新案键。回填值附加 id 保证唯一，且不改变业务内容；
// NOT NULL 与唯一索引由随后的 AutoMigrate / SET NOT NULL 收尾。新库（表尚不存在）直接跳过。
func backfillCaseKeyOnLegacyPostgres(tx *gorm.DB) error {
	tableExists, err := tableExists(tx, "conflict_checks")
	if err != nil {
		return err
	}
	if !tableExists {
		return nil
	}
	colExists, err := columnExists(tx, "conflict_checks", "case_key")
	if err != nil {
		return fmt.Errorf("detect case_key column: %w", err)
	}
	if colExists {
		return nil
	}
	if err := tx.Exec("ALTER TABLE conflict_checks ADD COLUMN case_key VARCHAR(100)").Error; err != nil {
		return fmt.Errorf("add case_key column: %w", err)
	}
	// 同一对方在旧模型下可能已有多条记录；追加 id 保证回填值唯一。
	if err := tx.Exec(
		"UPDATE conflict_checks SET case_key = 'legacy:' || COALESCE(identity_key, '') || ':' || id",
	).Error; err != nil {
		return fmt.Errorf("backfill case_key: %w", err)
	}
	return nil
}

// tableExists 判断表是否已存在。
func tableExists(tx *gorm.DB, table string) (bool, error) {
	var exists bool
	err := tx.Raw(`
SELECT EXISTS (
  SELECT 1 FROM information_schema.tables WHERE table_name = ?
)`, table).Scan(&exists).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}
	return exists, nil
}

// columnExists 判断列是否已存在。
func columnExists(tx *gorm.DB, table, column string) (bool, error) {
	var exists bool
	err := tx.Raw(`
SELECT EXISTS (
  SELECT 1 FROM information_schema.columns
  WHERE table_name = ? AND column_name = ?
)`, table, column).Scan(&exists).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}
	return exists, nil
}

// dropUniqueOnColumnPostgres 删除指定列上的单列唯一约束/唯一索引，但保留普通索引。
// 用于把旧版 identity_key 的“按对方身份全局唯一”修正为“同一对方可对应多个新案”。
//
// 先在 Go 中参数化查出对象名（避免在 DO 块体内使用不会被解析的 $1 绑定），再逐条安全地
// 用 %I 引用标识符执行 DDL；幂等：对象不存在时 IF EXISTS 为空操作。
func dropUniqueOnColumnPostgres(tx *gorm.DB, table, column string) error {
	type named struct {
		Name         string
		IsConstraint bool
	}
	var objs []named

	constraintSQL := `
SELECT con.conname AS name, true AS is_constraint
FROM pg_constraint con
JOIN pg_class c ON c.oid = con.conrelid
JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum = con.conkey[1]
WHERE c.relname = ? AND a.attname = ?
  AND con.contype = 'u' AND array_length(con.conkey, 1) = 1`
	if err := tx.Raw(constraintSQL, table, column).Scan(&objs).Error; err != nil {
		return fmt.Errorf("scan unique constraints on %s.%s: %w", table, column, err)
	}

	// 仅找出“不由某唯一约束支撑”的裸唯一索引（约束支撑的会随 DROP CONSTRAINT 一并删除）。
	indexSQL := `
SELECT ci.relname AS name, false AS is_constraint
FROM pg_index i
JOIN pg_class c ON c.oid = i.indrelid
JOIN pg_class ci ON ci.oid = i.indexrelid
JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum = i.indkey[1]
LEFT JOIN pg_constraint con ON con.conindid = i.indexrelid
WHERE c.relname = ? AND a.attname = ? AND i.indisunique
  AND array_length(i.indkey, 1) = 1 AND con.oid IS NULL`
	if err := tx.Raw(indexSQL, table, column).Scan(&objs).Error; err != nil {
		return fmt.Errorf("scan unique indexes on %s.%s: %w", table, column, err)
	}

	for _, o := range objs {
		if o.IsConstraint {
			if err := tx.Exec(fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT IF EXISTS %s",
				pqQuote(table), pqQuote(o.Name))).Error; err != nil {
				return fmt.Errorf("drop constraint %s: %w", o.Name, err)
			}
		} else {
			if err := tx.Exec(fmt.Sprintf("DROP INDEX IF EXISTS %s", pqQuote(o.Name))).Error; err != nil {
				return fmt.Errorf("drop index %s: %w", o.Name, err)
			}
		}
	}
	return nil
}

// pqQuote 用双引号安全引用 Postgres 标识符。
func pqQuote(ident string) string {
	return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
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
