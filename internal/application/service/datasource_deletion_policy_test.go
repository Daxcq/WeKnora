package service

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestDeletionPolicyDefaultsToRetain(t *testing.T) {
	ds := &types.DataSource{}
	normalizeDeletionPolicy(ds)
	if ds.DeletionPolicy != types.DeletionPolicyRetain {
		t.Fatalf("expected retain, got %q", ds.DeletionPolicy)
	}
}

func TestDeletionPolicyValidation(t *testing.T) {
	if !validDeletionPolicy(types.DeletionPolicyRetain) || !validDeletionPolicy(types.DeletionPolicyDelete) {
		t.Fatal("expected supported deletion policies to validate")
	}
	if validDeletionPolicy("hard_delete") {
		t.Fatal("unexpected unsupported deletion policy")
	}
}
