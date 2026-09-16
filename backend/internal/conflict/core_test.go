package conflict

import (
	"testing"

	"cylawcase/internal/constants"
	"cylawcase/internal/model"
)

func cand(id uint64, side, name, idNo, caseStatus string, version uint64) model.PartyCandidate {
	return model.PartyCandidate{
		PartyID: id, CaseID: id + 1000, CaseNo: "CY", CaseTitle: "t", CaseStatus: caseStatus,
		Side: side, Name: name, IDNumber: idNo, Version: version,
		NormName: Normalize(name), NormID: Normalize(idNo),
	}
}

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"  wang da ming ": "WANGDAMING",
		"王大明":             "王大明",
		"zhang\t san":     "ZHANGSAN",
		"9101 a":          "9101A",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q)=%q want %q", in, got, want)
		}
	}
}

func TestMatch(t *testing.T) {
	open := []model.PartyCandidate{
		cand(1, model.PartySideOpposing, "王大明", "440300198505056789", constants.CaseStatusFiled, 1),
		cand(2, model.PartySideOpposing, "李四", "", constants.CaseStatusInvestigating, 1),
		cand(3, model.PartySideOpposing, "王大明", "440300198505056789", constants.CaseStatusClosed, 1), // 已结，忽略
		cand(4, model.PartySideOur, "王大明", "440300198505056789", constants.CaseStatusFiled, 1),       // 本方，忽略
	}
	// 证件号命中：应只命中未结的 party 1（忽略已结/本方）。
	hits := Match("王大明", "440300198505056789", open)
	if len(hits) != 1 || hits[0].Candidate.PartyID != 1 || hits[0].MatchedBy != constants.MatchedByIDNumber {
		t.Fatalf("id match got %+v", ids(hits))
	}
	// 无证件号：按姓名精确命中 party 1（已结的 3、本方的 4 仍排除）。
	hits = Match("王大明", "", open)
	if len(hits) != 1 || hits[0].Candidate.PartyID != 1 || hits[0].MatchedBy != constants.MatchedByName {
		t.Fatalf("name match got %+v", ids(hits))
	}
	// 仅有姓名、无证件号的 party 2。
	hits = Match("李四", "", open)
	if len(hits) != 1 || hits[0].Candidate.PartyID != 2 {
		t.Fatalf("name-only match got %+v", ids(hits))
	}
	// 大小写/空格归一化后仍能证件命中。
	hits = Match(" 王 大 明 ", "440300198505 056789", open)
	if len(hits) != 1 {
		t.Fatalf("normalized match got %+v", ids(hits))
	}
	// 无任何命中。
	if hits := Match("赵六", "111", open); len(hits) != 0 {
		t.Fatalf("no match expected got %+v", ids(hits))
	}
}

func TestSnapshotStale(t *testing.T) {
	c := cand(7, model.PartySideOpposing, "王大明", "440300198505056789", constants.CaseStatusFiled, 1)
	snap := ToSnapshot(MatchHit{Candidate: c, MatchedBy: constants.MatchedByIDNumber})

	// 同版本同指纹：未失效。
	c2 := c
	if SnapshotStale(snap, &c2, constants.CaseStatusFiled) {
		t.Error("unchanged snapshot should not be stale")
	}
	// 版本推进：失效。
	c3 := c
	c3.Version = 2
	if !SnapshotStale(snap, &c3, constants.CaseStatusFiled) {
		t.Error("version bump should be stale")
	}
	// 指纹变化（改名/改证件号）：失效。
	c4 := c
	c4.Name = "王二明"
	c4.NormName = Normalize(c4.Name)
	if !SnapshotStale(snap, &c4, constants.CaseStatusFiled) {
		t.Error("identity change should be stale")
	}
	// 案件已结：失效。
	c5 := c
	if !SnapshotStale(snap, &c5, constants.CaseStatusClosed) {
		t.Error("closed case should be stale")
	}
	// 档案被删（current=nil）：失效。
	if !SnapshotStale(snap, nil, "") {
		t.Error("missing profile should be stale")
	}
}

func TestCollapseStatusUniqueTerminal(t *testing.T) {
	tt := []struct {
		name      string
		current   string
		hit       bool
		bindingOK bool
		want      string
	}{
		{"命中新建", constants.ConflictStatusNoConflict, true, false, constants.ConflictStatusPendingReview},
		{"放行稳定重复提交", constants.ConflictStatusReleased, true, true, constants.ConflictStatusReleased},
		{"放行后档案变更", constants.ConflictStatusReleased, true, false, constants.ConflictStatusPendingReview},
		{"放行后不再命中", constants.ConflictStatusReleased, false, false, constants.ConflictStatusNoConflict},
		{"驳回终态不翻案", constants.ConflictStatusRejected, true, true, constants.ConflictStatusRejected},
		{"待复核仍命中", constants.ConflictStatusPendingReview, true, false, constants.ConflictStatusPendingReview},
		{"待复核后解除命中", constants.ConflictStatusPendingReview, false, false, constants.ConflictStatusNoConflict},
		{"失效后重新命中", constants.ConflictStatusInvalidated, true, false, constants.ConflictStatusPendingReview},
	}
	for _, tc := range tt {
		if got := CollapseStatus(tc.current, tc.hit, tc.bindingOK); got != tc.want {
			t.Errorf("%s: got %s want %s", tc.name, got, tc.want)
		}
	}
	// 仅待复核可复核。
	if !CanDecide(constants.ConflictStatusPendingReview) {
		t.Error("pending should be decidable")
	}
	for _, s := range []string{constants.ConflictStatusReleased, constants.ConflictStatusRejected, constants.ConflictStatusInvalidated, constants.ConflictStatusNoConflict} {
		if CanDecide(s) {
			t.Errorf("%s should not be decidable", s)
		}
	}
}

func TestBindingUnchanged(t *testing.T) {
	c := cand(1, model.PartySideOpposing, "王大明", "440300198505056789", constants.CaseStatusFiled, 3)
	snaps := model.PartySnapshotJSON{ToSnapshot(MatchHit{Candidate: c, MatchedBy: constants.MatchedByIDNumber})}
	v, fp := Binding(snaps)
	if !BindingUnchanged(v, fp, snaps) {
		t.Error("same binding should be unchanged")
	}
	// 版本推进 -> 聚合版本变化。
	c2 := c
	c2.Version = 4
	snaps2 := model.PartySnapshotJSON{ToSnapshot(MatchHit{Candidate: c2, MatchedBy: constants.MatchedByIDNumber})}
	if BindingUnchanged(v, fp, snaps2) {
		t.Error("bumped version should change binding")
	}
}

func ids(hits []MatchHit) []uint64 {
	out := make([]uint64, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.Candidate.PartyID)
	}
	return out
}
