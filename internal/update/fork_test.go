package update

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A fork build has no upstream update channel: upstream releases carry none of
// its patches, and their version numbers are always ahead of a build cut from
// an older tag. Every remote seam must therefore stay silent and offline, and
// the self-update command - the one the notice used to advertise - must refuse
// rather than replace the fork binary with an upstream one.

func forkNoticeUpdater(t *testing.T, stderr *bytes.Buffer, spawned *bool) *updater {
	t.Helper()
	cachePath := filepath.Join(t.TempDir(), "update-check.json")
	if err := writeCache(cachePath, &checkCache{
		CheckedAt:     time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC),
		LatestVersion: "v1.73.0",
	}); err != nil {
		t.Fatal(err)
	}
	return &updater{
		appName:        "no-mistakes",
		currentVersion: "v1.66.0-14-gc30f8cf",
		cachePath:      cachePath,
		stderr:         stderr,
		forkBuild:      true,
		now:            func() time.Time { return time.Date(2026, 9, 9, 13, 0, 0, 0, time.UTC) },
		spawnBackground: func(string) error {
			*spawned = true
			return nil
		},
	}
}

func TestForkBuildNeverNotifiesOrRefreshesFromUpstream(t *testing.T) {
	stderr := new(bytes.Buffer)
	spawned := false
	u := forkNoticeUpdater(t, stderr, &spawned)

	u.maybeNotifyAndCheck([]string{"status"})

	if stderr.Len() != 0 {
		t.Fatalf("fork build printed an update notice: %q", stderr.String())
	}
	if spawned {
		t.Fatal("fork build spawned a background update check")
	}
}

func TestForkBuildReportsNoCachedLatestVersionToTheTUI(t *testing.T) {
	stderr := new(bytes.Buffer)
	spawned := false
	u := forkNoticeUpdater(t, stderr, &spawned)

	if got := u.cachedLatestVersion(); got != "" {
		t.Fatalf("cachedLatestVersion() = %q, want empty for a fork build", got)
	}
}

func TestForkBuildRefusesSelfUpdateWithoutReachingTheNetwork(t *testing.T) {
	stdout := new(bytes.Buffer)
	u := &updater{
		appName:        "no-mistakes",
		currentVersion: "v1.66.0-14-gc30f8cf",
		forkBuild:      true,
		stdout:         stdout,
		// No apiBaseURL and no httpClient: any attempt to reach upstream
		// would panic or error rather than pass silently.
	}

	if err := u.run(context.Background()); err != nil {
		t.Fatalf("run() = %v, want nil", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "self-update is disabled for this private-use fork build") {
		t.Fatalf("stdout = %q", out)
	}
	if !strings.Contains(out, "FORK.md") {
		t.Fatalf("refusal should point at the fork's build procedure, got %q", out)
	}
}

func TestForkBuildBackgroundCheckFlagIsAcceptedButDoesNothing(t *testing.T) {
	u := &updater{forkBuild: true}
	if !u.forkBuild {
		t.Fatal("fixture")
	}
	// MaybeHandleBackgroundCheck still claims the flag so an older installed
	// binary's spawned `--update-check` invocation exits cleanly instead of
	// falling through to the cobra root as an unknown command.
	handled, err := MaybeHandleBackgroundCheck([]string{backgroundFlag, "v1.66.0"})
	if !handled {
		t.Fatal("background check flag should still be claimed")
	}
	if err != nil {
		t.Fatalf("MaybeHandleBackgroundCheck() = %v, want nil", err)
	}
}

func TestDefaultUpdaterIsAForkBuild(t *testing.T) {
	u, err := defaultUpdater(new(bytes.Buffer), new(bytes.Buffer))
	if err != nil {
		t.Fatalf("defaultUpdater() = %v", err)
	}
	if !u.forkBuild {
		t.Fatal("every real binary built from this repository must be marked as a fork build")
	}
}
