package agent

import (
	"context"
	"fmt"
	"sync"
)

// PurposeBuilder constructs the agent to serve one purpose. It is called at
// most once per distinct purpose; the result is cached for the run.
type PurposeBuilder func(purpose string) (Agent, error)

// WithPurposeProfiles routes each invocation to an agent built for that
// invocation's purpose.
//
// A profile becomes native CLI arguments when the adapter is constructed (see
// NewWithOptions), so one agent instance can only ever run one model and one
// effort. Per-purpose tuning therefore needs one instance per purpose rather
// than a field read at call time, and building them lazily means a run that
// only ever reviews never pays to construct the housekeeping agent.
//
// base serves every purpose the operator did not narrow, and is also the
// fallback whenever a build fails: a misconfigured override must not take the
// run down, and the base profile is a strictly valid configuration that the
// same run would have used before purposes existed.
//
// Only model and effort vary by purpose - never the harness - so every
// capability query answers from base rather than from the per-purpose
// instances, which are the same adapter type by construction.
//
// narrowed names the purposes an operator actually tuned. Every other purpose
// runs on base, because building it would produce an instance identical to
// base. An empty set leaves base unwrapped.
func WithPurposeProfiles(base Agent, narrowed map[string]bool, build PurposeBuilder) Agent {
	if base == nil || build == nil || len(narrowed) == 0 {
		return base
	}
	return &purposeAgent{base: base, narrowed: narrowed, build: build, cache: map[string]Agent{}}
}

type purposeAgent struct {
	base     Agent
	narrowed map[string]bool
	build    PurposeBuilder

	mu    sync.Mutex
	cache map[string]Agent
	// failed records purposes whose build already errored, so a broken
	// override costs one attempt per run instead of one per invocation.
	failed map[string]struct{}
}

func (p *purposeAgent) Run(ctx context.Context, opts RunOpts) (*Result, error) {
	return p.agentFor(opts.Purpose).Run(ctx, opts)
}

// agentFor resolves the instance serving purpose, building and caching it on
// first use. It never returns nil: an unbuildable purpose falls back to base.
func (p *purposeAgent) agentFor(purpose string) Agent {
	if !p.narrowed[purpose] {
		return p.base
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if cached, ok := p.cache[purpose]; ok {
		return cached
	}
	if _, tried := p.failed[purpose]; tried {
		return p.base
	}
	built, err := p.build(purpose)
	if err != nil || built == nil {
		if p.failed == nil {
			p.failed = map[string]struct{}{}
		}
		p.failed[purpose] = struct{}{}
		return p.base
	}
	p.cache[purpose] = built
	return built
}

func (p *purposeAgent) Name() string { return p.base.Name() }

// Close closes every agent this wrapper owns. The base is closed last and its
// error is the reported one, since it is the instance every unnarrowed purpose
// ran on.
func (p *purposeAgent) Close() error {
	p.mu.Lock()
	built := make([]Agent, 0, len(p.cache))
	for _, a := range p.cache {
		built = append(built, a)
	}
	p.cache = map[string]Agent{}
	p.mu.Unlock()

	var firstErr error
	for _, a := range built {
		if err := a.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("close purpose agent: %w", err)
		}
	}
	if err := p.base.Close(); err != nil {
		return err
	}
	return firstErr
}

// The capability set is a property of the harness, which purposes never
// change, so these answer from base for every purpose.

func (p *purposeAgent) SupportsSessionResume() bool {
	return SupportsSessionResume(p.base)
}

func (p *purposeAgent) SupportsSessionProvider(provider string) bool {
	return SupportsSessionProvider(p.base, provider)
}

func (p *purposeAgent) ReportsAgentAttempts() bool {
	return ReportsAgentAttempts(p.base)
}

// NeutralizesGateInstructions is a security boundary, so it answers from base
// like the rest: a purpose changes model and effort, never which adapter runs
// or the raw args that decide whether its neutralization knob is in force. The
// builder still re-checks each purpose-built chain, so a build that could not
// hold this guarantee is discarded rather than run.
func (p *purposeAgent) NeutralizesGateInstructions() bool {
	return NeutralizesGateInstructions(p.base)
}
