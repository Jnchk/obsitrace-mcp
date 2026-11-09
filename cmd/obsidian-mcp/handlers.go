package main

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/Jnchk/obsitrace-mcp/internal/types"
)

func handleRead(ctx context.Context, req *mcp.CallToolRequest, input ReadInput) (*mcp.CallToolResult, ReadOutput, error) {
	path := strings.TrimSpace(input.Path)
	note, err := fileSystem.ReadNote(path)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ReadOutput{}, err
	}

	content := note.Content
	lines := strings.Split(content, "\n")
	totalLines := len(lines)

	offset := max(input.Offset, 0)
	if offset >= totalLines {
		return nil, ReadOutput{
			Frontmatter: note.Frontmatter,
			Content:     "",
			TotalLines:  totalLines,
			Truncated:   true,
		}, nil
	}

	limit := input.Limit
	if limit <= 0 {
		limit = totalLines
	}

	endIdx := offset + limit
	truncated := false
	if endIdx >= totalLines {
		endIdx = totalLines
	} else {
		truncated = true
	}

	resultLines := lines[offset:endIdx]
	resultContent := strings.Join(resultLines, "\n")

	return nil, ReadOutput{
		Frontmatter: note.Frontmatter,
		Content:     resultContent,
		TotalLines:  totalLines,
		Truncated:   truncated,
	}, nil
}

func handleWrite(ctx context.Context, req *mcp.CallToolRequest, input WriteInput) (*mcp.CallToolResult, WriteOutput, error) {
	path := strings.TrimSpace(input.Path)
	err := fileSystem.WriteNote(types.NoteWriteParams{
		Path:        path,
		Content:     input.Content,
		Frontmatter: input.Frontmatter,
		Mode:        "overwrite",
	})
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, WriteOutput{Success: false, Path: path}, err
	}

	return nil, WriteOutput{Success: true, Path: path}, nil
}

func handleDelete(ctx context.Context, req *mcp.CallToolRequest, input DeleteInput) (*mcp.CallToolResult, DeleteOutput, error) {
	path := strings.TrimSpace(input.Path)

	if input.Confirm != "yes" {
		return &mcp.CallToolResult{IsError: true}, DeleteOutput{Success: false, Path: path},
			fmt.Errorf("deletion not confirmed: set confirm='yes' to proceed")
	}

	result := fileSystem.DeleteNote(types.DeleteNoteParams{
		Path:        path,
		ConfirmPath: path,
	})

	if !result.Success {
		return &mcp.CallToolResult{IsError: true}, DeleteOutput{Success: false, Path: path},
			fmt.Errorf("%s", result.Message)
	}

	return nil, DeleteOutput{Success: true, Path: path}, nil
}

func handleRename(ctx context.Context, req *mcp.CallToolRequest, input RenameInput) (*mcp.CallToolResult, RenameOutput, error) {
	oldPath := strings.TrimSpace(input.Path)
	newPath := strings.TrimSpace(input.NewPath)

	result := fileSystem.MoveNote(types.MoveNoteParams{
		OldPath:   oldPath,
		NewPath:   newPath,
		Overwrite: input.Overwrite,
	})

	if !result.Success {
		return &mcp.CallToolResult{IsError: true},
			RenameOutput{Success: false, OldPath: oldPath, NewPath: newPath},
			fmt.Errorf("%s", result.Message)
	}

	return nil, RenameOutput{Success: true, OldPath: oldPath, NewPath: newPath}, nil
}

