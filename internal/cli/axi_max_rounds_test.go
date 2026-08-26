package cli

import (
	"strings"
	"testing"

	"github.com/kunchenguid/no-mistakes/internal/types"
)

const gateFindingsJSON = `{"findings":[{"id":"r1","severity":"error","file":"a.go","action":"auto-fix","description":"bug"}]}`

func renderGateHelp(t *testing.T, gate stepView) string {
	t.Helper()
	return axiDoc(gateFields(gate)...)
}

// TestGateWithdrawsFixOnceTheRoundBudgetIsSpent keeps the gate from offering an
// action the executor will refuse, and states why in the gate itself.
func TestGateWithdrawsFixOnceTheRoundBudgetIsSpent(t *testing.T) {
	out := renderGateHelp(t, stepView{
		Name:         string(types.StepReview),
		Status:       string(types.StepStatusAwaitingApproval),
		FindingsJSON: gateFindingsJSON,
		RoundCount:   4,
		MaxRounds:    4,
	})

	// Match the help command itself, not the round_budget note that explains
	// the refusal - that note names the action on purpose.
	if strings.Contains(out, "respond --action fix --findings") {
		t.Errorf("gate still offers the fix command with a spent round budget:\n%s", out)
	}
	for _, want := range []string{"--action approve", "--action skip", "round_budget", "spent (4/4)", "max_rounds.review"} {
		if !strings.Contains(out, want) {
			t.Errorf("gate output is missing %q:\n%s", want, out)
		}
	}
}

// TestGateKeepsFixWhileRoundBudgetRemains pins the unchanged gate for every run
// that has not spent its budget, including the unlimited default.
func TestGateKeepsFixWhileRoundBudgetRemains(t *testing.T) {
	tests := []struct {
		name string
		gate stepView
	}{
		{
			name: "unlimited",
			gate: stepView{Name: string(types.StepReview), FindingsJSON: gateFindingsJSON, RoundCount: 9},
		},
		{
			name: "budget_remaining",
			gate: stepView{Name: string(types.StepReview), FindingsJSON: gateFindingsJSON, RoundCount: 2, MaxRounds: 4},
		},
		{
			// A view built without round data reports 0 rounds; withholding
			// fix there would be a guess, and the executor refuses anyway.
			name: "unknown_round_count",
			gate: stepView{Name: string(types.StepReview), FindingsJSON: gateFindingsJSON, MaxRounds: 4},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := renderGateHelp(t, tt.gate)
			if !strings.Contains(out, "respond --action fix --findings") {
				t.Errorf("gate withheld --action fix:\n%s", out)
			}
			if strings.Contains(out, "round_budget") {
				t.Errorf("gate reported a spent budget it does not have:\n%s", out)
			}
		})
	}
}

// TestGateResolutionApprovesASpentBudget stops `--yes` from sending a fix the
// executor refuses, which would park the unattended run instead of converging.
func TestGateResolutionApprovesASpentBudget(t *testing.T) {
	gate := stepView{
		Name:         string(types.StepReview),
		Status:       string(types.StepStatusAwaitingApproval),
		FindingsJSON: gateFindingsJSON,
		RoundCount:   3,
		MaxRounds:    3,
	}
	action, ids := gateResolution(gate, false)
	if action != types.ActionApprove {
		t.Errorf("gateResolution = %q, want %q", action, types.ActionApprove)
	}
	if len(ids) != 0 {
		t.Errorf("gateResolution selected %v, want no findings", ids)
	}

	gate.RoundCount = 1
	if action, _ := gateResolution(gate, false); action != types.ActionFix {
		t.Errorf("gateResolution with budget left = %q, want %q", action, types.ActionFix)
	}
}
