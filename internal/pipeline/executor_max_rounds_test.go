package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kunchenguid/no-mistakes/internal/config"
	"github.com/kunchenguid/no-mistakes/internal/paths"
	"github.com/kunchenguid/no-mistakes/internal/types"
)

// blockingFindings keeps the gate open every round, standing in for a reviewer
// that substantiates new findings on each pass and so never converges on its
// own.
const blockingFindings = `{"findings":[{"id":"r1","severity":"error","description":"bug","action":"auto-fix"}],"summary":"1 issue"}`

// TestExecutor_MaxRoundsRefusesFurtherAgentFixRounds is the regression for the
// unbounded review loop: auto_fix caps only the automatic fix rounds, so an
// agent answering `--action fix` at every gate could loop without end, each
// round paying for a full re-read of the branch diff.
func TestExecutor_MaxRoundsRefusesFurtherAgentFixRounds(t *testing.T) {
	database, p, run, repo := setupTest(t)
	workDir := t.TempDir()

	cfg := &config.Config{
		AutoFix:   config.AutoFix{Review: 0},
		MaxRounds: config.MaxRounds{Review: 2},
	}

	callCount := 0
	step := &adaptiveCallStep{
		name: types.StepReview,
		fn: func(sctx *StepContext) (*StepOutcome, error) {
			callCount++
			return &StepOutcome{NeedsApproval: true, AutoFixable: true, Findings: blockingFindings}, nil
		},
	}

	exec := NewExecutor(database, p, cfg, nil, []Step{step}, nil)
	done := make(chan error, 1)
	go func() { done <- exec.Execute(context.Background(), run, repo, workDir) }()

	// Round 1 gates; the budget still has a round left, so fix is honored.
	waitForStepStatus(t, database, run.ID, types.StepReview, types.StepStatusAwaitingApproval)
	exec.Respond(types.StepReview, types.ActionFix, []string{"r1"})

	// Round 2 gates as a fix review. The budget is now spent.
	waitForStepStatus(t, database, run.ID, types.StepReview, types.StepStatusFixReview)

	// A fix here must be refused, not executed: the step must not run a third
	// time, and the same gate must still be open afterwards.
	exec.Respond(types.StepReview, types.ActionFix, []string{"r1"})
	waitForStepLog(t, p, run.ID, "fix refused: round budget spent (2/2)")

	if callCount != 2 {
		t.Fatalf("step executed %d times, want 2 - a refused fix must not start another round", callCount)
	}
	steps, err := database.GetStepsByRun(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := steps[0].Status; got != types.StepStatusFixReview {
		t.Errorf("step status after a refused fix = %q, want the gate still open (%q)", got, types.StepStatusFixReview)
	}

	// The findings were never downgraded, and the decision is still available.
	if steps[0].FindingsJSON == nil || !strings.Contains(*steps[0].FindingsJSON, `"severity":"error"`) {
		t.Errorf("findings = %v, want the blocking error finding preserved", steps[0].FindingsJSON)
	}

	exec.Respond(types.StepReview, types.ActionApprove, nil)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run did not finish after approving the parked gate")
	}
	if callCount != 2 {
		t.Errorf("step executed %d times overall, want 2", callCount)
	}
}

// TestExecutor_MaxRoundsCapsAutomaticFixRounds proves the budget bounds the
// automatic loop too, not just agent-driven fix rounds, so a generous
// auto_fix limit cannot outrun it.
func TestExecutor_MaxRoundsCapsAutomaticFixRounds(t *testing.T) {
	database, p, run, repo := setupTest(t)
	workDir := t.TempDir()

	cfg := &config.Config{
		AutoFix:   config.AutoFix{Review: 10},
		MaxRounds: config.MaxRounds{Review: 3},
	}

	callCount := 0
	step := &adaptiveCallStep{
		name: types.StepReview,
		fn: func(sctx *StepContext) (*StepOutcome, error) {
			callCount++
			return &StepOutcome{NeedsApproval: true, AutoFixable: true, Findings: blockingFindings}, nil
		},
	}

	exec := NewExecutor(database, p, cfg, nil, []Step{step}, nil)
	done := make(chan error, 1)
	go func() { done <- exec.Execute(context.Background(), run, repo, workDir) }()

	// Rounds 1 and 2 auto-fix without gating; round 3 spends the budget and
	// parks instead of starting a fourth round.
	waitForStepStatus(t, database, run.ID, types.StepReview, types.StepStatusFixReview)
	waitForStepLog(t, p, run.ID, "round budget spent (3/3)")
	if callCount != 3 {
		t.Fatalf("step executed %d times, want 3 - auto-fix must stop at the round budget", callCount)
	}

	exec.Respond(types.StepReview, types.ActionApprove, nil)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run did not finish after approving the parked gate")
	}
}