func handleEdit(ctx context.Context, req *mcp.CallToolRequest, input EditInput) (*mcp.CallToolResult, EditOutput, error) {
	path := strings.TrimSpace(input.Path)

	note, err := fileSystem.ReadNote(path)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, EditOutput{Success: false, Path: path}, err
	}

	replacements := 0
	newContent := note.Content

	// Handle text replacement if oldText is provided
	if input.OldText != "" {
		occurrences := strings.Count(note.OriginalContent, input.OldText)
		if occurrences == 0 {
			return &mcp.CallToolResult{IsError: true}, EditOutput{Success: false, Path: path},
				fmt.Errorf("oldText not found in note")
		}

		if !input.ReplaceAll && occurrences > 1 {
			return &mcp.CallToolResult{IsError: true}, EditOutput{Success: false, Path: path},
				fmt.Errorf("found %d occurrences of oldText; use replaceAll=true or provide more specific text", occurrences)
		}

		// Replace in full content (including frontmatter area)
		var updatedFull string
		if input.ReplaceAll {
			updatedFull = strings.ReplaceAll(note.OriginalContent, input.OldText, input.NewText)
			replacements = occurrences
		} else {
			updatedFull = strings.Replace(note.OriginalContent, input.OldText, input.NewText, 1)
			replacements = 1
		}

		// Write the raw updated content
		fullPath, err := fileSystem.ResolvePath(path)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, EditOutput{Success: false, Path: path}, err
		}
		if err := os.WriteFile(fullPath, []byte(updatedFull), 0o644); err != nil {
			return &mcp.CallToolResult{IsError: true}, EditOutput{Success: false, Path: path}, err
		}

		// Re-read the note if we also need to update frontmatter
		if input.Frontmatter != nil {
			note, err = fileSystem.ReadNote(path)
			if err != nil {
				return &mcp.CallToolResult{IsError: true}, EditOutput{Success: false, Path: path}, err
			}
			newContent = note.Content
		}
	}

	// Handle frontmatter update if provided
	if input.Frontmatter != nil {
		updatedFm := make(map[string]any)
		if note.Frontmatter != nil {
			maps.Copy(updatedFm, note.Frontmatter)
		}
		maps.Copy(updatedFm, input.Frontmatter)

		err := fileSystem.WriteNote(types.NoteWriteParams{
			Path:        path,
			Content:     newContent,
			Frontmatter: updatedFm,
			Mode:        "overwrite",
		})
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, EditOutput{Success: false, Path: path}, err
		}
	}

	return nil, EditOutput{Success: true, Path: path, Replacements: replacements}, nil
}

func handleSearch(ctx context.Context, req *mcp.CallToolRequest, input SearchInput) (*mcp.CallToolResult, SearchOutput, error) {
	query := strings.TrimSpace(input.Query)
	if query == "" {
		return &mcp.CallToolResult{IsError: true}, SearchOutput{}, fmt.Errorf("query cannot be empty")
	}

	contextLines := input.ContextLines
	if contextLines <= 0 {
		contextLines = 2
	}

	limit := input.Limit
	if limit <= 0 {
		limit = 15
	}

	offset := max(input.Offset, 0)

	results, totalFiles, err := searchService.SearchAdvanced(types.SearchParamsAdvanced{
		Query:         query,
		UseRegex:      input.UseRegex,
		CaseSensitive: input.CaseSensitive,
		ContextLines:  contextLines,
		Limit:         limit,
		Offset:        offset,
	})
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SearchOutput{}, err
	}

	// Convert to output format and sort: tag matches first, then content
	items := []SearchResultItem{}
	for _, r := range results {
		var matches []SearchMatch
		for _, m := range r.Matches {
			matches = append(matches, SearchMatch{
				Line:    m.Line,
				Context: m.Context,
				IsTag:   m.IsTag,
			})
		}
		items = append(items, SearchResultItem{
			Path:    r.Path,
			Matches: matches,
		})
	}

	// Sort: files with tag matches first
	sort.SliceStable(items, func(i, j int) bool {
		hasTagI := false
		for _, m := range items[i].Matches {
			if m.IsTag {
				hasTagI = true
				break
			}
		}
		hasTagJ := false
		for _, m := range items[j].Matches {
			if m.IsTag {
				hasTagJ = true
				break
			}
		}
		if hasTagI != hasTagJ {
			return hasTagI
		}
		return items[i].Path < items[j].Path
	})

	hasMore := totalFiles > offset+len(items)

	return nil, SearchOutput{
		Results:    items,
		TotalFiles: totalFiles,
		HasMore:    hasMore,
	}, nil
}

// Obsidian link pattern: [[note]] or [[note|alias]] or [[note#heading]]
var linkPattern = regexp.MustCompile(`\[\[([^\]|#]+)(?:#[^\]|]*)?(?:\|[^\]]+)?\]\]`)

// Inline tag pattern: #tag (not inside code blocks)
var inlineTagPattern = regexp.MustCompile(`(?:^|\s)#([a-zA-Z0-9_/-]+)`)

