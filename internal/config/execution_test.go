package config

import (
	"strings"
	"testing"
)

// An executor this binary does not link is refused at load, not at the first publish: a repo that
// names improve's run server must not quietly run in a local container instead.
func TestValidateExecution_RefusesAnExecutorThisBuildLacks(t *testing.T) {
	ex := Execution{Executor: "improve", Image: "oci://x@sha256:00"}
	err := validateExecution(ex, Environment{})
	if err == nil || !strings.Contains(err.Error(), "not available in this build") {
		t.Fatalf("got %v", err)
	}
	ex.Executor = ""
	if err := validateExecution(ex, Environment{}); err != nil {
		t.Fatalf("the default executor must be accepted: %v", err)
	}
}
