// Package search provides search functionality for the Obsidian vault.
package search

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/Jnchk/obsitrace-mcp/internal/pathfilter"
	"github.com/Jnchk/obsitrace-mcp/internal/types"
	"github.com/Jnchk/obsitrace-mcp/internal/uri"
)

// Service provides search functionality for the Obsidian vault.
type Service struct {
	vaultPath  string
	pathFilter *pathfilter.PathFilter
}

// New creates a new SearchService.
func New(vaultPath string, pf *pathfilter.PathFilter) *Service {
	absPath, _ := filepath.Abs(vaultPath)
	return &Service{
		vaultPath:  absPath,
		pathFilter: pf,
	}
}

// SearchAdvanced performs advanced search with regex support and context lines.
// Returns results sorted stably by path, with totalFiles count for pagination.
func (s *Service) SearchAdvanced(params types.SearchParamsAdvanced) ([]types.SearchResultAdvanced, int, error) {
	query := params.Query
	if query == "" || strings.TrimSpace(query) == "" {
		return nil, 0, &SearchError{Message: "Search query cannot be empty"}
	}

	contextLines := params.ContextLines
	if contextLines <= 0 {
		contextLines = 2
	}

	limit := params.Limit
	if limit <= 0 {
		limit = 15
	}

	offset := max(params.Offset, 0)

	// Build the search pattern
	var searchPattern *regexp.Regexp
	var err error
	if params.UseRegex {
		if params.CaseSensitive {
			searchPattern, err = regexp.Compile(query)
		} else {
			searchPattern, err = regexp.Compile("(?i)" + query)
		}
		if err != nil {
			return nil, 0, &SearchError{Message: "Invalid regex pattern: " + err.Error()}
		}
	} else {
		// Escape regex special chars for literal search
		escaped := regexp.QuoteMeta(query)
		if params.CaseSensitive {
			searchPattern, err = regexp.Compile(escaped)
		} else {
			searchPattern, err = regexp.Compile("(?i)" + escaped)
		}
		if err != nil {
			return nil, 0, &SearchError{Message: "Search error: " + err.Error()}
		}
	}

	// Tag pattern for detecting tag matches
	tagPattern := regexp.MustCompile(`#[a-zA-Z0-9_/-]+`)

	// Find all markdown files
	markdownFiles, err := s.findMarkdownFiles(s.vaultPath)
	if err != nil {
		return nil, 0, err
	}

	// Sort files for stable ordering
	sort.Strings(markdownFiles)

	// Process files in parallel
	numWorkers := max(min(runtime.NumCPU(), len(markdownFiles)), 1)

	type indexedResult struct {
		idx    int
		result *types.SearchResultAdvanced
	}

	resultsCh := make(chan indexedResult, len(markdownFiles))
	fileCh := make(chan struct {
		idx  int
		path string
	}, len(markdownFiles))

	var wg sync.WaitGroup
	for range numWorkers {
		wg.Go(func() {
			for file := range fileCh {
				relativePath := file.path[len(s.vaultPath)+1:]
				relativePath = strings.ReplaceAll(relativePath, "\\", "/")

				if !s.pathFilter.IsAllowed(relativePath) {
					continue
				}

				content, err := os.ReadFile(file.path)
				if err != nil {
					continue
				}

				contentStr := string(content)
				lines := strings.Split(contentStr, "\n")

				var matches []types.SearchMatchAdvanced

				for lineNum, line := range lines {
					if searchPattern.MatchString(line) {
						startLine := max(lineNum-contextLines, 0)
						endLine := min(lineNum+contextLines+1, len(lines))

						contextSlice := lines[startLine:endLine]
						contextText := strings.Join(contextSlice, "\n")

						isTag := false
						tagMatches := tagPattern.FindAllString(line, -1)
						if slices.ContainsFunc(tagMatches, searchPattern.MatchString) {
							isTag = true
						}

						matches = append(matches, types.SearchMatchAdvanced{
							Line:    lineNum + 1,
							Context: contextText,
							IsTag:   isTag,
						})
					}
				}

				if len(matches) > 0 {
					resultsCh <- indexedResult{
						idx: file.idx,
						result: &types.SearchResultAdvanced{
							Path:    relativePath,
							Matches: matches,
						},
					}
				}
			}
		})
	}

	for i, path := range markdownFiles {
		fileCh <- struct {
			idx  int
			path string
		}{i, path}
	}
	close(fileCh)

	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	// Collect results and sort by original index for stable ordering
	var indexedResults []indexedResult
	for r := range resultsCh {
		indexedResults = append(indexedResults, r)
	}
	sort.Slice(indexedResults, func(i, j int) bool {
		return indexedResults[i].idx < indexedResults[j].idx
	})

	allResults := make([]types.SearchResultAdvanced, 0, len(indexedResults))
	for _, ir := range indexedResults {
		allResults = append(allResults, *ir.result)
	}

	totalFiles := len(allResults)

	// Apply offset and limit
	if offset >= len(allResults) {
		return []types.SearchResultAdvanced{}, totalFiles, nil
	}

	endIdx := min(offset+limit, len(allResults))

	return allResults[offset:endIdx], totalFiles, nil
}

