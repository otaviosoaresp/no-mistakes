package cli

import (
	"strings"
	"testing"
)

// This fork closes upstream's release channel (FORK.md, "Upstream's release
// channel is not this build's update channel"), so `update` refuses for every
// build rather than only reporting a development build as unsupported. The
// flags must still parse and the command must still exit 0 without reaching
// the network.
const forkUpdateRefusal = "self-update is disabled for this private-use fork build"

func TestUpdateCommandRefusesForForkBuild(t *testing.T) {
	isolateUpdateCommand(t)

	out, err := executeCmd("update")
	if err != nil {
		t.Fatalf("update failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, forkUpdateRefusal) {
		t.Fatalf("unexpected update output: %s", out)
	}
	if !strings.Contains(out, "FORK.md") {
		t.Fatalf("refusal should point at the fork's build procedure: %s", out)
	}
}

func TestUpdateCommandBetaFlag(t *testing.T) {
	isolateUpdateCommand(t)

	out, err := executeCmd("update", "--beta")
	if err != nil {
		t.Fatalf("update --beta failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, forkUpdateRefusal) {
		t.Fatalf("unexpected update output: %s", out)
	}
}

func TestUpdateCommandYesFlag(t *testing.T) {
	isolateUpdateCommand(t)

	out, err := executeCmd("update", "-y")
	if err != nil {
		t.Fatalf("update -y failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, forkUpdateRefusal) {
		t.Fatalf("unexpected update output: %s", out)
	}
}

func isolateUpdateCommand(t *testing.T) {
	t.Helper()
	t.Setenv("NM_HOME", t.TempDir())
}