func handleRelated(ctx context.Context, req *mcp.CallToolRequest, input RelatedInput) (*mcp.CallToolResult, RelatedOutput, error) {
	path := strings.TrimSpace(input.Path)

	// Default to both if neither specified
	searchTags := input.Tags
	searchLinks := input.Links
	if !searchTags && !searchLinks {
		searchTags = true
		searchLinks = true
	}

	// Read the source note
	note, err := fileSystem.ReadNote(path)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, RelatedOutput{}, err
	}

	// Get the note name without extension for link matching
	noteName := strings.TrimSuffix(filepath.Base(path), ".md")

	// Extract tags from source note
	var sourceTags []string
	if searchTags {
		sourceTags = extractTags(note.Frontmatter, note.Content)
	}

	// Extract outgoing links from source note
	var outgoingLinks []string
	if searchLinks {
		outgoingLinks = extractLinks(note.Content)
	}

	// Collect all markdown files first
	vaultPath := fileSystem.GetVaultPath()
	var allFiles []string
	err = filepath.Walk(vaultPath, func(fullPath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if strings.HasPrefix(info.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".md") {
			return nil
		}
		relPath, _ := filepath.Rel(vaultPath, fullPath)
		relPath = strings.ReplaceAll(relPath, "\\", "/")
		if relPath != path {
			allFiles = append(allFiles, relPath)
		}
		return nil
	})
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, RelatedOutput{}, err
	}

	// Sort for stable ordering
	sort.Strings(allFiles)

	// Process files in parallel
	numWorkers := max(min(runtime.NumCPU(), len(allFiles)), 1)

	type fileResult struct {
		idx      int
		relPath  string
		relation string
		tags     []string
	}

	resultsCh := make(chan fileResult, len(allFiles)*3)
	fileCh := make(chan struct {
		idx     int
		relPath string
	}, len(allFiles))

	var wg sync.WaitGroup
	for range numWorkers {
		wg.Go(func() {
			for file := range fileCh {
				relPath := file.relPath
				otherNote, err := fileSystem.ReadNote(relPath)
				if err != nil {
					continue
				}

				otherNoteName := strings.TrimSuffix(filepath.Base(relPath), ".md")

				// Check for tag matches
				if searchTags && len(sourceTags) > 0 {
					otherTags := extractTags(otherNote.Frontmatter, otherNote.Content)
					sharedTags := findSharedTags(sourceTags, otherTags)
					if len(sharedTags) > 0 {
						resultsCh <- fileResult{
							idx:      file.idx,
							relPath:  relPath,
							relation: "shared-tags",
							tags:     sharedTags,
						}
					}
				}

				// Check for link relationships
				if searchLinks {
					otherLinks := extractLinks(otherNote.Content)
					for _, link := range otherLinks {
						if strings.EqualFold(link, noteName) {
							resultsCh <- fileResult{
								idx:      file.idx,
								relPath:  relPath,
								relation: "backlink",
							}
							break
						}
					}

					for _, link := range outgoingLinks {
						if strings.EqualFold(link, otherNoteName) {
							resultsCh <- fileResult{
								idx:      file.idx,
								relPath:  relPath,
								relation: "outgoing",
							}
							break
						}
					}
				}
			}
		})
	}

	for i, relPath := range allFiles {
		fileCh <- struct {
			idx     int
			relPath string
		}{i, relPath}
	}
	close(fileCh)

	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	// Collect and merge results
	relatedMap := make(map[string]*RelatedNote)
	for r := range resultsCh {
		if existing, ok := relatedMap[r.relPath]; ok {
			existing.Relation = addRelation(existing.Relation, r.relation)
			if len(r.tags) > 0 && len(existing.Tags) == 0 {
				existing.Tags = r.tags
			}
		} else {
			relatedMap[r.relPath] = &RelatedNote{
				Path:     r.relPath,
				Relation: r.relation,
				Tags:     r.tags,
			}
		}
	}

	// Convert to slice and sort by path
	related := make([]RelatedNote, 0, len(relatedMap))
	for _, rn := range relatedMap {
		related = append(related, *rn)
	}
	sort.Slice(related, func(i, j int) bool {
		return related[i].Path < related[j].Path
	})

	return nil, RelatedOutput{
		Path:    path,
		Related: related,
	}, nil
}

