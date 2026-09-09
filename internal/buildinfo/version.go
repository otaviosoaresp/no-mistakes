package buildinfo

import (
	"runtime/debug"
	"strings"
)

// Set via ldflags at build time:
//
//	-ldflags "-X github.com/kunchenguid/no-mistakes/internal/buildinfo.Version=v1.0.0
//	          -X github.com/kunchenguid/no-mistakes/internal/buildinfo.Commit=abc1234
//	          -X github.com/kunchenguid/no-mistakes/internal/buildinfo.Date=2024-01-01
//	          -X github.com/kunchenguid/no-mistakes/internal/buildinfo.TelemetryHost=https://a.example.com
//	          -X github.com/kunchenguid/no-mistakes/internal/buildinfo.TelemetryWebsiteID=abc123"
var (
	Version            = "dev"
	Commit             = "unknown"
	Date               = "unknown"
	TelemetryHost      = ""
	TelemetryWebsiteID = ""
)

// ForkSuffix marks every binary built from this repository as the private-use
// fork described in FORK.md.
//
// A fork build is cut from an upstream tag, so `git describe` alone produces a
// string indistinguishable from an ordinary upstream development build - and
// the installed CLI and the daemon are the same binary, so "is the thing
// running right now our build?" has to be answerable from the version alone.
// The marker is applied here rather than in the Makefile so it survives every
// way this tree is built: `make build`, a bare `go build`, and the
// cross-compile recipe in FORK.md.
//
// It is semver build metadata (everything from "+" on), which parsing and
// comparison discard, so it changes no version decision anywhere - it only
// rides every surface that reports the version: `--version`, `doctor`, the run
// record's no_mistakes_version, the eval capture manifest, and telemetry.
const ForkSuffix = "+fork"

func CurrentVersion() string {
	return withForkSuffix(resolveVersion())
}

func resolveVersion() string {
	if Version != "" && Version != "dev" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if info.Main.Version != "" && info.Main.Version != "(devel)" {
			return info.Main.Version
		}
	}
	return "dev"
}

// withForkSuffix leaves the bare "dev" identity alone. telemetry.buildChannel
// treats any version other than "" and "dev" as a release channel, so marking a
// development build would switch on remote telemetry for local builds.
func withForkSuffix(version string) string {
	if version == "" || version == "dev" || strings.Contains(version, ForkSuffix) {
		return version
	}
	return version + ForkSuffix
}

func String() string {
	return CurrentVersion() + " (" + Commit + ") " + Date
}
