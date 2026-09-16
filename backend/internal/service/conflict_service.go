package service

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"cylawcase/internal/conflict"
	"cylawcase/internal/constants"
	"cylawcase/internal/dto"
	"cylawcase/internal/model"
	"cylawcase/internal/repository"
	"cylawcase/internal/util"

	"gorm.io/gorm"
)

// ConflictService 案源利益冲突检查业务逻辑。
type ConflictService struct {
	repo      *repository.ConflictCheckRepository
	partyRepo *repository.CasePartyRepository
	db        *gorm.DB
	logger    *slog.Logger
}

// NewConflictService 构造冲突检查服务。
func NewConflictService(repo *repository.ConflictCheckRepository, partyRepo *repository.CasePartyRepository,
	db *gorm.DB, logger *slog.Logger) *ConflictService {
	return &ConflictService{repo: repo, partyRepo: partyRepo, db: db, logger: logger}
}

// Submit 提交新案冲突检查。收口维度是「新案」（case_key）：同一新案重复提交收口为一条结论；
// 不同新案即使对方姓名/证件号相同，也各自独立成行，互不覆盖。
func (s *ConflictService) Submit(req dto.ConflictSubmitRequest, submitterID uint64, submitterName string) (*model.ConflictCheck, error) {
	caseKey := strings.TrimSpace(req.CaseKey)
	if caseKey == "" {
		return nil, util.NewAppError(constants.CodeValidationFailed, "ConflictCheck[case_key] submit: case_key required")
	}
	normName := conflict.Normalize(req.OppName)
	normID := conflict.Normalize(req.OppIDNumber)
	if normName == "" {
		return nil, util.NewAppError(constants.CodeValidationFailed, "ConflictCheck[opp_name] submit: name required")
	}
	identityKey := conflict.IdentityKey(normName, normID)
	ours := make([]model.OurParty, 0, len(req.OurParties))
	for _, p := range req.OurParties {
		ours = append(ours, model.OurParty{
			Name: strings.TrimSpace(p.Name), IDNumber: strings.TrimSpace(p.IDNumber),
			Contact: strings.TrimSpace(p.Contact), PartyRole: strings.TrimSpace(p.PartyRole),
		})
	}

	// 并发提交同一新案：事务内查重后仍可能同时 INSERT 撞 case_key 唯一索引。
	// 败方捕获唯一冲突后按 case_key 回读收口，必要时有限重试，绝不新建重复行或产生重复主键。
	const maxAttempts = 3
	var result *model.ConflictCheck
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		forceExisting := attempt > 1
		var err error
		result, err = s.upsert(caseKey, identityKey, normName, normID, strings.TrimSpace(req.OppName), strings.TrimSpace(req.OppIDNumber),
			req.CaseTitle, ours, submitterID, submitterName, forceExisting)
		if err == nil {
			return result, nil
		}
		if !isDuplicateKey(err) {
			s.logger.Error(constants.LogConflictSubmitFailed, "error", err.Error())
			return nil, util.Wrap(err, "ConflictCheck[case_key=%s] submit failed", caseKey)
		}
		s.logger.Info(constants.LogConflictIdempotentReuse, "case_key", caseKey, "attempt", attempt)
	}
	// 极端并发下重试仍冲突：让调用方稍后重发，不写入任何重复结论。
	return nil, util.NewAppError(constants.CodeConflictStateConflict,
		"ConflictCheck[case_key="+caseKey+"] submit: concurrent conflict, please retry")
}