// TestExecutor_MaxRoundsUnlimitedByDefault pins the historical behavior: with
// no budget configured, an agent can keep asking for fix rounds.
func TestExecutor_MaxRoundsUnlimitedByDefault(t *testing.T) {
	database, p, run, repo := setupTest(t)
	workDir := t.TempDir()

	cfg := &config.Config{AutoFix: config.AutoFix{Review: 0}}

	var callCount atomic.Int64
	step := &adaptiveCallStep{
		name: types.StepReview,
		fn: func(sctx *StepContext) (*StepOutcome, error) {
			callCount.Add(1)
			return &StepOutcome{NeedsApproval: true, AutoFixable: true, Findings: blockingFindings}, nil
		},
	}

	exec := NewExecutor(database, p, cfg, nil, []Step{step}, nil)
	done := make(chan error, 1)
	go func() { done <- exec.Execute(context.Background(), run, repo, workDir) }()

	// Wait on the execution count rather than the gate status: consecutive fix
	// rounds park at the same fix_review status, so a status wait would return
	// on the previous round's gate and race ahead of the new one.
	waitForCallCount(t, &callCount, 1)
	for round := 2; round <= 4; round++ {
		exec.Respond(types.StepReview, types.ActionFix, []string{"r1"})
		waitForCallCount(t, &callCount, int64(round))
	}
	waitForStepStatus(t, database, run.ID, types.StepReview, types.StepStatusFixReview)

	exec.Respond(types.StepReview, types.ActionApprove, nil)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run did not finish")
	}
}

// TestExecutor_PersistsEffectiveMaxRounds proves status surfaces can tell the
// driver how much budget the active step was started with.
func TestExecutor_PersistsEffectiveMaxRounds(t *testing.T) {
	database, p, run, repo := setupTest(t)
	workDir := t.TempDir()
	cfg := &config.Config{MaxRounds: config.MaxRounds{Review: 4}}

	step := &adaptiveCallStep{
		name: types.StepReview,
		fn: func(sctx *StepContext) (*StepOutcome, error) {
			return &StepOutcome{ExitCode: 0}, nil
		},
	}

	exec := NewExecutor(database, p, cfg, nil, []Step{step}, nil)
	if err := exec.Execute(context.Background(), run, repo, workDir); err != nil {
		t.Fatalf("execute: %v", err)
	}

	steps, err := database.GetStepsByRun(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if steps[0].MaxRounds == nil || *steps[0].MaxRounds != 4 {
		t.Fatalf("max rounds = %v, want 4", steps[0].MaxRounds)
	}
}

// TestExecutor_PersistsUnlimitedMaxRoundsAsNull keeps "unlimited" reading back
// the same way for a step started with no budget and for a row written before
// the column existed.
func TestExecutor_PersistsUnlimitedMaxRoundsAsNull(t *testing.T) {
	database, p, run, repo := setupTest(t)
	workDir := t.TempDir()

	step := &adaptiveCallStep{
		name: types.StepReview,
		fn: func(sctx *StepContext) (*StepOutcome, error) {
			return &StepOutcome{ExitCode: 0}, nil
		},
	}

	exec := NewExecutor(database, p, &config.Config{}, nil, []Step{step}, nil)
	if err := exec.Execute(context.Background(), run, repo, workDir); err != nil {
		t.Fatalf("execute: %v", err)
	}

	steps, err := database.GetStepsByRun(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if steps[0].MaxRounds != nil {
		t.Fatalf("max rounds = %v, want NULL for an unlimited step", *steps[0].MaxRounds)
	}
}

// waitForStepLog blocks until want appears in a step log for the run, which is
// where the executor records that it refused a fix or parked a spent budget.
func waitForStepLog(t *testing.T, p *paths.Paths, runID, want string) {
	t.Helper()
	dir := p.RunLogDir(runID)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		entries, err := os.ReadDir(dir)
		if err == nil {
			for _, e := range entries {
				if !strings.HasSuffix(e.Name(), ".log") {
					continue
				}
				data, readErr := os.ReadFile(filepath.Join(dir, e.Name()))
				if readErr == nil && strings.Contains(string(data), want) {
					return
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("step log never contained %q (looked in %s)", want, dir)
}

// waitForCallCount blocks until the step has been executed want times.
func waitForCallCount(t *testing.T, count *atomic.Int64, want int64) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if got := count.Load(); got >= want {
			if got > want {
				t.Fatalf("step executed %d times, want %d", got, want)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("step executed %d times, want %d", count.Load(), want)
}
