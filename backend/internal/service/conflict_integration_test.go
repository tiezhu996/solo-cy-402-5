package service

import (
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"

	"cylawcase/internal/constants"
	"cylawcase/internal/dto"
	"cylawcase/internal/migration"
	"cylawcase/internal/model"
	"cylawcase/internal/repository"
	"cylawcase/internal/util"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newConflictTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := migration.Run(db, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	sqlDB, _ := db.DB()
	// 单连接：并发请求在连接层排队、整事务串行提交，确定性地验证「同时到达也只出一条且都成功」。
	// 生产为 PostgreSQL（MVCC），真正的撞键由 case_key 唯一索引 + 提交重试兜底。
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

type conflictEnv struct {
	db          *gorm.DB
	partySvc    *CasePartyService
	conflictSvc *ConflictService
	partyRepo   *repository.CasePartyRepository
}

func setupConflict(t *testing.T) *conflictEnv {
	db := newConflictTestDB(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	caseRepo := repository.NewCaseRepository(db)
	partyRepo := repository.NewCasePartyRepository(db)
	chkRepo := repository.NewConflictCheckRepository(db)
	return &conflictEnv{
		db:          db,
		partySvc:    NewCasePartyService(partyRepo, chkRepo, caseRepo, db, logger),
		conflictSvc: NewConflictService(chkRepo, partyRepo, db, logger),
		partyRepo:   partyRepo,
	}
}

func seedOpenCaseWithOpposing(t *testing.T, env *conflictEnv, caseNo, oppName, oppID string) (*model.Case, *model.CaseParty) {
	t.Helper()
	c := &model.Case{
		CaseNo: caseNo, Title: "现存案件-" + caseNo, CaseType: constants.CaseTypeCivil,
		Status: constants.CaseStatusFiled, ClientID: 1, LeadLawyerID: 1,
		CoLawyerIDs: model.CoLawyerJSON([]byte("[]")),
	}
	if err := env.db.Create(c).Error; err != nil {
		t.Fatalf("seed case: %v", err)
	}
	p := &model.CaseParty{
		CaseID: c.ID, Side: model.PartySideOpposing, Name: oppName, IDNumber: oppID,
		NormName: oppName, NormID: oppID, Version: 1,
	}
	if err := env.db.Create(p).Error; err != nil {
		t.Fatalf("seed party: %v", err)
	}
	return c, p
}

func submitReq(title, oppName, oppID string) dto.ConflictSubmitRequest {
	return dto.ConflictSubmitRequest{
		CaseKey:     "KEY:" + title,
		CaseTitle:   title,
		OurParties:  []dto.OurPartyDTO{{Name: "本方客户", IDNumber: "111111"}},
		OppName:     oppName,
		OppIDNumber: oppID,
	}
}

func appCode(t *testing.T, err error) int {
	t.Helper()
	var ae *util.AppError
	if !errors.As(err, &ae) {
		t.Fatalf("expected AppError, got %v", err)
	}
	return ae.Code
}

// TestConflictDifferentCasesSameOpponentStaySeparate 不同新案即使对方姓名/证件号相同，
// 也必须各自独立成行，标题不互相覆盖、可分别复核。
func TestConflictDifferentCasesSameOpponentStaySeparate(t *testing.T) {
	env := setupConflict(t)
	seedOpenCaseWithOpposing(t, env, "C1", "王大明", "440300198505056789")

	a, err := env.conflictSvc.Submit(submitReq("新案甲", "王大明", "440300198505056789"), 9, "lawyer")
	if err != nil {
		t.Fatalf("submit 甲: %v", err)
	}
	b, err := env.conflictSvc.Submit(submitReq("新案乙", "王大明", "440300198505056789"), 9, "lawyer")
	if err != nil {
		t.Fatalf("submit 乙: %v", err)
	}
	if a.ID == b.ID {
		t.Fatalf("different new cases must not share a row, both id=%d", a.ID)
	}
	if a.CaseTitle != "新案甲" || b.CaseTitle != "新案乙" {
		t.Fatalf("titles must not overwrite each other: %q vs %q", a.CaseTitle, b.CaseTitle)
	}
	// 按对方读回应看到多条结论（取最近一条为乙）。
	var n int64
	if err := env.db.Model(&model.ConflictCheck{}).Count(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Fatalf("want 2 independent conclusions, got %d", n)
	}
	latest, err := env.conflictSvc.ReadConclusion("", "王大明", "440300198505056789", "")
	if err != nil {
		t.Fatalf("read latest: %v", err)
	}
	if latest.ID != b.ID {
		t.Fatalf("latest by opponent should be case 乙(id=%d), got id=%d", b.ID, latest.ID)
	}
	// 按新案键可分别精确读回甲、乙。
	gotA, err := env.conflictSvc.ReadConclusion("KEY:新案甲", "", "", "")
	if err != nil {
		t.Fatalf("read A: %v", err)
	}
	if gotA.ID != a.ID || gotA.CaseTitle != "新案甲" {
		t.Fatalf("read by case_key must isolate A, got id=%d title=%q", gotA.ID, gotA.CaseTitle)
	}
}

// TestConflictConcurrentSameCaseCollapsesToOne 同一新案两个请求同时到达：只生成一条结论，
// 两者都成功收口到同一行，不串案、不产生重复主键。
func TestConflictConcurrentSameCaseCollapsesToOne(t *testing.T) {
	env := setupConflict(t)
	seedOpenCaseWithOpposing(t, env, "C1", "王大明", "440300198505056789")

	req := submitReq("新案并发", "王大明", "4403001985056789")
	var wg sync.WaitGroup
	results := make(chan *model.ConflictCheck, 2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := env.conflictSvc.Submit(req, 9, "lawyer")
			if err != nil {
				errs <- err
				return
			}
			results <- r
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		t.Fatalf("concurrent submit must not error: %v", e)
	}
	ids := map[uint64]struct{}{}
	for r := range results {
		ids[r.ID] = struct{}{}
		if r.CaseTitle != "新案并发" {
			t.Fatalf("case title串案: %q", r.CaseTitle)
		}
	}
	if len(ids) != 1 {
		t.Fatalf("concurrent same-case submit must collapse to one row, got ids=%v", ids)
	}
	var n int64
	if err := env.db.Model(&model.ConflictCheck{}).Where("case_key = ?", "KEY:新案并发").Count(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("want exactly 1 row for the case_key, got %d", n)
	}
}

// TestConflictHitMustPendingThenRelease 命中→只能待复核→无依据拒绝→带依据放行→可办理。
func TestConflictHitMustPendingThenRelease(t *testing.T) {
	env := setupConflict(t)
	seedOpenCaseWithOpposing(t, env, "C1", "王大明", "440300198505056789")

	chk, err := env.conflictSvc.Submit(submitReq("新案A", "王大明", "440300198505056789"), 9, "lawyer")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if chk.Status != constants.ConflictStatusPendingReview || chk.HitCount != 1 || chk.CanProceed {
		t.Fatalf("want pending/hit1/not proceed, got %s hit=%d proceed=%v", chk.Status, chk.HitCount, chk.CanProceed)
	}
	if chk.BoundPartyVersion != 1 {
		t.Fatalf("bound version want 1 got %d", chk.BoundPartyVersion)
	}

	// 放行必须填写依据。
	if _, err := env.conflictSvc.Release(chk.ID, "  ", 1, "admin"); err == nil {
		t.Fatal("release without basis must fail")
	} else if appCode(t, err) != constants.CodeConflictBasisRequired {
		t.Fatalf("want basis required code, got %v", err)
	}

	rel, err := env.conflictSvc.Release(chk.ID, "已核实两案主体不同，准予接案", 1, "admin")
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if rel.Status != constants.ConflictStatusReleased || !rel.CanProceed {
		t.Fatalf("want released/proceed, got %s/%v", rel.Status, rel.CanProceed)
	}
}

// TestConflictDuplicateSubmitCollapses 同一新案重复提交收口为唯一行；有效放行保持终态。
func TestConflictDuplicateSubmitCollapses(t *testing.T) {
	env := setupConflict(t)
	seedOpenCaseWithOpposing(t, env, "C1", "李四", "")

	first, err := env.conflictSvc.Submit(submitReq("新案B", "李四", ""), 9, "lawyer")
	if err != nil {
		t.Fatalf("submit1: %v", err)
	}
	if _, err := env.conflictSvc.Release(first.ID, "经主任批准", 1, "admin"); err != nil {
		t.Fatalf("release: %v", err)
	}
	// 同一新案重复提交（对方姓名的大小写/空白差异归一化），不产生第二条记录。
	second, err := env.conflictSvc.Submit(submitReq("新案B", " 李 四 ", ""), 9, "lawyer")
	if err != nil {
		t.Fatalf("submit2: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("duplicate submit must collapse to same row: %d vs %d", second.ID, first.ID)
	}
	if second.Status != constants.ConflictStatusReleased || !second.CanProceed {
		t.Fatalf("valid release must survive identical resubmit, got %s proceed=%v", second.Status, second.CanProceed)
	}
}

// TestConflictProfileChangeInvalidatesRelease 档案版本变化→旧放行失效→不能沿用，需重新复核。
func TestConflictProfileChangeInvalidatesRelease(t *testing.T) {
	env := setupConflict(t)
	_, party := seedOpenCaseWithOpposing(t, env, "C1", "王大明", "440300198505056789")

	chk, err := env.conflictSvc.Submit(submitReq("新案C", "王大明", "440300198505056789"), 9, "lawyer")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := env.conflictSvc.Release(chk.ID, "批准接案", 1, "admin"); err != nil {
		t.Fatalf("release: %v", err)
	}

	// 对方身份字段变化：版本推进，旧放行被主动吊销。
	updated, err := env.partySvc.Update(party.ID, "王二明", "440300198505056789", "", "")
	if err != nil {
		t.Fatalf("party update: %v", err)
	}
	if updated.Version != 2 {
		t.Fatalf("party version want 2 got %d", updated.Version)
	}

	got, err := env.conflictSvc.Get(chk.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != constants.ConflictStatusInvalidated || got.CanProceed {
		t.Fatalf("want invalidated/not proceed, got %s proceed=%v", got.Status, got.CanProceed)
	}

	// 失效态不能再放行，必须重新提交回到待复核。
	if _, err := env.conflictSvc.Release(got.ID, "再批准一次", 1, "admin"); err == nil {
		t.Fatal("release on invalidated must be rejected")
	}
	// 证件号不变（法定身份不变）：同一记录收口；改名档案仍被命中，但版本/指纹已变，故回到待复核，不得沿用旧放行。
	recheck, err := env.conflictSvc.Submit(submitReq("新案C", "王二明", "440300198505056789"), 9, "lawyer")
	if err != nil {
		t.Fatalf("resubmit: %v", err)
	}
	if recheck.ID != chk.ID {
		t.Fatalf("must keep same record, got %d vs %d", recheck.ID, chk.ID)
	}
	if recheck.Status != constants.ConflictStatusPendingReview || recheck.CanProceed {
		t.Fatalf("want pending/not proceed after identity change, got %s proceed=%v", recheck.Status, recheck.CanProceed)
	}
}

// TestConflictNoConflictBecomesHit 读回复核：无冲突结论在档案新增命中后自动回到待复核。
func TestConflictNoConflictBecomesHit(t *testing.T) {
	env := setupConflict(t)
	chk, err := env.conflictSvc.Submit(submitReq("新案D", "赵六", "555"), 9, "lawyer")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if chk.Status != constants.ConflictStatusNoConflict || !chk.CanProceed {
		t.Fatalf("want no_conflict/proceed, got %s", chk.Status)
	}
	// 之后事务所新登记一个未结案件的对方，恰为赵六。
	seedOpenCaseWithOpposing(t, env, "C9", "赵六", "555")

	recheck, err := env.conflictSvc.ReadConclusion("", "赵六", "555", "")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if recheck.Status != constants.ConflictStatusPendingReview || recheck.CanProceed {
		t.Fatalf("want pending/not proceed after new hit, got %s proceed=%v", recheck.Status, recheck.CanProceed)
	}
}

// TestConflictRejectIsTerminal 驳回为终态，重复提交不翻案。
func TestConflictRejectIsTerminal(t *testing.T) {
	env := setupConflict(t)
	seedOpenCaseWithOpposing(t, env, "C1", "钱七", "777")
	chk, err := env.conflictSvc.Submit(submitReq("新案E", "钱七", "777"), 9, "lawyer")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := env.conflictSvc.Reject(chk.ID, "存在直接利益冲突，禁止接案", 1, "admin"); err != nil {
		t.Fatalf("reject: %v", err)
	}
	again, err := env.conflictSvc.Submit(submitReq("新案E", "钱七", "777"), 9, "lawyer")
	if err != nil {
		t.Fatalf("resubmit: %v", err)
	}
	if again.ID != chk.ID || again.Status != constants.ConflictStatusRejected || again.CanProceed {
		t.Fatalf("reject must be terminal unique state, got id=%d %s proceed=%v", again.ID, again.Status, again.CanProceed)
	}
	// 驳回态不允许再次放行。
	if _, err := env.conflictSvc.Release(chk.ID, "尝试翻案", 1, "admin"); err == nil {
		t.Fatal("release on rejected must be rejected")
	}
}

// TestConflictReleasedClearsWhenCaseClosed 放行后所冲突案件结案：冲突消除，收口为无冲突、可办理。
func TestConflictReleasedClearsWhenCaseClosed(t *testing.T) {
	env := setupConflict(t)
	c, _ := seedOpenCaseWithOpposing(t, env, "C1", "周九", "999")

	chk, err := env.conflictSvc.Submit(submitReq("新案G", "周九", "999"), 9, "lawyer")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := env.conflictSvc.Release(chk.ID, "批准接案", 1, "admin"); err != nil {
		t.Fatalf("release: %v", err)
	}
	// 现存案件结案，对方不再构成未结冲突。
	if err := env.db.Model(c).Update("status", constants.CaseStatusClosed).Error; err != nil {
		t.Fatalf("close: %v", err)
	}
	got, err := env.conflictSvc.ReadConclusion("", "周九", "999", "")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.Status != constants.ConflictStatusNoConflict || !got.CanProceed {
		t.Fatalf("want no_conflict/proceed after case closed, got %s proceed=%v", got.Status, got.CanProceed)
	}
}

// TestConflictLookupFromSameEntry 提交后始终能从同一入口（对方姓名/证件号）读回唯一结论。
func TestConflictLookupFromSameEntry(t *testing.T) {
	env := setupConflict(t)
	seedOpenCaseWithOpposing(t, env, "C1", "吴十", "1010")

	chk, err := env.conflictSvc.Submit(submitReq("新案H", "吴十", "1010"), 9, "lawyer")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	byIdentity, err := env.conflictSvc.ReadConclusion("", " 吴 十 ", "1010", "")
	if err != nil {
		t.Fatalf("lookup normalized: %v", err)
	}
	if byIdentity.ID != chk.ID {
		t.Fatalf("lookup must read back same record %d vs %d", byIdentity.ID, chk.ID)
	}
	byNo, err := env.conflictSvc.ReadConclusion("", "", "", chk.CheckNo)
	if err != nil {
		t.Fatalf("lookup by no: %v", err)
	}
	if byNo.ID != chk.ID {
		t.Fatalf("lookup by check_no must read back same record")
	}
}

func TestConflictClosedCaseNotMatched(t *testing.T) {
	env := setupConflict(t)
	c, p := seedOpenCaseWithOpposing(t, env, "C1", "孙八", "888")
	if err := env.db.Model(c).Update("status", constants.CaseStatusClosed).Error; err != nil {
		t.Fatalf("close case: %v", err)
	}
	_ = p
	chk, err := env.conflictSvc.Submit(submitReq("新案F", "孙八", "888"), 9, "lawyer")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if chk.Status != constants.ConflictStatusNoConflict || chk.HitCount != 0 {
		t.Fatalf("closed case must not match, got %s hit=%d", chk.Status, chk.HitCount)
	}
}

// TestConflictNewHitOnAnotherCaseReopensRelease 放行后另一未结案件登记相同对方：
// 命中集合变化，旧放行失效并回到待复核；重复读回稳定且只产生一条结论。
func TestConflictNewHitOnAnotherCaseReopensRelease(t *testing.T) {
	env := setupConflict(t)
	seedOpenCaseWithOpposing(t, env, "C1", "王大明", "440300198505056789")

	chk, err := env.conflictSvc.Submit(submitReq("新案J", "王大明", "440300198505056789"), 9, "lawyer")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := env.conflictSvc.Release(chk.ID, "批准接案", 1, "admin"); err != nil {
		t.Fatalf("release: %v", err)
	}
	// 另一个未结案件登记了相同姓名+证件号的对方。
	seedOpenCaseWithOpposing(t, env, "C2", "王大明", "440300198505056789")

	first, err := env.conflictSvc.ReadConclusion("", "王大明", "440300198505056789", "")
	if err != nil {
		t.Fatalf("read1: %v", err)
	}
	if first.Status != constants.ConflictStatusPendingReview || first.CanProceed {
		t.Fatalf("want pending/not proceed, got %s proceed=%v", first.Status, first.CanProceed)
	}
	if first.HitCount != 2 {
		t.Fatalf("want 2 live hits, got %d", first.HitCount)
	}

	// 重复读回：状态稳定、不回退为可办理，且结论仍只有一条。
	second, err := env.conflictSvc.ReadConclusion("", "王大明", "440300198505056789", "")
	if err != nil {
		t.Fatalf("read2: %v", err)
	}
	if second.ID != chk.ID || second.Status != constants.ConflictStatusPendingReview || second.CanProceed {
		t.Fatalf("repeat read must stay pending on same row, got id=%d %s proceed=%v", second.ID, second.Status, second.CanProceed)
	}
	var n int64
	if err := env.db.Model(&model.ConflictCheck{}).Count(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("repeat reads must not create extra conclusions, got %d rows", n)
	}
}

// TestConflictRejectedStableUnderNewHit 驳回终态在命中集合变化时保持稳定、不可办理、仍唯一。
func TestConflictRejectedStableUnderNewHit(t *testing.T) {
	env := setupConflict(t)
	seedOpenCaseWithOpposing(t, env, "C1", "钱七", "777")
	chk, err := env.conflictSvc.Submit(submitReq("新案K", "钱七", "777"), 9, "lawyer")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := env.conflictSvc.Reject(chk.ID, "直接利益冲突，禁止接案", 1, "admin"); err != nil {
		t.Fatalf("reject: %v", err)
	}
	seedOpenCaseWithOpposing(t, env, "C2", "钱七", "777")
	got, err := env.conflictSvc.ReadConclusion("", "钱七", "777", "")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.Status != constants.ConflictStatusRejected || got.CanProceed {
		t.Fatalf("reject must stay terminal/not proceed, got %s proceed=%v", got.Status, got.CanProceed)
	}
	var n int64
	_ = env.db.Model(&model.ConflictCheck{}).Count(&n).Error
	if n != 1 {
		t.Fatalf("must remain single conclusion, got %d", n)
	}
}

// TestConflictReleaseBindsCurrentProfileVersion 待复核期间档案已变更：放行锚定到放行时刻版本，可办理。
func TestConflictReleaseBindsCurrentProfileVersion(t *testing.T) {
	env := setupConflict(t)
	_, party := seedOpenCaseWithOpposing(t, env, "C1", "王大明", "440300198505056789")

	chk, err := env.conflictSvc.Submit(submitReq("新案I", "王大明", "440300198505056789"), 9, "lawyer")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	// 待复核期间对方改名（身份字段变化，档案版本推进到 2）。
	if _, err := env.partySvc.Update(party.ID, "王二明", "440300198505056789", "", ""); err != nil {
		t.Fatalf("party update: %v", err)
	}
	// 证件号不变仍命中同一档案；管理员据「当前档案」放行，结论锚定到放行时刻版本 v2。
	rel, err := env.conflictSvc.Release(chk.ID, "管理员据当前档案放行", 1, "admin")
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if rel.Status != constants.ConflictStatusReleased || !rel.CanProceed || rel.BoundPartyVersion != 2 {
		t.Fatalf("want released/proceed/bound v2, got %s proceed=%v bound=%d", rel.Status, rel.CanProceed, rel.BoundPartyVersion)
	}
	// 放行之后档案再变化，旧放行才失效。
	if _, err := env.partySvc.Update(party.ID, "王三明", "440300198505056789", "", ""); err != nil {
		t.Fatalf("party update2: %v", err)
	}
	again, err := env.conflictSvc.Get(chk.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if again.Status != constants.ConflictStatusInvalidated || again.CanProceed {
		t.Fatalf("post-release change must invalidate, got %s proceed=%v", again.Status, again.CanProceed)
	}
}

func asApp(err error, target **util.AppError) (bool, *util.AppError) {
	for err != nil {
		if ae, ok := err.(*util.AppError); ok {
			*target = ae
			return true, ae
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false, nil
		}
		err = u.Unwrap()
	}
	return false, nil
}
