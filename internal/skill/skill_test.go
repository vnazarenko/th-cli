package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// skillDir is the actual skill bundle, relative to this package directory (Go
// tests run with the package dir as the working dir). It lives under skills/ in
// the Claude-plugin layout so the plugin loader auto-discovers it.
const skillDir = "../../skills/th-cli"

// TestActualSkillFrontmatter is the doc-lint smoke check: the shipped
// skill/SKILL.md must carry valid frontmatter naming the th-cli skill,
// since that metadata is what an agent runtime uses to discover and trigger it.
func TestActualSkillFrontmatter(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
	if err != nil {
		t.Fatalf("read SKILL.md: %v", err)
	}

	fm, err := ParseFrontmatter(string(raw))
	if err != nil {
		t.Fatalf("parse frontmatter: %v", err)
	}
	if err := fm.Validate(); err != nil {
		t.Fatalf("frontmatter invalid: %v", err)
	}
	if fm.Name != "th-cli" {
		t.Errorf("name = %q, want th-cli", fm.Name)
	}
	// The description is the trigger surface — sanity-check it actually points at
	// the CLI and the domain rather than being a placeholder.
	for _, want := range []string{"trendHERO", "th-cli"} {
		if !strings.Contains(fm.Description, want) {
			t.Errorf("description does not mention %q: %q", want, fm.Description)
		}
	}
}

// TestReferenceFilesPresent guards the bundle layout SKILL.md points at.
func TestReferenceFilesPresent(t *testing.T) {
	for _, f := range []string{
		"references/auth.md",
		"references/reports.md",
		"references/examples.md",
	} {
		if _, err := os.Stat(filepath.Join(skillDir, f)); err != nil {
			t.Errorf("missing reference file %s: %v", f, err)
		}
	}
}

// TestLauncherBundle guards the self-provisioning launcher the skill drives: the
// pinned VERSION file and an executable bin/th-cli must ship with the bundle, and
// VERSION must look like a release tag (vX.Y.Z) so it matches a published asset.
func TestLauncherBundle(t *testing.T) {
	verRaw, err := os.ReadFile(filepath.Join(skillDir, "VERSION"))
	if err != nil {
		t.Fatalf("read VERSION: %v", err)
	}
	ver := strings.TrimSpace(string(verRaw))
	if !strings.HasPrefix(ver, "v") || len(ver) < 2 {
		t.Errorf("VERSION = %q, want a release tag like v0.1.0", ver)
	}

	info, err := os.Stat(filepath.Join(skillDir, "bin", "th-cli"))
	if err != nil {
		t.Fatalf("missing launcher bin/th-cli: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("launcher bin/th-cli is not executable (mode %v)", info.Mode().Perm())
	}
}

func TestParseFrontmatter(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr bool
		name_   string
		desc    string
	}{
		{
			name:    "valid block",
			content: "---\nname: foo\ndescription: a bar skill\n---\n\n# Body\n",
			name_:   "foo",
			desc:    "a bar skill",
		},
		{
			name:    "leading blank lines before fence are tolerated",
			content: "\n\n---\nname: foo\ndescription: d\n---\n",
			name_:   "foo",
			desc:    "d",
		},
		{
			name:    "missing opening fence",
			content: "name: foo\ndescription: d\n",
			wantErr: true,
		},
		{
			name:    "unterminated block",
			content: "---\nname: foo\ndescription: d\n",
			wantErr: true,
		},
		{
			name:    "invalid yaml",
			content: "---\nname: [unclosed\ndescription: d\n---\n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, err := ParseFrontmatter(tt.content)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got fm=%+v", fm)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if fm.Name != tt.name_ {
				t.Errorf("name = %q, want %q", fm.Name, tt.name_)
			}
			if fm.Description != tt.desc {
				t.Errorf("description = %q, want %q", fm.Description, tt.desc)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		fm      Frontmatter
		wantErr bool
	}{
		{name: "both present", fm: Frontmatter{Name: "x", Description: "y"}},
		{name: "missing name", fm: Frontmatter{Description: "y"}, wantErr: true},
		{name: "missing description", fm: Frontmatter{Name: "x"}, wantErr: true},
		{name: "whitespace name", fm: Frontmatter{Name: "  ", Description: "y"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.fm.Validate()
			if tt.wantErr && err == nil {
				t.Errorf("expected an error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
