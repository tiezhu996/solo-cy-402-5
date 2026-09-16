package migration

import (
	"io"
	"log/slog"
	"strings"
	"testing"

	"cylawcase/internal/constants"
	"cylawcase/internal/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func openMigratedDB(t *testing.T, dsn string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := Run(db, testLogger()); err != nil {
		t.Fatalf("first migration: %v", err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

// TestMigrationIdempotent 连续执行两次迁移都必须成功（模拟反复重启 / 多实例启动）。
func TestMigrationIdempotent(t *testing.T) {
	dsn := "file:migration_idem?mode=memory&cache=shared"
	db := openMigratedDB(t, dsn)
	// 第二次启动：已有唯一索引时必须跳过，不得报错退出。
	if err := Run(db, testLogger()); err != nil {
		t.Fatalf("second migration must be idempotent, got: %v", err)
	}
	// 第三次也一样。
	if err := Run(db, testLogger()); err != nil {
		t.Fatalf("third migration must be idempotent, got: %v", err)
	}
}

// TestMigrationCreatesCheckNoUnique 新库必须建出 check_no 唯一约束：重复单号写入失败。
func TestMigrationCreatesCheckNoUnique(t *testing.T) {
	dsn := "file:migration_unique?mode=memory&cache=shared"
	db := openMigratedDB(t, dsn)

	mk := func(no string) *model.ConflictCheck {
		return &model.ConflictCheck{
			CheckNo: no, CaseKey: "case-" + no, CaseTitle: "t", OppName: "对方" + no,
			IdentityKey: "k" + no, NormOppName: "n" + no,
			Status: constants.ConflictStatusPendingReview,
		}
	}
	if err := db.Create(mk("CF-1")).Error; err != nil {
		t.Fatalf("first insert: %v", err)
	}
	err := db.Create(mk("CF-1")).Error
	if err == nil {
		t.Fatal("duplicate check_no must be rejected by unique constraint")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "unique") {
		t.Fatalf("expected unique violation, got: %v", err)
	}

	// case_key 唯一约束建出（同一新案收口键）。
	dupCase := mk("CF-2")
	dupCase.CaseKey = "case-CF-1"
	if err := db.Create(dupCase).Error; err == nil {
		t.Fatal("duplicate case_key must be rejected")
	}

	// identity_key 不再唯一：不同新案允许同一对方身份。
	sameOpp := mk("CF-3")
	sameOpp.IdentityKey = "kCF-1"
	if err := db.Create(sameOpp).Error; err != nil {
		t.Fatalf("different cases may share opponent identity, got: %v", err)
	}
}

// TestMigrationDoesNotTouchRows 既有检查记录在重复迁移后保持不变。
func TestMigrationDoesNotTouchRows(t *testing.T) {
	dsn := "file:migration_rows?mode=memory&cache=shared"
	db := openMigratedDB(t, dsn)

	row := &model.ConflictCheck{
		CheckNo: "CF-KEEP", CaseKey: "case-keep", CaseTitle: "保留案件", OppName: "王大明",
		OppIDNumber: "440300198505056789", IdentityKey: "id:440300198505056789",
		NormOppName: "王大明", NormOppID: "440300198505056789",
		Status: constants.ConflictStatusReleased, ReviewBasis: "原始依据",
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("insert: %v", err)
	}

	if err := Run(db, testLogger()); err != nil {
		t.Fatalf("re-migrate: %v", err)
	}
	var got model.ConflictCheck
	if err := db.First(&got, row.ID).Error; err != nil {
		t.Fatalf("reread: %v", err)
	}
	if got.CheckNo != "CF-KEEP" || got.CaseTitle != "保留案件" ||
		got.Status != constants.ConflictStatusReleased || got.ReviewBasis != "原始依据" {
		t.Fatalf("existing row was altered by migration: %+v", got)
	}
}
