package config

import (
	"testing"

	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/constants"
)

func TestTestrepoRevision_envSet(t *testing.T) {
	t.Setenv(constants.TESTREPO_REVISION_ENV, "abc123")
	if got := testrepoRevision(); got != "abc123" {
		t.Fatalf("got %q", got)
	}
}

func TestTestrepoRevision_defaultMain(t *testing.T) {
	t.Setenv(constants.TESTREPO_REVISION_ENV, "")
	if got := testrepoRevision(); got != "main" {
		t.Fatalf("got %q, want main", got)
	}
}
