package skill

import (
	"os"
	"path/filepath"
	"testing"
)

// TestClaudeConfigDirDefault pins the unset and blank cases to the historical
// layout, so honoring the env var never changes where a plain install lands.
func TestClaudeConfigDirDefault(t *testing.T) {
	root := t.TempDir()
	want := filepath.Join(root, ".claude")

	for _, value := range []string{"", "   "} {
		t.Setenv(ClaudeConfigDirEnv, value)
		if got := ClaudeConfigDir(root); got != want {
			t.Errorf("ClaudeConfigDir(root) with %q = %q, want %q", value, got, want)
		}
	}
}

// TestInstallHonorsClaudeConfigDir is the regression for a profile setup that
// points Claude Code at a non-default config directory (for example
// ~/.claude-work): the skill must land in the profile Claude Code actually
// reads, not in ~/.claude.
func TestInstallHonorsClaudeConfigDir(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(t.TempDir(), ".claude-work")
	t.Setenv(ClaudeConfigDirEnv, configDir)

	written, err := Install(root)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	installed := filepath.Join(configDir, "skills", Name, "SKILL.md")
	data, err := os.ReadFile(installed)
	if err != nil {
		t.Fatalf("skill not installed under CLAUDE_CONFIG_DIR: %v", err)
	}
	if string(data) != Markdown() {
		t.Errorf("%s content does not match Markdown()", installed)
	}

	if _, err := os.Stat(filepath.Join(root, ".claude")); !os.IsNotExist(err) {
		t.Errorf("Install wrote into root/.claude despite CLAUDE_CONFIG_DIR being set (stat err = %v)", err)
	}

	// The vendor-neutral base is not a Claude Code directory, so it stays
	// under root regardless of CLAUDE_CONFIG_DIR.
	agentsRel := filepath.Join(".agents", "skills", Name, "SKILL.md")
	if _, err := os.ReadFile(filepath.Join(root, agentsRel)); err != nil {
		t.Fatalf("skill not installed at the vendor-neutral base: %v", err)
	}

	want := []string{installed, agentsRel}
	if len(written) != len(want) {
		t.Fatalf("written = %v, want %v", written, want)
	}
	for i := range want {
		if written[i] != want[i] {
			t.Errorf("written[%d] = %q, want %q", i, written[i], want[i])
		}
	}
}

// TestInstallClaudeConfigDirRelative covers a relative CLAUDE_CONFIG_DIR,
// which Claude Code resolves against the working directory.
func TestInstallClaudeConfigDirRelative(t *testing.T) {
	root := t.TempDir()
	workdir := t.TempDir()
	t.Chdir(workdir)
	t.Setenv(ClaudeConfigDirEnv, "profile")

	if _, err := Install(root); err != nil {
		t.Fatalf("Install: %v", err)
	}

	installed := filepath.Join(workdir, "profile", "skills", Name, "SKILL.md")
	if _, err := os.ReadFile(installed); err != nil {
		t.Fatalf("relative CLAUDE_CONFIG_DIR not resolved against the working directory: %v", err)
	}
}

// TestInstallUserHonorsClaudeConfigDir proves the init entry point - not just
// the root-parameterized helper - follows the env var.
func TestInstallUserHonorsClaudeConfigDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	configDir := filepath.Join(t.TempDir(), ".claude-work")
	t.Setenv(ClaudeConfigDirEnv, configDir)

	if _, err := InstallUser(); err != nil {
		t.Fatalf("InstallUser: %v", err)
	}

	if _, err := os.ReadFile(filepath.Join(configDir, "skills", Name, "SKILL.md")); err != nil {
		t.Fatalf("InstallUser ignored CLAUDE_CONFIG_DIR: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude")); !os.IsNotExist(err) {
		t.Errorf("InstallUser wrote into ~/.claude despite CLAUDE_CONFIG_DIR being set (stat err = %v)", err)
	}
}

// TestInstallTargetsOrder pins the install order the reported paths depend on.
func TestInstallTargetsOrder(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(t.TempDir(), "cfg")
	t.Setenv(ClaudeConfigDirEnv, configDir)

	want := []string{
		filepath.Join(configDir, "skills"),
		filepath.Join(root, ".agents", "skills"),
	}
	got := InstallTargets(root)
	if len(got) != len(want) {
		t.Fatalf("InstallTargets = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("InstallTargets[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
