# Fork notes

This is a private-use fork of [`kunchenguid/no-mistakes`](https://github.com/kunchenguid/no-mistakes).

The changes here are **ours and stay ours**. They are not proposed upstream, and no pull request against `kunchenguid/no-mistakes` should be opened from this repository. Upstream issue numbers appear in commit messages only to record which reported problem a change addresses; they are references, not contributions.

The fork exists for one reason: token consumption. Measured over 94 local runs (`~/.no-mistakes/state.sqlite`, 731 agent invocations, 4.02B cache read + write tokens), the review step alone accounted for 59% of everything spent, and review rounds past the fourth accounted for 12% of the total on their own.

## What diverges from upstream

Every patch is additive and defaults to upstream behavior, so a fork build behaves exactly like the release it was built from until the new config is set.

### `max_rounds` - a total round budget per step

`auto_fix` caps only the fix rounds the executor starts by itself. An agent answering `axi respond --action fix` at every gate was unbounded, and the review loop has no fixed point when a thorough reviewer keeps substantiating new findings on a large change - upstream reports show runs reaching 21 and 27 review rounds over 6-8 hours without converging, each round re-reading the whole diff.

`max_rounds.<step>` bounds every round of a step in one run: the initial pass, each automatic fix round, and each round an agent asked for. `0` is unlimited and is the default.

When the budget is spent the step **parks**. It does not approve itself and it does not downgrade any finding's severity - only the fix action is withdrawn:

- the gate re-publishes without the `--action fix` help line and reports `round_budget: spent (N/M)`
- the executor refuses a fix that arrives anyway and re-publishes the same gate rather than running the step again
- `axi run --yes` approves such a gate, the same way it already approves one it has fixed once
- the effective budget is persisted per step result (`step_results.max_rounds`), mirroring `auto_fix_limit`

The refusal lives at **both** gate-driving sites. `Executor.Resume` drives a gate that was parked when the daemon went down, and it answers `ActionFix` on its own path rather than through `executeStep`'s gate loop, so it carries its own budget check and its own re-park. Without it the budget bound only the lifetime of the daemon that started the run and every restart bought one more round of the loop this patch exists to bound. Regressions: `TestExecutor_MaxRoundsRefusesFurtherAgentFixRounds` (live gate) and `TestExecutor_MaxRoundsRefusesAgentFixRoundOnARecoveredGate` (recovered gate).

Config surface is documented in `docs/src/content/docs/reference/global-config.md` (`max_rounds`) and `docs/src/content/docs/reference/repo-config.md`.

Touches: `internal/config/config.go`, `internal/pipeline/executor.go`, `internal/db/{schema,step}.go`, `internal/ipc/protocol.go`, `internal/daemon/daemon.go`, `internal/cli/{axi_render,axi_drive}.go`, `internal/skill/skill.go`.

### `CLAUDE_CONFIG_DIR` honored on skill install

`internal/skill/install.go` hardcoded `<home>/.claude/skills`, so a per-profile setup that points Claude Code at another config directory got the skill written into a profile its session never reads - `/no-mistakes` silently did not exist for that agent. The Claude base now resolves through `CLAUDE_CONFIG_DIR` (relative values resolved against the working directory, falling back to `<home>/.claude` when unset, blank, or unresolvable).

The vendor-neutral `~/.agents/skills` base is not a Claude Code directory and is unaffected.

Documented in `docs/src/content/docs/reference/environment.md` (`CLAUDE_CONFIG_DIR`).

Touches: `internal/skill/install.go`.

### Upstream's release channel is not this build's update channel

This is the one patch that does **not** default to upstream behavior, deliberately.

A fork build is cut from a tag upstream has already moved past, so its version always compares older than the latest release. Every run of any command therefore printed `A new version of no-mistakes is available: v1.66.0-14-gc30f8cf -> v1.73.0 / Run "no-mistakes update" to update` on stderr - and that advertised command is the one thing that must never run here: it downloads an upstream release and overwrites the fork binary with a build carrying none of the patches above, silently, with no warning that anything was lost.

`updater.forkBuild` is set by `defaultUpdater`, so every real binary built from this repository has the whole remote channel closed at four seams:

- the stderr notice, and the background refresh it spawns (`maybeNotifyAndCheck`)
- the TUI's "update available" banner (`cachedLatestVersion`, consumed by `internal/cli/attach.go`)
- the `--update-check` background subprocess (`MaybeHandleBackgroundCheck`, which still claims the flag so an older installed binary's spawn exits cleanly instead of reaching the cobra root as an unknown command)
- `no-mistakes update` itself, which now prints a refusal pointing here instead of downloading anything

Nothing reaches `api.github.com` for release metadata any more, and `NO_MISTAKES_NO_UPDATE_CHECK` is no longer needed on any host.

Updating is the build-and-install procedure below. The field is left `false` in tests so the upstream behavior they cover stays exercised; flipping it in `defaultUpdater` restores the released binary's behavior in full.

Touches: `internal/update/update.go`. Regressions: `internal/update/fork_test.go`.

### The version says `+fork`

`git describe` on a fork checkout produces a string indistinguishable from an ordinary upstream development build cut from the same tag, and the installed CLI and the daemon are the same binary - so "is the thing running right now our build?" has to be answerable from the version alone.

`buildinfo.CurrentVersion` appends `+fork`:

```
no-mistakes version v1.73.0-16-g94638f5+fork (94638f5) 2026-09-09T21:38:43Z
```

It is applied in `buildinfo` rather than in the `Makefile` so it survives every way this tree is built - `make build`, a bare `go build`, and the cross-compile recipe below. It is semver build metadata (everything from `+` on), which parsing and comparison discard, so it changes no version decision anywhere; it only rides the surfaces that report the version: `--version`, `doctor`, the run record's `no_mistakes_version`, the eval capture manifest, and telemetry. A `dev` build stays bare, because `telemetry.buildChannel` treats any other value as a release channel and that decides whether an unconfigured build emits remote telemetry at all.

After installing, `no-mistakes --version` not showing `+fork` means the released binary is back in place.

Touches: `internal/buildinfo/version.go`. Regressions: `internal/buildinfo/version_test.go`.

## Local configuration this fork assumes

No patch does anything until configured. The operator config that makes the round budget active lives in `~/.no-mistakes/config.yaml`:

```yaml
max_rounds:
  review: 3
  test: 3

# auto_fix.review defaults to 0, which parks the review gate on the first
# blocking finding and waits for an agent decision. Left at 0, max_rounds.review
# only ever bounds rounds an agent asks for, so a run driven by a bare
# `git push` sits parked instead of converging.
auto_fix:
  review: 3

# Duty tuning goes through purposes, not through upstream's review_agents -
# see "Model and effort per purpose" above for why, given `agent: auto`.
agent_config:
  claude:
    purposes:
      review-fix:
        effort: medium
      housekeeping:
        effort: low
```

The duty choices come from the local telemetry, not from taste: `review-fix` is 32.5% of all tokens and `housekeeping` 12.8%, and neither judges the change - one applies findings a review round already prescribed, the other edits documentation. `review` itself is left alone. Read your own split with `no-mistakes stats` before copying these.

Every value here is per host, and the Coder workspace runs a longer version of this block (a pinned `opus`/`high` base plus `test-fix`, `lint-fix`, and `document-fix`). `max_rounds` and `auto_fix` in particular were set independently on each machine.

## Building and installing over the released binary

The installed CLI and the daemon are the same binary; `~/.local/bin/no-mistakes` is a symlink to `~/.no-mistakes/bin/no-mistakes`. Replace it with a fork build:

```sh
cd <this repo>
make lint && go test -race ./...
make build                                  # -> bin/no-mistakes, version stamped from git describe

no-mistakes runs                            # confirm nothing is mid-flight first
no-mistakes daemon stop                     # refuses while runs are active; do not --force past that

cp ~/.no-mistakes/bin/no-mistakes ~/.no-mistakes/bin/no-mistakes.upstream-<version>.bak
cp bin/no-mistakes ~/.no-mistakes/bin/no-mistakes

no-mistakes daemon start
no-mistakes doctor
```

Then refresh the generated agent skill, which `init` installs at user level, from any registered repository:

```sh
no-mistakes init                            # idempotent; re-installs SKILL.md for the active profile
```

Schema migrations are additive and run automatically on the next daemon start. Rolling back to the backup binary is safe: every column this fork adds is nullable and read only when present.

`no-mistakes update` pulls an upstream release and will overwrite the fork build. After running it, rebuild and reinstall from here.

## Installing on a remote host

There is no Go toolchain on the Coder workspace, so the binary is cross-compiled here and shipped. `CGO_ENABLED=0` is what makes that work at all: the SQLite driver is `modernc.org/sqlite`, pure Go, so the result is a static ELF with no runtime dependency on the target.

```sh
VERSION=$(git describe --tags --always --dirty)
COMMIT=$(git rev-parse --short HEAD)
DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)

CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build \
  -ldflags "-X github.com/kunchenguid/no-mistakes/internal/buildinfo.Version=$VERSION \
            -X github.com/kunchenguid/no-mistakes/internal/buildinfo.Commit=$COMMIT \
            -X github.com/kunchenguid/no-mistakes/internal/buildinfo.Date=$DATE" \
  -o no-mistakes-linux-arm64 ./cmd/no-mistakes

scp no-mistakes-linux-arm64 <host>:/tmp/no-mistakes-fork
```

Match `GOARCH` to the target - the Coder workspace is `aarch64`, this machine is `amd64`. Compare `sha256sum` on both ends before installing, then follow the same stop, back up, replace, start sequence as above, and finish with `no-mistakes init` from a registered repository to refresh the skill.

Persistence on the Coder workspace: `/home` is its own volume, so `~/.no-mistakes` and the `~/.local/bin/no-mistakes` symlink survive a stop and start, and no template or dotfiles script reinstalls the released binary over ours. `no-mistakes update` used to be the remaining way to lose the fork build by accident; since the update-channel patch above it refuses instead.

The operator config is per host and does not travel with the binary - set `max_rounds` on each machine separately.

## Staying current with upstream

**Always merge, never squash or rebase.** A squash flattens the shared history and the next merge then reports every upstream file as a conflict.

```sh
git fetch upstream
git merge upstream/main          # on main
make lint && go test -race ./... # the executor round loop and the skill install are the conflict-prone spots
make build                       # then reinstall as above
```

`skills/no-mistakes/SKILL.md` is generated from the `body` constant in `internal/skill/skill.go`. After resolving a conflict that touches either, run `make skill`; `make lint` fails on drift.

Two CI checks always fail on an upstream-sync PR here, and neither is a regression:

- **Generated files must not be hand-edited** - the merge carries upstream's release-please `CHANGELOG.md` and `.release-please-manifest.json` updates, and the guard rejects any PR that touches them. It exists to police contributor feature PRs, which a sync is not.
- **PR must be raised via no-mistakes** - a bulk sync ships direct-PR, without the pipeline.

Judge a sync on `make lint`, `go build ./...`, `go test ./...`, and the CI build/test legs instead.

### Model and effort per purpose

`agent_config` was keyed by agent name only, so review, the review fixer, and the documentation pass all ran on one model at one effort. Those three duties are 63% of measured token spend here, which made a single global effort the only lever - and lowering it weakened the initial review, the pass that actually catches bugs.

`agent_config.<agent>.purposes` narrows `model` and `effort` per duty, as a delta on that agent's base profile. It nests under the agent because a model name is harness-specific: with an ordered fallback list, one purpose-level model string cannot serve both harnesses.

Two things made this more than a config change:

- A profile becomes native argv when the adapter is constructed, so one instance runs one model at one effort. `agent.WithPurposeProfiles` builds one instance per narrowed purpose, lazily and cached for the run, and falls back to base when a build fails.
- `RunOpts.Purpose` arrived **empty** at several call sites (ci, rebase, pr, test-evidence). Only the telemetry recorder defaulted it to the step name, far too late to route anything. That default moved into `perfRecordingAgent.Run`, before delegation, so the duty that dispatches is exactly the duty that is recorded.

Config surface is documented in `docs/src/content/docs/reference/global-config.md` (`agent_config` -> `purposes`); the vocabulary lives in `internal/types/purpose.go`.

Touches: `internal/types/purpose.go`, `internal/agent/purpose.go`, `internal/agentcfg/agentcfg.go`, `internal/config/config.go`, `internal/daemon/manager.go`, `internal/pipeline/instrument.go`.

Since v1.72.0 upstream ships `review_agents`, which pins the `reviewer` and `fixer` roles to an explicit harness plus model and effort. The two mechanisms compose rather than compete: `Config.ForReviewAgent` writes the role's profile as an `AgentTuning` base with no purpose deltas, so a `review`/`review-fix` delta can never re-narrow a role the operator pinned explicitly, and `purposes` keeps governing every duty outside the review loop.

Both hosts here nonetheless still tune the review loop through `purposes`, deliberately. `review_agents` requires an explicit harness - `agentcfg.Known` refuses `auto` - and `ForReviewAgent` sets `role.Agents` to that single harness. Under this fork's `agent: auto`, adopting it would collapse the fallback chain for exactly the two duties where a dead harness is most expensive: on the Coder workspace `codex` is installed and green, so today a `claude` outage leaves review running, and a pinned role would fail it instead. Reach for `review_agents` when you actually want a role on a *different* harness from the rest of the pipeline; that is the thing `purposes` cannot express.

`purposes` is also the only one of the two with reach beyond review: `review_agents` has exactly two roles, and `housekeeping` - 12.8% of measured spend here - has none to be pinned to, nor does any other non-review duty.

## Deliberately not done

- **Contributing any of this upstream.** See the top of this file.
