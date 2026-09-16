package service

import (
	"errors"
	"fmt"
	"testing"

	"gorm.io/gorm"
)

func TestIsDuplicateKey(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"postgres", errors.New("ERROR: duplicate key value violates unique constraint \"idx_conflict_case_key\" (SQLSTATE 23505)"), true},
		{"sqlite modernc", errors.New("UNIQUE constraint failed: conflict_checks.case_key (155)"), true},
		{"sqlite mattn", errors.New("constraint failed: UNIQUE constraint failed: conflict_checks.case_key (2067)"), true},
		{"gorm translated", gorm.ErrDuplicatedKey, true},
		{"wrapped gorm", fmt.Errorf("create conflict check: %w", gorm.ErrDuplicatedKey), true},
		{"not found", errors.New("record not found"), false},
		{"nil", nil, false},
	}
	for _, tc := range cases {
		if got := isDuplicateKey(tc.err); got != tc.want {
			t.Errorf("%s: isDuplicateKey=%v want %v", tc.name, got, tc.want)
		}
	}
}
