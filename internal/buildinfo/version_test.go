package buildinfo

import (
	"strings"
	"testing"
)

func TestCurrentVersionMarksAStampedBuildAsFork(t *testing.T) {
	t.Cleanup(func(prev string) func() {
		return func() { Version = prev }
	}(Version))

	Version = "v1.73.0-15-gc30f8cf"
	got := CurrentVersion()
	if got != "v1.73.0-15-gc30f8cf"+ForkSuffix {
		t.Fatalf("CurrentVersion() = %q, want the stamped version plus %q", got, ForkSuffix)
	}
}

func TestCurrentVersionDoesNotDoubleMarkAnAlreadyMarkedBuild(t *testing.T) {
	t.Cleanup(func(prev string) func() {
		return func() { Version = prev }
	}(Version))

	Version = "v1.73.0" + ForkSuffix
	if got := CurrentVersion(); got != "v1.73.0"+ForkSuffix {
		t.Fatalf("CurrentVersion() = %q, want it unchanged", got)
	}
}

// telemetry.buildChannel reports any version other than "" and "dev" as a
// release channel, and that channel gates whether a build without an explicit
// host/website ID emits remote telemetry at all. Marking a development build
// would silently switch that on for local builds.
func TestCurrentVersionLeavesADevelopmentBuildUnmarked(t *testing.T) {
	t.Cleanup(func(prev string) func() {
		return func() { Version = prev }
	}(Version))

	Version = "dev"
	if got := CurrentVersion(); strings.Contains(got, ForkSuffix) {
		t.Fatalf("CurrentVersion() = %q, want no fork marker on a development build", got)
	}
}

func TestStringCarriesTheForkMarker(t *testing.T) {
	t.Cleanup(func(prev string) func() {
		return func() { Version = prev }
	}(Version))

	Version = "v1.73.0"
	if got := String(); !strings.Contains(got, ForkSuffix) {
		t.Fatalf("String() = %q, want it to carry %q", got, ForkSuffix)
	}
}
