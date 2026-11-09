// Package frontmatter handles YAML frontmatter parsing and stringification.
package frontmatter

import (
	"fmt"
	"maps"
	"reflect"
	"strings"

	"github.com/Jnchk/obsitrace-mcp/internal/types"
	"gopkg.in/yaml.v3"
)

// Handler handles frontmatter parsing and validation.
type Handler struct{}

// New creates a new FrontmatterHandler.
func New() *Handler {
	return &Handler{}
}

// Parse parses a note's content and extracts frontmatter.
func (h *Handler) Parse(content string) types.ParsedNote {
	result := types.ParsedNote{
		Frontmatter:     make(map[string]any),
		Content:         content,
		OriginalContent: content,
	}

	// Check if content starts with frontmatter delimiter
	if !strings.HasPrefix(content, "---\n") {
		return result
	}

	// Find the closing delimiter.
	bodyStart := len("---\n")
	closingIndex := strings.Index(content[bodyStart:], "\n---\n")
	var yamlContent string
	var contentStart int
	if closingIndex == -1 {
		// Try finding --- at the very end.
		if !strings.HasSuffix(content, "\n---") {
			return result
		}
		yamlEnd := len(content) - len("\n---")
		yamlContent = content[bodyStart:yamlEnd]
		contentStart = len(content)
	} else {
		yamlContent = content[bodyStart : bodyStart+closingIndex]
		contentStart = bodyStart + closingIndex + len("\n---\n")
	}

	// Parse YAML
	var frontmatter map[string]any
	if err := yaml.Unmarshal([]byte(yamlContent), &frontmatter); err != nil {
		// If parsing fails, treat as content without frontmatter
		return result
	}

	result.Frontmatter = frontmatter
	if frontmatter == nil {
		result.Frontmatter = make(map[string]any)
	}

	// Content starts after the closing delimiter.
	result.Content = content[contentStart:]

	return result
}

// Stringify converts frontmatter and content back to a note string.
func (h *Handler) Stringify(frontmatter map[string]any, content string) (string, error) {
	// If no frontmatter, return content as-is
	if len(frontmatter) == 0 {
		return content, nil
	}

	yamlBytes, err := yaml.Marshal(frontmatter)
	if err != nil {
		return "", fmt.Errorf("failed to stringify frontmatter: %w", err)
	}

	return "---\n" + string(yamlBytes) + "---\n" + content, nil
}

// Validate validates frontmatter data.
func (h *Handler) Validate(frontmatter map[string]any) types.FrontmatterValidationResult {
	result := types.FrontmatterValidationResult{
		IsValid:  true,
		Errors:   []string{},
		Warnings: []string{},
	}

	// Check for problematic values FIRST (before Marshal which can panic on functions)
	h.checkForProblematicValues(frontmatter, &result, "")

	// Only try to marshal if no problematic values found
	if result.IsValid {
		// Test if the frontmatter can be serialized to valid YAML
		if _, err := yaml.Marshal(frontmatter); err != nil {
			result.IsValid = false
			result.Errors = append(result.Errors, fmt.Sprintf("Invalid YAML structure: %v", err))
		}
	}

	return result
}

func (h *Handler) checkForProblematicValues(obj any, result *types.FrontmatterValidationResult, path string) {
	if obj == nil {
		return
	}

	v := reflect.ValueOf(obj)

	switch v.Kind() {
	case reflect.Func:
		result.Errors = append(result.Errors, fmt.Sprintf("Functions are not allowed in frontmatter at path: %s", path))
		result.IsValid = false
		return

	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			h.checkForProblematicValues(v.Index(i).Interface(), result, fmt.Sprintf("%s[%d]", path, i))
		}

	case reflect.Map:
		iter := v.MapRange()
		for iter.Next() {
			key := iter.Key()
			value := iter.Value()

			var currentPath string
			if path == "" {
				currentPath = fmt.Sprintf("%v", key.Interface())
			} else {
				currentPath = fmt.Sprintf("%s.%v", path, key.Interface())
			}

			// Check for non-string keys
			if key.Kind() != reflect.String {
				result.Errors = append(result.Errors, fmt.Sprintf("Non-string keys are not allowed: %v", key.Interface()))
				result.IsValid = false
			}

			h.checkForProblematicValues(value.Interface(), result, currentPath)
		}
	}
}

// ExtractFrontmatter extracts frontmatter from content without the content body.
func (h *Handler) ExtractFrontmatter(content string) map[string]any {
	parsed := h.Parse(content)
	return parsed.Frontmatter
}

// UpdateFrontmatter updates frontmatter in existing content.
func (h *Handler) UpdateFrontmatter(content string, updates map[string]any) (string, error) {
	parsed := h.Parse(content)

	// Merge updates into existing frontmatter
	updatedFrontmatter := make(map[string]any)
	maps.Copy(updatedFrontmatter, parsed.Frontmatter)
	maps.Copy(updatedFrontmatter, updates)

	validation := h.Validate(updatedFrontmatter)
	if !validation.IsValid {
		return "", fmt.Errorf("invalid frontmatter: %s", strings.Join(validation.Errors, ", "))
	}

	return h.Stringify(updatedFrontmatter, parsed.Content)
}
