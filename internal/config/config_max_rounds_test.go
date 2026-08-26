package config

import (
	"testing"

	"github.com/kunchenguid/no-mistakes/internal/types"
)

func TestMaxRounds_UnlimitedByDefault(t *testing.T) {
	cfg := Merge(&GlobalConfig{}, &RepoConfig{})
	for _, step := range []types.StepName{types.StepReview, types.StepTest, types.StepLint, types.StepDocument, types.StepCI, types.StepRebase} {
		if got := cfg.MaxRoundsLimit(step); got != 0 {
			t.Errorf("MaxRoundsLimit(%s) = %d, want 0 (unlimited)", step, got)
		}
	}
}

func TestMaxRounds_RepoOverridesGlobal(t *testing.T) {
	global := &GlobalConfig{MaxRounds: MaxRoundsRaw{Review: intPtr(4), Test: intPtr(2)}}
	repo := &RepoConfig{MaxRounds: MaxRoundsRaw{Review: intPtr(6)}}

	cfg := Merge(global, repo)
	if got := cfg.MaxRoundsLimit(types.StepReview); got != 6 {
		t.Errorf("review budget = %d, want the repo override 6", got)
	}
	if got := cfg.MaxRoundsLimit(types.StepTest); got != 2 {
		t.Errorf("test budget = %d, want the global value 2 to survive a partial repo override", got)
	}
}

// TestMaxRounds_NegativeClampsToUnlimited keeps a typo from inverting the
// bound into something that refuses every fix round.
func TestMaxRounds_NegativeClampsToUnlimited(t *testing.T) {
	cfg := Merge(&GlobalConfig{MaxRounds: MaxRoundsRaw{Review: intPtr(-3)}}, &RepoConfig{})
	if got := cfg.MaxRoundsLimit(types.StepReview); got != 0 {
		t.Errorf("review budget = %d, want 0 for a negative value", got)
	}
}

// TestMaxRounds_ZeroIsAnExplicitUnlimitedOverride proves the pointer field
// distinguishes "not set" from "set to 0", so a repo can restore unlimited
// rounds against a global budget.
func TestMaxRounds_ZeroIsAnExplicitUnlimitedOverride(t *testing.T) {
	global := &GlobalConfig{MaxRounds: MaxRoundsRaw{Review: intPtr(3)}}
	repo := &RepoConfig{MaxRounds: MaxRoundsRaw{Review: intPtr(0)}}

	if got := Merge(global, repo).MaxRoundsLimit(types.StepReview); got != 0 {
		t.Errorf("review budget = %d, want 0 from the explicit repo override", got)
	}
}

func TestMaxRounds_ParsesFromGlobalYAML(t *testing.T) {
	global, err := LoadGlobalFromBytes([]byte("max_rounds:\n  review: 5\n  test: 2\n"))
	if err != nil {
		t.Fatalf("load global config: %v", err)
	}
	cfg := Merge(global, &RepoConfig{})
	if got := cfg.MaxRoundsLimit(types.StepReview); got != 5 {
		t.Errorf("review budget = %d, want 5", got)
	}
	if got := cfg.MaxRoundsLimit(types.StepTest); got != 2 {
		t.Errorf("test budget = %d, want 2", got)
	}
}

func TestMaxRounds_ParsesFromRepoYAML(t *testing.T) {
	repo, err := LoadRepoFromBytes([]byte("max_rounds:\n  review: 3\n"))
	if err != nil {
		t.Fatalf("parse repo config: %v", err)
	}
	if got := Merge(&GlobalConfig{}, repo).MaxRoundsLimit(types.StepReview); got != 3 {
		t.Errorf("review budget = %d, want 3", got)
	}
}

func intPtr(v int) *int { return &v }
