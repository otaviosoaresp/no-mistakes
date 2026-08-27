package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// purposeStubAgent stands in for a built adapter: it reports which purpose-built
// instance served an invocation.
type purposeStubAgent struct {
	label  string
	mu     sync.Mutex
	ran    []string
	closed int
}

func (r *purposeStubAgent) Run(_ context.Context, opts RunOpts) (*Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ran = append(r.ran, opts.Purpose)
	return &Result{Text: r.label}, nil
}

func (r *purposeStubAgent) Name() string { return r.label }

func (r *purposeStubAgent) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed++
	return nil
}

func (r *purposeStubAgent) runCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.ran)
}

func runPurpose(t *testing.T, a Agent, purpose string) string {
	t.Helper()
	res, err := a.Run(context.Background(), RunOpts{Purpose: purpose})
	if err != nil {
		t.Fatalf("run %q: %v", purpose, err)
	}
	return res.Text
}

// TestWithPurposeProfiles_RoutesNarrowedPurposes is the core behavior: only a
// purpose the operator tuned gets its own instance.
func TestWithPurposeProfiles_RoutesNarrowedPurposes(t *testing.T) {
	base := &purposeStubAgent{label: "base"}
	builds := 0
	wrapped := WithPurposeProfiles(base, map[string]bool{"review-fix": true}, func(purpose string) (Agent, error) {
		builds++
		return &purposeStubAgent{label: "built:" + purpose}, nil
	})

	if got := runPurpose(t, wrapped, "review-fix"); got != "built:review-fix" {
		t.Errorf("narrowed purpose ran on %q, want its own instance", got)
	}
	for _, purpose := range []string{"review", "test", ""} {
		if got := runPurpose(t, wrapped, purpose); got != "base" {
			t.Errorf("purpose %q ran on %q, want base", purpose, got)
		}
	}
	if builds != 1 {
		t.Errorf("builds = %d, want 1 - only the narrowed purpose is built", builds)
	}
	if base.runCount() != 3 {
		t.Errorf("base served %d invocations, want 3", base.runCount())
	}
}

// TestWithPurposeProfiles_BuildsEachPurposeOnce keeps a multi-round step from
// reconstructing its adapter on every invocation.
func TestWithPurposeProfiles_BuildsEachPurposeOnce(t *testing.T) {
	builds := map[string]int{}
	wrapped := WithPurposeProfiles(&purposeStubAgent{label: "base"}, map[string]bool{"review": true, "review-fix": true},
		func(purpose string) (Agent, error) {
			builds[purpose]++
			return &purposeStubAgent{label: "built:" + purpose}, nil
		})

	for range 3 {
		runPurpose(t, wrapped, "review")
		runPurpose(t, wrapped, "review-fix")
	}
	if builds["review"] != 1 || builds["review-fix"] != 1 {
		t.Errorf("builds = %v, want one per purpose", builds)
	}
}

// TestWithPurposeProfiles_FallsBackWhenABuildFails keeps a misconfigured
// override from taking the run down: the base profile is always a valid
// configuration, and it is the one the run would have used anyway.
func TestWithPurposeProfiles_FallsBackWhenABuildFails(t *testing.T) {
	base := &purposeStubAgent{label: "base"}
	attempts := 0
	wrapped := WithPurposeProfiles(base, map[string]bool{"housekeeping": true}, func(string) (Agent, error) {
		attempts++
		return nil, errors.New("cannot express model")
	})

	for range 3 {
		if got := runPurpose(t, wrapped, "housekeeping"); got != "base" {
			t.Fatalf("failed build ran on %q, want base", got)
		}
	}
	if attempts != 1 {
		t.Errorf("build attempts = %d, want 1 - a failed build is not retried per invocation", attempts)
	}
}

// TestWithPurposeProfiles_UnwrappedWithoutNarrowedPurposes is the
// compatibility floor: no purposes configured means no wrapper at all.
func TestWithPurposeProfiles_UnwrappedWithoutNarrowedPurposes(t *testing.T) {
	base := &purposeStubAgent{label: "base"}
	for _, narrowed := range []map[string]bool{nil, {}} {
		if got := WithPurposeProfiles(base, narrowed, func(string) (Agent, error) { return nil, nil }); got != Agent(base) {
			t.Errorf("WithPurposeProfiles with %v narrowed returned a wrapper, want the base agent itself", narrowed)
		}
	}
}

// TestWithPurposeProfiles_ClosesEveryBuiltAgent guards against leaking an
// adapter the wrapper built but the caller never sees.
func TestWithPurposeProfiles_ClosesEveryBuiltAgent(t *testing.T) {
	base := &purposeStubAgent{label: "base"}
	built := map[string]*purposeStubAgent{}
	wrapped := WithPurposeProfiles(base, map[string]bool{"review": true, "housekeeping": true}, func(purpose string) (Agent, error) {
		a := &purposeStubAgent{label: "built:" + purpose}
		built[purpose] = a
		return a, nil
	})

	runPurpose(t, wrapped, "review")
	runPurpose(t, wrapped, "housekeeping")
	if err := wrapped.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if base.closed != 1 {
		t.Errorf("base closed %d times, want 1", base.closed)
	}
	for purpose, a := range built {
		if a.closed != 1 {
			t.Errorf("purpose %q agent closed %d times, want 1", purpose, a.closed)
		}
	}
}

// sessionCapableAgent reports the optional capabilities a decorator must
// forward, so wrapping cannot silently disable session resume or the gate
// instruction opt-out.
type sessionCapableAgent struct {
	purposeStubAgent
}

func (s *sessionCapableAgent) SupportsSessionResume() bool         { return true }
func (s *sessionCapableAgent) SupportsSessionProvider(string) bool { return true }
func (s *sessionCapableAgent) ReportsAgentAttempts() bool          { return true }
func (s *sessionCapableAgent) NeutralizesGateInstructions() bool   { return true }

func TestWithPurposeProfiles_ForwardsCapabilities(t *testing.T) {
	base := &sessionCapableAgent{purposeStubAgent: purposeStubAgent{label: "base"}}
	wrapped := WithPurposeProfiles(base, map[string]bool{"review": true}, func(string) (Agent, error) {
		// A per-purpose instance that reports nothing must not be able to
		// downgrade what the wrapper advertises: capabilities are a property of
		// the harness, which a purpose never changes.
		return &purposeStubAgent{label: "plain"}, nil
	})

	if !SupportsSessionResume(wrapped) {
		t.Error("session resume was hidden by the purpose wrapper")
	}
	if !SupportsSessionProvider(wrapped, "claude") {
		t.Error("session provider match was hidden by the purpose wrapper")
	}
	if !ReportsAgentAttempts(wrapped) {
		t.Error("attempt reporting was hidden by the purpose wrapper")
	}
	if err := EnsureGateNeutralized(wrapped); err != nil {
		t.Errorf("gate neutralization was hidden by the purpose wrapper: %v", err)
	}
	if wrapped.Name() != "base" {
		t.Errorf("Name() = %q, want the base agent's name", wrapped.Name())
	}
}