// upsert 在事务内完成「按新案查重 → 实时匹配 → 收口到该新案唯一终态」。
func (s *ConflictService) upsert(caseKey, identityKey, normName, normID, oppName, oppID, title string, ours []model.OurParty,
	submitterID uint64, submitterName string, forceExisting bool) (*model.ConflictCheck, error) {

	var out *model.ConflictCheck
	err := s.db.Transaction(func(tx *gorm.DB) error {
		chkRepo := s.repo.WithTx(tx)
		partyRepo := s.partyRepo.WithTx(tx)

		candidates, err := partyRepo.FindOpposingCandidates(normName, normID)
		if err != nil {
			return err
		}
		hits := conflict.Match(oppName, oppID, candidates)
		snapshots := make(model.PartySnapshotJSON, 0, len(hits))
		for _, h := range hits {
			snapshots = append(snapshots, conflict.ToSnapshot(h))
		}
		boundVersion, boundFP := conflict.Binding(snapshots)
		hit := len(hits) > 0

		existing, err := chkRepo.FindByCaseKey(caseKey)
		if err != nil && err != repository.ErrNotFound {
			return err
		}
		notFound := err == repository.ErrNotFound
		if notFound && forceExisting {
			// 并发撞键回读后仍未查到，交由上层重试/报错，绝不重复插入。
			return repository.ErrNotFound
		}

		if notFound {
			status := constants.ConflictStatusNoConflict
			if hit {
				status = constants.ConflictStatusPendingReview
			}
			chk := &model.ConflictCheck{
				CheckNo:           genConflictNo(),
				CaseKey:           caseKey,
				CaseTitle:         title,
				OurParties:        model.OurPartyJSON(ours),
				OppName:           oppName,
				OppIDNumber:       oppID,
				IdentityKey:       identityKey,
				NormOppName:       normName,
				NormOppID:         normID,
				Status:            status,
				HitCount:          len(hits),
				MatchedSnapshot:   snapshots,
				BoundPartyVersion: boundVersion,
				BoundFingerprint:  boundFP,
				SubmitByID:        submitterID,
				SubmitByName:      submitterName,
			}
			if err := chkRepo.Create(chk); err != nil {
				return err
			}
			if err := chkRepo.ReplaceBindings(chk.ID, snapshots); err != nil {
				return err
			}
			tpl := constants.LogConflictSubmitNoHit
			if hit {
				tpl = constants.LogConflictSubmitHit
			}
			s.logger.Info(tpl, "check_id", chk.ID, "case_key", caseKey, "hit", hit)
			out = chk
			return nil
		}

		// 已存在（同一新案重复提交）：仅刷新该案内容与实时快照，再据状态机收口；不触碰其他新案记录。
		// 先用库中现存的旧绑定判定放行是否仍锚定同一档案版本，再覆盖为本次快照。
		bindingOK := conflict.BindingUnchanged(existing.BoundPartyVersion, existing.BoundFingerprint, snapshots)
		existing.CaseTitle = title
		existing.OurParties = model.OurPartyJSON(ours)
		existing.OppName = oppName
		existing.OppIDNumber = oppID
		existing.IdentityKey = identityKey
		existing.NormOppName = normName
		existing.NormOppID = normID
		existing.HitCount = len(hits)
		existing.MatchedSnapshot = snapshots
		existing.BoundPartyVersion = boundVersion
		existing.BoundFingerprint = boundFP

		existing.Status = conflict.CollapseStatus(existing.Status, hit, bindingOK)
		switch existing.Status {
		case constants.ConflictStatusInvalidated:
			existing.InvalidatedReason = constants.MsgConflictStale
		case constants.ConflictStatusPendingReview, constants.ConflictStatusNoConflict:
			// 回到待复核/无冲突：清空上一轮审查痕迹，等待重新复核。
			existing.InvalidatedReason = ""
			existing.ReviewBasis = ""
			existing.ReviewByID = 0
			existing.ReviewByName = ""
			existing.ReviewedAt = nil
		}

		if err := chkRepo.Save(existing); err != nil {
			return err
		}
		if err := chkRepo.ReplaceBindings(existing.ID, snapshots); err != nil {
			return err
		}
		s.logger.Info(constants.LogConflictIdempotentReuse, "check_id", existing.ID, "status", existing.Status)
		out = existing
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.enrich(out)
}

// Get 按 ID 读回唯一结论，并做实时版本复核（档案变化即吊销旧放行）。
func (s *ConflictService) Get(id uint64) (*model.ConflictCheck, error) {
	chk, err := s.repo.FindByID(id)
	if err != nil {
		return nil, util.Wrap(err, "ConflictCheck[id=%d] get failed", id)
	}
	return s.revalidate(chk)
}

// ReadConclusion 读回结论：优先按新案键 case_key 精确读回；其次按检查单号；
// 仅给对方姓名/证件号时返回该对方最近一条结论（同一对方可能对应多个不同新案）。
func (s *ConflictService) ReadConclusion(caseKey, oppName, oppID, checkNo string) (*model.ConflictCheck, error) {
	var chk *model.ConflictCheck
	var err error
	switch {
	case strings.TrimSpace(caseKey) != "":
		chk, err = s.repo.FindByCaseKey(strings.TrimSpace(caseKey))
	case strings.TrimSpace(checkNo) != "":
		chk, err = s.repo.FindByCheckNo(strings.TrimSpace(checkNo))
	case conflict.Normalize(oppName) != "":
		key := conflict.IdentityKey(conflict.Normalize(oppName), conflict.Normalize(oppID))
		chk, err = s.repo.FindLatestByIdentity(key)
	default:
		return nil, util.NewAppError(constants.CodeBadRequest, "ConflictCheck read: case_key or opp_name or check_no required")
	}
	if err != nil {
		return nil, util.Wrap(err, "ConflictCheck read failed")
	}
	return s.revalidate(chk)
}

// List 分页查询待复核/全部记录。
func (s *ConflictService) List(page, pageSize int, status string) ([]model.ConflictCheck, int64, error) {
	list, total, err := s.repo.List(page, pageSize, status)
	if err != nil {
		return nil, 0, util.Wrap(err, "ConflictCheck list failed")
	}
	for i := range list {
		if _, err := s.enrich(&list[i]); err != nil {
			s.logger.Error(constants.LogConflictRevalidateStale, "error", err.Error())
		}
	}
	return list, total, nil
}

// Release 管理员填写依据后放行。仅待复核可放行；档案已变则拒绝沿用旧结论。
func (s *ConflictService) Release(id uint64, basis string, adminID uint64, adminName string) (*model.ConflictCheck, error) {
	return s.decide(id, strings.TrimSpace(basis), adminID, adminName, true)
}

// Reject 管理员填写依据后驳回。
func (s *ConflictService) Reject(id uint64, basis string, adminID uint64, adminName string) (*model.ConflictCheck, error) {
	return s.decide(id, strings.TrimSpace(basis), adminID, adminName, false)
}

func (s *ConflictService) decide(id uint64, basis string, adminID uint64, adminName string, release bool) (*model.ConflictCheck, error) {
	if basis == "" {
		return nil, util.NewAppError(constants.CodeConflictBasisRequired, constants.MsgConflictBasisRequired)
	}
	chk, err := s.repo.FindByID(id)
	if err != nil {
		return nil, util.Wrap(err, "ConflictCheck[id=%d] decision find failed", id)
	}
	chk, err = s.revalidate(chk)
	if err != nil {
		return nil, err
	}

	target := constants.ConflictStatusReleased
	okLog, failLog := constants.LogConflictReleaseSuccess, constants.LogConflictReleaseFailed
	if !release {
		target = constants.ConflictStatusRejected
		okLog, failLog = constants.LogConflictRejectSuccess, constants.LogConflictRejectFailed
	}

	// 已处于相同终态：幂等返回，不重复写入。
	if chk.Status == target {
		return chk, nil
	}
	if chk.Status == constants.ConflictStatusInvalidated {
		return nil, util.NewAppError(constants.CodeConflictDecisionStale, "ConflictCheck[id="+u64(id)+"] decision: "+constants.MsgConflictStale)
	}
	if !conflict.CanDecide(chk.Status) {
		return nil, util.NewAppError(constants.CodeConflictStateConflict,
			fmt.Sprintf("ConflictCheck[id=%d] decision forbidden in status=%s", id, chk.Status))
	}

	now := time.Now()
	chk.Status = target
	chk.ReviewByID = adminID
	chk.ReviewByName = adminName
	chk.ReviewBasis = basis
	chk.ReviewedAt = &now
	chk.InvalidatedReason = ""

	// 放行锚定到「放行时刻」的当前档案版本：管理员据现状放行，之后档案再变化才令其失效。
	// 若此刻已无任何未结命中（冲突在待复核期间消除），不应放行一个不存在的冲突，需重新提交收口为无冲突。
	if release {
		if !chk.LiveMatch || len(chk.LiveSnapshot) == 0 {
			return nil, util.NewAppError(constants.CodeConflictStateConflict,
				fmt.Sprintf("ConflictCheck[id=%d] release: no live opposing hit, please resubmit", id))
		}
		chk.MatchedSnapshot = chk.LiveSnapshot
		chk.HitCount = len(chk.LiveSnapshot)
		v, fp := conflict.Binding(chk.LiveSnapshot)
		chk.BoundPartyVersion = v
		chk.BoundFingerprint = fp
	}

	if err := s.repo.Save(chk); err != nil {
		s.logger.Error(failLog, "error", err.Error())
		return nil, util.Wrap(err, "ConflictCheck[id=%d] decision save failed", id)
	}
	if release {
		if err := s.repo.ReplaceBindings(chk.ID, chk.LiveSnapshot); err != nil {
			return nil, util.Wrap(err, "ConflictCheck[id=%d] decision bindings failed", id)
		}
	}
	s.logger.Info(okLog, "check_id", id, "admin", adminName)
	return s.enrich(chk)
}

// revalidate 读回时复核：
//   - 放行后档案在未结案件中被改动：吊销为 invalidated（旧放行不得沿用，须重新复核）。
//   - 放行后冲突已消除（所冲突案件结案/档案删除）：收口为 no_conflict。
//   - 无冲突结论下出现新的未结命中：回到 pending_review。
func (s *ConflictService) revalidate(chk *model.ConflictCheck) (*model.ConflictCheck, error) {
	enriched, err := s.enrich(chk)
	if err != nil {
		return nil, err
	}

	var nextStatus string
	switch {
	// 命中集合出现放行时未绑定的新对方档案（另一未结案件登记了相同姓名/证件号）：
	// 旧放行不能再覆盖新冲突，失效并回到待复核。
	case chk.Status == constants.ConflictStatusReleased && enriched.HasNewHit:
		nextStatus = constants.ConflictStatusPendingReview
	case chk.Status == constants.ConflictStatusReleased && enriched.Stale && enriched.StaleReason == model.StaleReasonProfileChanged:
		nextStatus = constants.ConflictStatusInvalidated
	case chk.Status == constants.ConflictStatusReleased && enriched.Stale && enriched.StaleReason == model.StaleReasonCleared:
		nextStatus = constants.ConflictStatusNoConflict
	case chk.Status == constants.ConflictStatusNoConflict && enriched.LiveMatch:
		nextStatus = constants.ConflictStatusPendingReview
	default:
		return enriched, nil
	}

	chk.HitCount = len(enriched.LiveSnapshot)
	chk.MatchedSnapshot = enriched.LiveSnapshot
	chk.Status = nextStatus
	if nextStatus == constants.ConflictStatusInvalidated {
		chk.InvalidatedReason = constants.MsgConflictStale
		chk.ReviewBasis = ""
	} else {
		chk.InvalidatedReason = ""
		chk.ReviewBasis = ""
		chk.ReviewByID = 0
		chk.ReviewByName = ""
		chk.ReviewedAt = nil
	}
	if err := s.repo.Save(chk); err != nil {
		return nil, util.Wrap(err, "ConflictCheck[id=%d] revalidate save failed", chk.ID)
	}
	if err := s.repo.ReplaceBindings(chk.ID, enriched.LiveSnapshot); err != nil {
		return nil, util.Wrap(err, "ConflictCheck[id=%d] revalidate bindings failed", chk.ID)
	}
	s.logger.Info(constants.LogConflictRevalidateStale, "check_id", chk.ID, "status", chk.Status, "reason", enriched.StaleReason)
	return s.enrich(chk)
}

// enrich 用当前档案数据填充实时派生字段（不写库）。
func (s *ConflictService) enrich(chk *model.ConflictCheck) (*model.ConflictCheck, error) {
	// 实时未结命中（供 live_match 与最新快照）。
	candidates, err := s.partyRepo.FindOpposingCandidates(chk.NormOppName, chk.NormOppID)
	if err != nil {
		return nil, util.Wrap(err, "ConflictCheck[id=%d] enrich failed", chk.ID)
	}
	hits := conflict.Match(chk.OppName, chk.OppIDNumber, candidates)
	live := make(model.PartySnapshotJSON, 0, len(hits))
	for _, h := range hits {
		live = append(live, conflict.ToSnapshot(h))
	}
	chk.LiveMatch = len(hits) > 0
	chk.LiveSnapshot = live
	chk.Stale = false
	chk.StaleReason = ""
	chk.HasNewHit = false

	// 只有曾绑定过档案版本的结论（released / 曾命中）才需要版本复核。
	if len(chk.MatchedSnapshot) == 0 {
		chk.CanProceed = chk.Status == constants.ConflictStatusNoConflict && !chk.LiveMatch
		return chk, nil
	}

	// 取绑定档案的现状（含已结案件/已删除的判定）。
	boundIDSet := make(map[uint64]struct{}, len(chk.MatchedSnapshot))
	boundIDs := make([]uint64, 0, len(chk.MatchedSnapshot))
	for _, sp := range chk.MatchedSnapshot {
		boundIDs = append(boundIDs, sp.PartyID)
		boundIDSet[sp.PartyID] = struct{}{}
	}
	// 实时命中里若出现绑定集合之外的档案，即「命中集合新增」（另一未结案件登记了相同姓名/证件号）。
	for _, sp := range live {
		if _, ok := boundIDSet[sp.PartyID]; !ok {
			chk.HasNewHit = true
			break
		}
	}
	current, err := s.partyRepo.FindCandidatesByPartyIDs(boundIDs)
	if err != nil {
		return nil, util.Wrap(err, "ConflictCheck[id=%d] enrich bound parties failed", chk.ID)
	}
	curByID := map[uint64]*model.PartyCandidate{}
	for i := range current {
		curByID[current[i].PartyID] = &current[i]
	}

	if chk.LiveMatch {
		// 仍有未结命中：只要任一绑定档案被改动（版本/指纹变化），即判旧放行失效。
		for _, snap := range chk.MatchedSnapshot {
			if cur, ok := curByID[snap.PartyID]; ok {
				if conflict.SnapshotStale(snap, cur, cur.CaseStatus) {
					// 版本推进但案件已结 -> 该档案已不构成冲突，不算改动导致的失效。
					if !conflict.IsOpenCase(cur.CaseStatus) {
						continue
					}
					chk.Stale = true
					chk.StaleReason = model.StaleReasonProfileChanged
					break
				}
			}
		}
	} else {
		// 无未结命中：判定是「档案被改动」还是「冲突已消除」。
		changedWhileOpen := false
		for _, snap := range chk.MatchedSnapshot {
			cur, ok := curByID[snap.PartyID]
			if !ok {
				// 档案被删除。无法确认是改名(改动)还是真删除；安全起见按失效处理。
				changedWhileOpen = true
				break
			}
			if conflict.IsOpenCase(cur.CaseStatus) {
				// 档案仍在未结案件中但已不在命中集合：身份字段被改动（改名/改证件号）。
				changedWhileOpen = true
				break
			}
			// 否则案件已结：属冲突消除。
		}
		if changedWhileOpen {
			chk.Stale = true
			chk.StaleReason = model.StaleReasonProfileChanged
		} else {
			chk.Stale = true
			chk.StaleReason = model.StaleReasonCleared
		}
	}

	chk.CanProceed = (chk.Status == constants.ConflictStatusReleased && !chk.Stale && !chk.HasNewHit) ||
		(chk.Status == constants.ConflictStatusNoConflict && !chk.LiveMatch)
	return chk, nil
}

// genConflictNo 生成冲突检查单号。
func genConflictNo() string {
	return fmt.Sprintf("CF%d%05d", time.Now().Year(), time.Now().UnixNano()%100000)
}

// isDuplicateKey 识别唯一索引冲突（避免在未开启 TranslateError 时强依赖驱动错误类型）。
// isDuplicateKey 方言无关地识别唯一约束冲突：Postgres 报 "duplicate key"，
// SQLite（modernc/mattn）报 "UNIQUE constraint failed"，另兼容 GORM 翻译后的 ErrDuplicatedKey。
func isDuplicateKey(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate key") || strings.Contains(msg, "unique constraint failed")
}
