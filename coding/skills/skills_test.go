package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromDirParsesSkillFrontmatter(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "test-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	content := `---
name: test-skill
description: useful skill
disableModelInvocation: true
---
# Skill
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	result := LoadFromDir(root)
	if len(result.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(result.Skills))
	}
	if result.Skills[0].Name != "test-skill" {
		t.Fatalf("unexpected skill name: %s", result.Skills[0].Name)
	}
	if !result.Skills[0].DisableModelInvocation {
		t.Fatal("expected DisableModelInvocation=true")
	}
}

func TestLoadFromDirFallsBackToDirectoryNameWhenNameMissing(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "fallback-name")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	content := `---
description: useful skill
---
# Skill
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	result := LoadFromDir(root)
	if len(result.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(result.Skills))
	}
	if result.Skills[0].Name != "fallback-name" {
		t.Fatalf("expected fallback name from directory, got %q", result.Skills[0].Name)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Type != "warning" {
		t.Fatalf("expected warning diagnostic for missing name, got %#v", result.Diagnostics)
	}
}

func TestLoadFromDirAddsDiagnosticOnReadError(t *testing.T) {
	result := LoadFromDir(filepath.Join(t.TempDir(), "does-not-exist"))
	if len(result.Diagnostics) == 0 {
		t.Fatal("expected diagnostic for invalid directory")
	}
}
