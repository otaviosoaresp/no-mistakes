package types

import "slices"

// AgentPurpose names the duty one agent invocation serves within a step.
//
// A step can invoke the agent for more than one job, and those jobs differ
// enough in what they need that an operator may want to tune them separately:
// the reviewer reads a whole diff adversarially, its fixer applies findings it
// was handed, and the housekeeping pass edits documentation. Every purpose is
// recorded per invocation in agent_invocations.purpose, and agent_config uses
// this same vocabulary to override model and effort per duty.
//
// A purpose is either a step's own name - the default when a step does not say
// otherwise - or one of the named duties below.
type AgentPurpose string

const (
	// PurposeReviewFix is the review step's fixer turn: it applies findings a
	// previous review round produced, rather than judging the change itself.
	PurposeReviewFix AgentPurpose = "review-fix"
	// PurposeTestFix is the test step's repair turn.
	PurposeTestFix AgentPurpose = "test-fix"
	// PurposeLintFix is the lint step's repair turn.
	PurposeLintFix AgentPurpose = "lint-fix"
	// PurposeDocumentFix is the document step's repair turn.
	PurposeDocumentFix AgentPurpose = "document-fix"
	// PurposeHousekeeping is the document step's combined documentation and
	// lint pass, used when no deterministic lint command is configured.
	PurposeHousekeeping AgentPurpose = "housekeeping"
)

// namedPurposes are the duties that are not simply a step's own name.
var namedPurposes = []AgentPurpose{
	PurposeReviewFix,
	PurposeTestFix,
	PurposeLintFix,
	PurposeDocumentFix,
	PurposeHousekeeping,
}

// AllAgentPurposes returns every purpose an invocation can carry: one per
// pipeline step, plus the named duties. The order is stable so help text and
// error messages do not shuffle between runs.
func AllAgentPurposes() []AgentPurpose {
	steps := AllSteps()
	out := make([]AgentPurpose, 0, len(steps)+len(namedPurposes))
	for _, step := range steps {
		out = append(out, AgentPurpose(step))
	}
	out = append(out, namedPurposes...)
	slices.Sort(out)
	return out
}

// AgentPurposeNames returns AllAgentPurposes as strings, for config validation
// messages.
func AgentPurposeNames() []string {
	purposes := AllAgentPurposes()
	out := make([]string, 0, len(purposes))
	for _, p := range purposes {
		out = append(out, string(p))
	}
	return out
}

// KnownAgentPurpose reports whether name is a purpose this pipeline can emit.
// Config validation uses it so a misspelled purpose is a load error rather than
// an override that silently never applies.
func KnownAgentPurpose(name string) bool {
	for _, p := range AllAgentPurposes() {
		if string(p) == name {
			return true
		}
	}
	return false
}
