package pipeline

import (
	"context"
	"testing"

	"github.com/kunchenguid/no-mistakes/internal/agent"
	"github.com/kunchenguid/no-mistakes/internal/types"
)

// purposeCapturingAgent records the purpose each invocation carried by the time
// it reached the adapter.
type purposeCapturingAgent struct {
	seen []string
}

func (p *purposeCapturingAgent) Run(_ context.Context, opts agent.RunOpts) (*agent.Result, error) {
	p.seen = append(p.seen, opts.Purpose)
	return &agent.Result{}, nil
}

func (p *purposeCapturingAgent) Name() string { return "capture" }
func (p *purposeCapturingAgent) Close() error { return nil }

// TestPerfRecordingAgent_DefaultsPurposeBeforeDelegating is the regression for
// the wiring gap that made per-purpose tuning unreachable for half the
// pipeline: several steps invoke the agent without naming a purpose, and
// defaulting it only while writing the telemetry row left the adapter - and so
// the per-purpose dispatcher - seeing an empty string it could never match.
func TestPerfRecordingAgent_DefaultsPurposeBeforeDelegating(t *testing.T) {
	inner := &purposeCapturingAgent{}
	recorder := &perfRecordingAgent{
		inner:    inner,
		stepName: types.StepCI,
		round:    func() int { return 1 },
	}

	if _, err := recorder.Run(context.Background(), agent.RunOpts{}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(inner.seen) != 1 || inner.seen[0] != string(types.StepCI) {
		t.Fatalf("adapter saw purposes %v, want the step name %q", inner.seen, types.StepCI)
	}
}

// TestPerfRecordingAgent_KeepsAnExplicitPurpose proves the default never
// overwrites a duty a step named for itself.
func TestPerfRecordingAgent_KeepsAnExplicitPurpose(t *testing.T) {
	inner := &purposeCapturingAgent{}
	recorder := &perfRecordingAgent{
		inner:    inner,
		stepName: types.StepReview,
		round:    func() int { return 1 },
	}

	if _, err := recorder.Run(context.Background(), agent.RunOpts{Purpose: string(types.PurposeReviewFix)}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(inner.seen) != 1 || inner.seen[0] != string(types.PurposeReviewFix) {
		t.Fatalf("adapter saw purposes %v, want %q", inner.seen, types.PurposeReviewFix)
	}
}
