package config

import (
	"strings"
	"testing"

	"github.com/kunchenguid/no-mistakes/internal/agentcfg"
	"github.com/kunchenguid/no-mistakes/internal/types"
)

// TestPurposeProfile_NarrowsOverTheAgentBase is the core rule: a purpose entry
// is a delta on the agent's own profile, not a replacement, so lowering effort
// for one duty keeps the model the operator pinned for that agent.
func TestPurposeProfile_NarrowsOverTheAgentBase(t *testing.T) {
	cfg := writeGlobalConfig(t, `agent_config:
  claude:
    model: claude-opus-5
    effort: xhigh
    purposes:
      review-fix:
        effort: medium
      housekeeping:
        model: claude-sonnet-5
        effort: low
`)
	merged := Merge(cfg, &RepoConfig{})

	if got := merged.AgentProfileFor(types.AgentClaude); got.Model != "claude-opus-5" || got.Effort != agentcfg.EffortXHigh {
		t.Errorf("base profile = %#v, want the agent's own model and effort", got)
	}

	fix := merged.AgentProfileForPurpose(types.AgentClaude, string(types.PurposeReviewFix))
	if fix.Effort != agentcfg.EffortMedium {
		t.Errorf("review-fix effort = %q, want medium", fix.Effort)
	}
	if fix.Model != "claude-opus-5" {
		t.Errorf("review-fix model = %q, want the base model to survive an effort-only override", fix.Model)
	}

	house := merged.AgentProfileForPurpose(types.AgentClaude, string(types.PurposeHousekeeping))
	if house.Model != "claude-sonnet-5" || house.Effort != agentcfg.EffortLow {
		t.Errorf("housekeeping profile = %#v, want both fields overridden", house)
	}
}

// TestPurposeProfile_UnnarrowedPurposeGetsTheBase pins that an invocation whose
// duty nobody configured behaves exactly as it did before purposes existed.
func TestPurposeProfile_UnnarrowedPurposeGetsTheBase(t *testing.T) {
	cfg := writeGlobalConfig(t, `agent_config:
  claude:
    effort: xhigh
    purposes:
      housekeeping:
        effort: low
`)
	merged := Merge(cfg, &RepoConfig{})

	for _, purpose := range []string{string(types.StepReview), string(types.StepTest), ""} {
		if got := merged.AgentProfileForPurpose(types.AgentClaude, purpose); got.Effort != agentcfg.EffortXHigh {
			t.Errorf("purpose %q effort = %q, want the base xhigh", purpose, got.Effort)
		}
	}
}

// TestPurposeProfile_AbsentIsBackwardsCompatible is the compatibility floor:
// an agent_config written before purposes existed resolves identically through
// both accessors, and reports no purpose profiles at all.
func TestPurposeProfile_AbsentIsBackwardsCompatible(t *testing.T) {
	cfg := writeGlobalConfig(t, "agent_config:\n  claude:\n    effort: high\n")
	merged := Merge(cfg, &RepoConfig{})

	if len(merged.NarrowedPurposes()) != 0 {
		t.Errorf("NarrowedPurposes() = %v, want none configured", merged.NarrowedPurposes())
	}
	base := merged.AgentProfileFor(types.AgentClaude)
	if got := merged.AgentProfileForPurpose(types.AgentClaude, string(types.PurposeReviewFix)); got != base {
		t.Errorf("purpose profile = %#v, want the base %#v", got, base)
	}
}

func TestPurposeProfile_NarrowedPurposes(t *testing.T) {
	cfg := writeGlobalConfig(t, `agent_config:
  claude:
    purposes:
      review-fix:
        effort: low
`)
	if got := Merge(cfg, &RepoConfig{}).NarrowedPurposes(); !got["review-fix"] {
		t.Errorf("NarrowedPurposes() = %v, want review-fix", got)
	}
}

// TestPurposeProfile_RejectsUnknownPurpose keeps a typo from becoming an
// override that silently never matches an invocation.
func TestPurposeProfile_RejectsUnknownPurpose(t *testing.T) {
	_, err := LoadGlobalFromBytes([]byte("agent_config:\n  claude:\n    purposes:\n      reviewfix:\n        effort: low\n"))
	if err == nil {
		t.Fatal("an unknown purpose must fail the config load")
	}
	if !strings.Contains(err.Error(), "reviewfix") || !strings.Contains(err.Error(), "review-fix") {
		t.Errorf("error should name the bad value and the valid vocabulary, got: %v", err)
	}
}

func TestPurposeProfile_RejectsBadEffort(t *testing.T) {
	_, err := LoadGlobalFromBytes([]byte("agent_config:\n  claude:\n    purposes:\n      review:\n        effort: turbo\n"))
	if err == nil {
		t.Fatal("an invalid effort inside a purpose must fail the config load")
	}
	if !strings.Contains(err.Error(), "purposes.review") {
		t.Errorf("error should locate the offending purpose, got: %v", err)
	}
}

// TestPurposeProfile_RejectsKnobTheHarnessCannotExpress proves a purpose cannot
// smuggle past the validation the base profile faces.
func TestPurposeProfile_RejectsKnobTheHarnessCannotExpress(t *testing.T) {
	_, err := LoadGlobalFromBytes([]byte("agent_config:\n  antigravity:\n    purposes:\n      review:\n        model: some-model\n"))
	if err == nil {
		t.Fatal("a model on a harness that cannot express one must fail the load")
	}
	if !strings.Contains(err.Error(), "purposes.review") {
		t.Errorf("error should locate the offending purpose, got: %v", err)
	}
}

// TestPurposeProfile_EveryStepNameIsAValidPurpose guards the vocabulary against
// drift: a step whose name the config refuses would be untunable.
func TestPurposeProfile_EveryStepNameIsAValidPurpose(t *testing.T) {
	for _, step := range types.AllSteps() {
		if !types.KnownAgentPurpose(string(step)) {
			t.Errorf("step %q is not a valid purpose", step)
		}
	}
}

// TestPurposeProfile_IsGlobalOnly keeps purposes on the same footing as the
// rest of agent_config: it decides which model runs with the operator's
// credentials, so no pushed branch may set it.
func TestPurposeProfile_IsGlobalOnly(t *testing.T) {
	repo, err := LoadRepoFromBytes([]byte("agent_config:\n  claude:\n    purposes:\n      review:\n        effort: low\n"))
	if err != nil {
		t.Fatalf("a repo config naming agent_config should parse and ignore it: %v", err)
	}
	if got := Merge(DefaultGlobalConfig(), repo).AgentProfileForPurpose(types.AgentClaude, string(types.StepReview)); !got.IsZero() {
		t.Errorf("repo purpose profile = %#v, want zero - agent_config is global-only", got)
	}
}
