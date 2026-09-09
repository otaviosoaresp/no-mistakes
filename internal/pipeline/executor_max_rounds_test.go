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
		respondWhenReady(t, exec, types.ActionFix)
		waitForCallCount(t, &callCount, int64(round))
	}
	waitForStepStatus(t, database, run.ID, types.StepReview, types.StepStatusFixReview)

	respondWhenReady(t, exec, types.ActionApprove)
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

// respondWhenReady closes the small handoff between a step call returning and
// the executor publishing its next approval slot. The production API correctly
// rejects a response before that slot exists; this helper waits for that
// documented readiness instead of making the test depend on goroutine timing.
func respondWhenReady(t *testing.T, exec *Executor, action types.ApprovalAction) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := exec.Respond(types.StepReview, action, []string{"r1"}); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("executor did not publish an approval slot")
}

// TestExecutor_MaxRoundsRefusesAgentFixRoundOnARecoveredGate is the recovery
// half of the budget. Resume drives a gate that was parked when the daemon
// went down, and it answers ActionFix on its own path rather than through
// executeStep's gate loop - so without its own check the budget is enforced
// only while the daemon that started the run is still alive, and every restart
// buys one more round of the loop max_rounds exists to bound.
func TestExecutor_MaxRoundsRefusesAgentFixRoundOnARecoveredGate(t *testing.T) {
	database, p, run, repo := setupTest(t)

	if err := database.UpdateRunStatus(run.ID, types.RunRunning); err != nil {
		t.Fatal(err)
	}
	stepResult, err := database.InsertStepResult(run.ID, types.StepReview)
	if err != nil {
		t.Fatal(err)
	}
	// The budget is 2 and the crash left two rounds already recorded, so the
	// step is exactly spent: a fix here must be refused, not run.
	if err := database.StartStepWithLimits(stepResult.ID, 0, 2); err != nil {
		t.Fatal(err)
	}
	const reviewedHead = "4444444444444444444444444444444444444444"
	for round := 1; round <= 2; round++ {
		if _, err := database.InsertReviewStepRound(stepResult.ID, round, "initial", ptr(blockingFindings), nil, reviewedHead, 10); err != nil {
			t.Fatal(err)
		}
	}
	if err := database.SetStepFindings(stepResult.ID, blockingFindings); err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateStepStatusWithDuration(stepResult.ID, types.StepStatusFixReview, 10); err != nil {
		t.Fatal(err)
	}
	if err := database.SetRunAwaitingAgent(run.ID); err != nil {
		t.Fatal(err)
	}
	run, err = database.GetRun(run.ID)
	if err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		AutoFix:   config.AutoFix{Review: 0},
		MaxRounds: config.MaxRounds{Review: 2},
	}
	var callCount atomic.Int32
	step := &adaptiveCallStep{
		name: types.StepReview,
		fn: func(sctx *StepContext) (*StepOutcome, error) {
			callCount.Add(1)
			return &StepOutcome{NeedsApproval: true, AutoFixable: true, Findings: blockingFindings}, nil
		},
	}

	exec := NewExecutor(database, p, cfg, nil, []Step{step}, nil)
	done := make(chan error, 1)
	go func() { done <- exec.Resume(context.Background(), run, repo, t.TempDir()) }()

	respondEventually(t, exec, types.StepReview, types.ActionFix, []string{"r1"})
	waitForStepLog(t, p, run.ID, "fix refused: round budget spent (2/2)")

	if got := callCount.Load(); got != 0 {
		t.Fatalf("recovered gate executed the step %d time(s), want 0 - a spent budget must refuse the fix", got)
	}
	rounds, err := database.GetRoundsByStep(stepResult.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rounds) != 2 {
		t.Fatalf("rounds recorded = %d, want 2 - a refused fix must start no round", len(rounds))
	}

	// The gate is still open on the same findings, so approve/skip/abort remain
	// with whoever is driving the run.
	parked, err := database.GetStepResult(stepResult.ID)
	if err != nil {
		t.Fatal(err)
	}
	if parked.Status != types.StepStatusFixReview {
		t.Errorf("step status after a refused fix = %q, want the gate still open (%q)", parked.Status, types.StepStatusFixReview)
	}
	if parked.FindingsJSON == nil || !strings.Contains(*parked.FindingsJSON, `"severity":"error"`) {
		t.Errorf("findings = %v, want the blocking error finding preserved", parked.FindingsJSON)
	}
	if resumed, err := database.GetRun(run.ID); err != nil {
		t.Fatal(err)
	} else if resumed.AwaitingAgentSince == nil {
		t.Error("AwaitingAgentSince = nil after a refused fix, want the run still readable as parked")
	}

	respondEventually(t, exec, types.StepReview, types.ActionApprove, nil)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("resume: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("recovered run did not finish after approving the parked gate")
	}
	if got := callCount.Load(); got != 0 {
		t.Errorf("step executed %d time(s) overall, want 0", got)
	}
}

func ptr[T any](v T) *T { return &v }

// respondEventually retries until the executor is actually waiting on the gate,
// which Resume reaches asynchronously.
func respondEventually(t *testing.T, exec *Executor, step types.StepName, action types.ApprovalAction, findingIDs []string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if err := exec.Respond(step, action, findingIDs); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("executor never accepted %s on the %s gate", action, step)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