// Search searches for notes in the vault (legacy method).
func (s *Service) Search(params types.SearchParams) ([]types.SearchResult, error) {
	query := params.Query
	limit := params.Limit
	searchContent := params.SearchContent
	searchFrontmatter := params.SearchFrontmatter
	caseSensitive := params.CaseSensitive

	if limit == 0 {
		limit = 5
	}
	if limit > 20 {
		limit = 20
	}
	if !searchContent && !searchFrontmatter {
		searchContent = true
	}

	if query == "" || strings.TrimSpace(query) == "" {
		return nil, &SearchError{Message: "Search query cannot be empty"}
	}

	var results []types.SearchResult

	markdownFiles, err := s.findMarkdownFiles(s.vaultPath)
	if err != nil {
		return nil, err
	}

	for _, fullPath := range markdownFiles {
		if len(results) >= limit {
			break
		}

		relativePath := fullPath[len(s.vaultPath)+1:]
		relativePath = strings.ReplaceAll(relativePath, "\\", "/")

		if !s.pathFilter.IsAllowed(relativePath) {
			continue
		}

		content, err := os.ReadFile(fullPath)
		if err != nil {
			continue
		}

		contentStr := string(content)
		var searchableText string

		if searchContent && searchFrontmatter {
			searchableText = contentStr
		} else if searchContent {
			if strings.HasPrefix(contentStr, "---\n") {
				if endIdx := strings.Index(contentStr[4:], "\n---\n"); endIdx != -1 {
					searchableText = contentStr[endIdx+4+5:]
				} else {
					searchableText = contentStr
				}
			} else {
				searchableText = contentStr
			}
		} else if searchFrontmatter {
			if strings.HasPrefix(contentStr, "---\n") {
				if endIdx := strings.Index(contentStr[4:], "\n---\n"); endIdx != -1 {
					searchableText = contentStr[4 : endIdx+4]
				}
			}
		}

		searchIn := searchableText
		searchQuery := query
		if !caseSensitive {
			searchIn = strings.ToLower(searchableText)
			searchQuery = strings.ToLower(query)
		}

		idx := strings.Index(searchIn, searchQuery)
		if idx != -1 {
			excerptStart := max(idx-21, 0)
			excerptEnd := min(idx+len(searchQuery)+21, len(searchableText))

			excerpt := strings.TrimSpace(searchableText[excerptStart:excerptEnd])

			if excerptStart > 0 {
				excerpt = "..." + excerpt
			}
			if excerptEnd < len(searchableText) {
				excerpt = excerpt + "..."
			}

			matchCount := 0
			searchIdx := 0
			for {
				foundIdx := strings.Index(searchIn[searchIdx:], searchQuery)
				if foundIdx == -1 {
					break
				}
				matchCount++
				searchIdx += foundIdx + len(searchQuery)
			}

			lines := strings.Split(searchableText[:idx], "\n")
			lineNumber := len(lines)

			title := filepath.Base(relativePath)
			title = strings.TrimSuffix(title, ".md")

			results = append(results, types.SearchResult{
				Path:       relativePath,
				Title:      title,
				Excerpt:    excerpt,
				MatchCount: matchCount,
				LineNumber: lineNumber,
				URI:        uri.GenerateObsidianURI(s.vaultPath, relativePath),
			})
		}
	}

	return results, nil
}

// findMarkdownFiles recursively finds all markdown files in a directory.
func (s *Service) findMarkdownFiles(dirPath string) ([]string, error) {
	var markdownFiles []string

	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return markdownFiles, nil
	}

	for _, entry := range entries {
		fullPath := filepath.Join(dirPath, entry.Name())

		if entry.IsDir() {
			subFiles, _ := s.findMarkdownFiles(fullPath)
			markdownFiles = append(markdownFiles, subFiles...)
		} else if entry.Type().IsRegular() && strings.HasSuffix(entry.Name(), ".md") {
			markdownFiles = append(markdownFiles, fullPath)
		}
	}

	return markdownFiles, nil
}

// SearchError represents a search error.
type SearchError struct {
	Message string
}

func (e *SearchError) Error() string {
	return e.Message
}
