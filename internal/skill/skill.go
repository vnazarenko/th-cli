// Package skill provides a lightweight validator for the th-cli Claude
// skill bundle (skill/SKILL.md + references). It exists so `go test ./...` can
// guard the skill's frontmatter — the metadata an agent runtime relies on to
// discover and trigger the skill — without pulling in a separate doc-lint
// toolchain. The parsing is intentionally minimal: just enough to read the
// leading `---`-delimited YAML block and confirm the required keys are present.
package skill

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// fence is the `---` delimiter that brackets a SKILL.md YAML frontmatter block.
const fence = "---"

// Frontmatter is the subset of SKILL.md YAML frontmatter the runtime needs to
// discover and trigger a skill. Both keys are required: name identifies the
// skill, description is the trigger surface the model matches against.
type Frontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// ParseFrontmatter extracts and parses the leading `---`-delimited YAML
// frontmatter block from SKILL.md content. It errors when the block is absent
// (the file must open with a `---` fence), unterminated, or not valid YAML.
func ParseFrontmatter(content string) (Frontmatter, error) {
	lines := strings.Split(content, "\n")

	// The opening fence must be the first non-empty line.
	open := 0
	for open < len(lines) && strings.TrimSpace(lines[open]) == "" {
		open++
	}
	if open >= len(lines) || strings.TrimSpace(lines[open]) != fence {
		return Frontmatter{}, fmt.Errorf("SKILL.md must begin with a `---` YAML frontmatter block")
	}

	// Find the closing fence.
	closeIdx := -1
	for i := open + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == fence {
			closeIdx = i
			break
		}
	}
	if closeIdx == -1 {
		return Frontmatter{}, fmt.Errorf("unterminated frontmatter block: missing closing `---`")
	}

	block := strings.Join(lines[open+1:closeIdx], "\n")
	var fm Frontmatter
	if err := yaml.Unmarshal([]byte(block), &fm); err != nil {
		return Frontmatter{}, fmt.Errorf("frontmatter is not valid YAML: %w", err)
	}
	return fm, nil
}

// Validate confirms the required frontmatter keys are present and non-empty.
func (f Frontmatter) Validate() error {
	if strings.TrimSpace(f.Name) == "" {
		return fmt.Errorf("frontmatter missing required key: name")
	}
	if strings.TrimSpace(f.Description) == "" {
		return fmt.Errorf("frontmatter missing required key: description")
	}
	return nil
}
