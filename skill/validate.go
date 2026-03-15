package skill

import (
	"fmt"
	"path/filepath"
	"regexp"
	"unicode/utf8"
)

var skillNamePattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// ValidateMeta validates SKILL.md frontmatter against the Agent Skills spec.
func ValidateMeta(meta *Meta, skillPath string) error {
	if meta == nil {
		return fmt.Errorf("%w: missing frontmatter", ErrInvalidSkill)
	}

	if meta.Name == "" {
		return fmt.Errorf("%w: missing required field %q", ErrInvalidSkill, "name")
	}
	if utf8.RuneCountInString(meta.Name) > 64 {
		return fmt.Errorf("%w: name exceeds 64 characters", ErrInvalidSkill)
	}
	if !skillNamePattern.MatchString(meta.Name) {
		return fmt.Errorf("%w: name must contain lowercase letters, numbers, and single hyphens only", ErrInvalidSkill)
	}

	if skillPath != "" && filepath.Base(skillPath) != meta.Name {
		return fmt.Errorf("%w: name %q must match directory %q", ErrInvalidSkill, meta.Name, filepath.Base(skillPath))
	}

	if meta.Description == "" {
		return fmt.Errorf("%w: missing required field %q", ErrInvalidSkill, "description")
	}
	if utf8.RuneCountInString(meta.Description) > 1024 {
		return fmt.Errorf("%w: description exceeds 1024 characters", ErrInvalidSkill)
	}

	if meta.Compatibility != "" && utf8.RuneCountInString(meta.Compatibility) > 500 {
		return fmt.Errorf("%w: compatibility exceeds 500 characters", ErrInvalidSkill)
	}

	for key, value := range meta.Metadata {
		if _, ok := value.(string); !ok {
			return fmt.Errorf("%w: metadata value for %q must be a string", ErrInvalidSkill, key)
		}
	}

	return nil
}
