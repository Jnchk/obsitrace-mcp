// Package pathfilter provides security filtering for file paths.
package pathfilter

import (
	"regexp"
	"strings"

	"github.com/Jnchk/obsitrace-mcp/internal/types"
)

// PathFilter filters allowed paths and file types.
type PathFilter struct {
	ignoredPatterns   []string
	allowedExtensions []string
}

// New creates a new PathFilter with the given configuration.
func New(config *types.PathFilterConfig) *PathFilter {
	pf := &PathFilter{
		ignoredPatterns: []string{
			".obsidian/**",
			".git/**",
			"node_modules/**",
			".DS_Store",
			"Thumbs.db",
		},
		allowedExtensions: []string{
			".md",
			".markdown",
			".txt",
		},
	}

	if config != nil {
		pf.ignoredPatterns = append(pf.ignoredPatterns, config.IgnoredPatterns...)
		pf.allowedExtensions = append(pf.allowedExtensions, config.AllowedExtensions...)
	}

	return pf
}

// simpleGlobMatch converts a glob pattern to regex and tests against the path.
func (pf *PathFilter) simpleGlobMatch(pattern, path string) bool {
	// Normalize pattern path separators (Windows compatibility)
	normalizedPattern := strings.ReplaceAll(pattern, "\\", "/")

	// Escape all regex special chars first
	regexPattern := regexp.QuoteMeta(normalizedPattern)

	// Convert glob patterns (unescape the escaped versions)
	regexPattern = strings.ReplaceAll(regexPattern, `\*\*`, ".*")  // ** matches any
	regexPattern = strings.ReplaceAll(regexPattern, `\*`, "[^/]*") // * matches non-slash
	regexPattern = strings.ReplaceAll(regexPattern, `\?`, "[^/]")  // ? matches single char

	// Ensure we match the full path
	regexPattern = "^" + regexPattern + "$"

	re, err := regexp.Compile(regexPattern)
	if err != nil {
		return false
	}

	return re.MatchString(path)
}

// IsAllowed checks if a path is allowed based on the filter rules.
func (pf *PathFilter) IsAllowed(path string) bool {
	// Normalize path separators
	normalizedPath := strings.ReplaceAll(path, "\\", "/")

	// Check if path matches any ignored pattern
	for _, pattern := range pf.ignoredPatterns {
		if pf.simpleGlobMatch(pattern, normalizedPath) {
			return false
		}
	}

	// For files, check extension if allowedExtensions is configured
	if len(pf.allowedExtensions) > 0 && pf.isFile(normalizedPath) {
		hasAllowedExtension := false
		lowerPath := strings.ToLower(normalizedPath)
		for _, ext := range pf.allowedExtensions {
			if strings.HasSuffix(lowerPath, strings.ToLower(ext)) {
				hasAllowedExtension = true
				break
			}
		}
		if !hasAllowedExtension {
			return false
		}
	}

	return true
}

// isFile determines if a path represents a file (has a valid extension).
func (pf *PathFilter) isFile(path string) bool {
	// Paths ending with '/' are always directories
	if strings.HasSuffix(path, "/") {
		return false
	}

	// Get the last component of the path
	lastSlashIndex := strings.LastIndex(path, "/")
	var lastComponent string
	if lastSlashIndex == -1 {
		lastComponent = path
	} else {
		lastComponent = path[lastSlashIndex+1:]
	}

	// Check if the last component has a file extension
	lastDotIndex := strings.LastIndex(lastComponent, ".")
	if lastDotIndex == -1 || lastDotIndex == 0 {
		// No dot, or dot at the start (like .gitignore)
		return false
	}

	extension := lastComponent[lastDotIndex+1:]
	// Extension should be 1-10 characters and contain only alphanumeric characters
	if len(extension) < 1 || len(extension) > 10 {
		return false
	}

	matched, _ := regexp.MatchString("^[a-zA-Z0-9]+$", extension)
	return matched
}

// FilterPaths filters a slice of paths to only include allowed ones.
func (pf *PathFilter) FilterPaths(paths []string) []string {
	var allowed []string
	for _, path := range paths {
		if pf.IsAllowed(path) {
			allowed = append(allowed, path)
		}
	}
	return allowed
}