func extractTags(frontmatter map[string]any, content string) []string {
	tagSet := make(map[string]bool)

	// Extract from frontmatter
	if fmTags, ok := frontmatter["tags"]; ok {
		switch t := fmTags.(type) {
		case []any:
			for _, tag := range t {
				if s, ok := tag.(string); ok {
					tagSet[strings.ToLower(s)] = true
				}
			}
		case []string:
			for _, tag := range t {
				tagSet[strings.ToLower(tag)] = true
			}
		case string:
			tagSet[strings.ToLower(t)] = true
		}
	}

	// Extract inline tags
	matches := inlineTagPattern.FindAllStringSubmatch(content, -1)
	for _, match := range matches {
		if len(match) > 1 {
			tagSet[strings.ToLower(match[1])] = true
		}
	}

	var tags []string
	for tag := range tagSet {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	return tags
}

func extractLinks(content string) []string {
	linkSet := make(map[string]bool)
	matches := linkPattern.FindAllStringSubmatch(content, -1)
	for _, match := range matches {
		if len(match) > 1 {
			// Normalize: lowercase for comparison
			linkSet[strings.ToLower(strings.TrimSpace(match[1]))] = true
		}
	}

	var links []string
	for link := range linkSet {
		links = append(links, link)
	}
	sort.Strings(links)
	return links
}

func findSharedTags(tags1, tags2 []string) []string {
	set1 := make(map[string]bool)
	for _, t := range tags1 {
		set1[t] = true
	}

	var shared []string
	for _, t := range tags2 {
		if set1[t] {
			shared = append(shared, t)
		}
	}
	sort.Strings(shared)
	return shared
}

func handleList(ctx context.Context, req *mcp.CallToolRequest, input ListInput) (*mcp.CallToolResult, ListOutput, error) {
	path := strings.TrimSpace(input.Path)
	if path == "" {
		path = "."
	}

	listing, err := fileSystem.ListDirectory(path)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, ListOutput{}, err
	}

	return nil, ListOutput{
		Files:       listing.Files,
		Directories: listing.Directories,
	}, nil
}

func addRelation(existing, newRel string) string {
	if existing == "" {
		return newRel
	}
	for _, rel := range strings.Split(existing, ",") {
		if rel == newRel {
			return existing
		}
	}
	return existing + "," + newRel
}

func handleTags(ctx context.Context, req *mcp.CallToolRequest, input TagsInput) (*mcp.CallToolResult, TagsOutput, error) {
	vaultPath := fileSystem.GetVaultPath()

	// Collect all markdown files
	var allFiles []string
	err := filepath.Walk(vaultPath, func(fullPath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if strings.HasPrefix(info.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".md") {
			return nil
		}
		relPath, _ := filepath.Rel(vaultPath, fullPath)
		relPath = strings.ReplaceAll(relPath, "\\", "/")
		allFiles = append(allFiles, relPath)
		return nil
	})
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, TagsOutput{}, err
	}

	// Process files in parallel, sending tags via channel
	numWorkers := max(min(runtime.NumCPU(), len(allFiles)), 1)

	tagsCh := make(chan []string, len(allFiles))
	fileCh := make(chan string, len(allFiles))

	var wg sync.WaitGroup
	for range numWorkers {
		wg.Go(func() {
			for relPath := range fileCh {
				note, err := fileSystem.ReadNote(relPath)
				if err != nil {
					continue
				}
				tags := extractTags(note.Frontmatter, note.Content)
				if len(tags) > 0 {
					tagsCh <- tags
				}
			}
		})
	}

	for _, relPath := range allFiles {
		fileCh <- relPath
	}
	close(fileCh)

	go func() {
		wg.Wait()
		close(tagsCh)
	}()

	// Collect tags with counts using map[string]int
	tagCounts := make(map[string]int)
	notesWithTags := 0
	for tags := range tagsCh {
		notesWithTags++
		for _, tag := range tags {
			tagCounts[tag]++
		}
	}

	// Convert to sorted slice of TagInfo
	tagInfos := make([]TagInfo, 0, len(tagCounts))
	for tag, count := range tagCounts {
		tagInfos = append(tagInfos, TagInfo{Tag: tag, Count: count})
	}
	sort.Slice(tagInfos, func(i, j int) bool {
		return tagInfos[i].Tag < tagInfos[j].Tag
	})

	return nil, TagsOutput{
		Tags:          tagInfos,
		TotalTags:     len(tagInfos),
		TotalNotes:    len(allFiles),
		NotesWithTags: notesWithTags,
	}, nil
}
