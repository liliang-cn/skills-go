package skill

import (
	"bytes"
	"strings"

	"gopkg.in/yaml.v3"
)

var separator = []byte("---")

// ParseFrontmatter parses YAML frontmatter and markdown content
func ParseFrontmatter(content []byte) (*Meta, string, error) {
	// Check if starts with ---
	if !bytes.HasPrefix(content, separator) {
		return nil, "", ErrInvalidFrontmatter
	}

	// Find second ---
	parts := bytes.SplitN(content, separator, 3)
	if len(parts) < 3 {
		return nil, "", ErrInvalidFrontmatter
	}

	frontmatter := bytes.TrimSpace(parts[1])
	markdown := bytes.TrimSpace(parts[2])

	var meta Meta
	if err := yaml.Unmarshal(frontmatter, &meta); err != nil {
		return nil, "", err
	}

	return &meta, string(markdown), nil
}

// MarshalFrontmatter converts metadata and content to SKILL.md format
func MarshalFrontmatter(meta *Meta, content string) ([]byte, error) {
	var buf bytes.Buffer

	fm, err := yaml.Marshal(meta)
	if err != nil {
		return nil, err
	}

	buf.WriteString("---\n")
	buf.Write(fm)
	buf.WriteString("---\n\n")
	buf.WriteString(content)

	return buf.Bytes(), nil
}

// ParseArguments extracts skill name and arguments from an invocation string
// e.g., "/skill-name arg1 arg2" -> ("skill-name", ["arg1", "arg2"])
func ParseArguments(invocation string) (string, []string) {
	invocation = strings.TrimSpace(invocation)
	if !strings.HasPrefix(invocation, "/") {
		return "", nil
	}

	parts := strings.Fields(invocation[1:])
	if len(parts) == 0 {
		return "", nil
	}

	return parts[0], parts[1:]
}
