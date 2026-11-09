package search

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Jnchk/obsitrace-mcp/internal/pathfilter"
	"github.com/Jnchk/obsitrace-mcp/internal/types"
)

func setupTestVault(t *testing.T) (string, *Service) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "mcp-obsidian-search-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	pf := pathfilter.New(nil)
	svc := New(tmpDir, pf)
	return tmpDir, svc
}

func cleanupTestVault(t *testing.T, path string) {
	t.Helper()
	os.RemoveAll(path)
}

func TestService_Search(t *testing.T) {
	t.Run("finds matching content", func(t *testing.T) {
		tmpDir, svc := setupTestVault(t)
		defer cleanupTestVault(t, tmpDir)

		// Create test files
		os.WriteFile(filepath.Join(tmpDir, "note1.md"), []byte("# Note 1\n\nThis contains searchterm."), 0o644)
		os.WriteFile(filepath.Join(tmpDir, "note2.md"), []byte("# Note 2\n\nNo match here."), 0o644)
		os.WriteFile(filepath.Join(tmpDir, "note3.md"), []byte("# Note 3\n\nAlso has searchterm."), 0o644)

		results, err := svc.Search(types.SearchParams{
			Query:         "searchterm",
			Limit:         10,
			SearchContent: true,
		})
		if err != nil {
			t.Fatalf("Search() error = %v", err)
		}

		if len(results) != 2 {
			t.Errorf("Search() returned %d results, want 2", len(results))
		}
	})

	t.Run("respects limit", func(t *testing.T) {
		tmpDir, svc := setupTestVault(t)
		defer cleanupTestVault(t, tmpDir)

		// Create multiple matching files
		for i := 1; i <= 10; i++ {
			os.WriteFile(
				filepath.Join(tmpDir, "note"+string(rune('0'+i))+".md"),
				[]byte("# Note\n\nThis contains keyword."),
				0o644,
			)
		}

		results, err := svc.Search(types.SearchParams{
			Query:         "keyword",
			Limit:         3,
			SearchContent: true,
		})
		if err != nil {
			t.Fatalf("Search() error = %v", err)
		}

		if len(results) != 3 {
			t.Errorf("Search() returned %d results, want 3", len(results))
		}
	})

	t.Run("max limit is 20", func(t *testing.T) {
		tmpDir, svc := setupTestVault(t)
		defer cleanupTestVault(t, tmpDir)

		// Create 25 matching files
		for i := 1; i <= 25; i++ {
			filename := filepath.Join(tmpDir, "note"+string(rune('A'+i-1))+".md")
			os.WriteFile(filename, []byte("# Note\n\nThis contains keyword."), 0o644)
		}

		results, err := svc.Search(types.SearchParams{
			Query:         "keyword",
			Limit:         100, // Request more than max
			SearchContent: true,
		})
		if err != nil {
			t.Fatalf("Search() error = %v", err)
		}

		if len(results) > 20 {
			t.Errorf("Search() returned %d results, want <= 20", len(results))
		}
	})

	t.Run("case insensitive by default", func(t *testing.T) {
		tmpDir, svc := setupTestVault(t)
		defer cleanupTestVault(t, tmpDir)

		os.WriteFile(filepath.Join(tmpDir, "note.md"), []byte("# Note\n\nThis contains KEYWORD."), 0o644)

		results, err := svc.Search(types.SearchParams{
			Query:         "keyword",
			SearchContent: true,
		})
		if err != nil {
			t.Fatalf("Search() error = %v", err)
		}

		if len(results) != 1 {
			t.Errorf("Search() returned %d results, want 1", len(results))
		}
	})

	t.Run("case sensitive when specified", func(t *testing.T) {
		tmpDir, svc := setupTestVault(t)
		defer cleanupTestVault(t, tmpDir)

		os.WriteFile(filepath.Join(tmpDir, "note.md"), []byte("# Note\n\nThis contains KEYWORD."), 0o644)

		results, err := svc.Search(types.SearchParams{
			Query:         "keyword",
			SearchContent: true,
			CaseSensitive: true,
		})
		if err != nil {
			t.Fatalf("Search() error = %v", err)
		}

		if len(results) != 0 {
			t.Errorf("Search() returned %d results, want 0", len(results))
		}
	})

	t.Run("search frontmatter only", func(t *testing.T) {
		tmpDir, svc := setupTestVault(t)
		defer cleanupTestVault(t, tmpDir)

		content := `---
title: Special Title
tags: [test]
---

# Note

Regular content here.`
		os.WriteFile(filepath.Join(tmpDir, "note.md"), []byte(content), 0o644)

		results, err := svc.Search(types.SearchParams{
			Query:             "Special Title",
			SearchContent:     false,
			SearchFrontmatter: true,
		})
		if err != nil {
			t.Fatalf("Search() error = %v", err)
		}

		if len(results) != 1 {
			t.Errorf("Search() returned %d results, want 1", len(results))
		}
	})

	t.Run("empty query returns error", func(t *testing.T) {
		tmpDir, svc := setupTestVault(t)
		defer cleanupTestVault(t, tmpDir)

		_, err := svc.Search(types.SearchParams{
			Query: "",
		})

		if err == nil {
			t.Error("Search() should return error for empty query")
		}
	})

	t.Run("returns correct metadata", func(t *testing.T) {
		tmpDir, svc := setupTestVault(t)
		defer cleanupTestVault(t, tmpDir)

		content := "# Test Note\n\nThis line has keyword in it.\n\nAnother line."
		os.WriteFile(filepath.Join(tmpDir, "test-note.md"), []byte(content), 0o644)

		results, err := svc.Search(types.SearchParams{
			Query:         "keyword",
			SearchContent: true,
		})
		if err != nil {
			t.Fatalf("Search() error = %v", err)
		}

		if len(results) != 1 {
			t.Fatalf("Search() returned %d results, want 1", len(results))
		}

		result := results[0]
		if result.Path != "test-note.md" {
			t.Errorf("Path = %q, want %q", result.Path, "test-note.md")
		}
		if result.Title != "test-note" {
			t.Errorf("Title = %q, want %q", result.Title, "test-note")
		}
		if result.MatchCount != 1 {
			t.Errorf("MatchCount = %d, want 1", result.MatchCount)
		}
		if result.URI == "" {
			t.Error("URI should not be empty")
		}
	})

	t.Run("handles special characters in filenames", func(t *testing.T) {
		tmpDir, svc := setupTestVault(t)
		defer cleanupTestVault(t, tmpDir)

		// Create folder and file with special chars
		os.MkdirAll(filepath.Join(tmpDir, "folder (archive)"), 0o755)
		os.WriteFile(
			filepath.Join(tmpDir, "folder (archive)", "note [old].md"),
			[]byte("# Old Note\n\nArchived keyword."),
			0o644,
		)

		results, err := svc.Search(types.SearchParams{
			Query:         "keyword",
			SearchContent: true,
		})
		if err != nil {
			t.Fatalf("Search() error = %v", err)
		}

		if len(results) != 1 {
			t.Errorf("Search() returned %d results, want 1", len(results))
		}

		if results[0].Path != "folder (archive)/note [old].md" {
			t.Errorf("Path = %q, want %q", results[0].Path, "folder (archive)/note [old].md")
		}
	})
}

func TestService_SearchAdvanced(t *testing.T) {
	t.Run("returns context lines", func(t *testing.T) {
		tmpDir, svc := setupTestVault(t)
		defer cleanupTestVault(t, tmpDir)

		content := "line1\nline2\nline3 keyword\nline4\nline5"
		os.WriteFile(filepath.Join(tmpDir, "note.md"), []byte(content), 0o644)

		results, total, err := svc.SearchAdvanced(types.SearchParamsAdvanced{
			Query:        "keyword",
			ContextLines: 2,
			Limit:        15,
		})
		if err != nil {
			t.Fatalf("SearchAdvanced() error = %v", err)
		}

		if total != 1 {
			t.Errorf("total = %d, want 1", total)
		}
		if len(results) != 1 {
			t.Fatalf("results = %d, want 1", len(results))
		}
		if len(results[0].Matches) != 1 {
			t.Fatalf("matches = %d, want 1", len(results[0].Matches))
		}

		context := results[0].Matches[0].Context
		if context != "line1\nline2\nline3 keyword\nline4\nline5" {
			t.Errorf("context = %q, want full content with 2 lines before/after", context)
		}
	})

	t.Run("regex search", func(t *testing.T) {
		tmpDir, svc := setupTestVault(t)
		defer cleanupTestVault(t, tmpDir)

		os.WriteFile(filepath.Join(tmpDir, "note.md"), []byte("foo123bar\nfoo456bar"), 0o644)

		results, _, err := svc.SearchAdvanced(types.SearchParamsAdvanced{
			Query:    "foo[0-9]+bar",
			UseRegex: true,
			Limit:    15,
		})
		if err != nil {
			t.Fatalf("SearchAdvanced() error = %v", err)
		}

		if len(results) != 1 {
			t.Fatalf("results = %d, want 1", len(results))
		}
		if len(results[0].Matches) != 2 {
			t.Errorf("matches = %d, want 2", len(results[0].Matches))
		}
	})

	t.Run("identifies tag matches", func(t *testing.T) {
		tmpDir, svc := setupTestVault(t)
		defer cleanupTestVault(t, tmpDir)

		os.WriteFile(filepath.Join(tmpDir, "note.md"), []byte("Content with #project tag"), 0o644)

		results, _, err := svc.SearchAdvanced(types.SearchParamsAdvanced{
			Query: "project",
			Limit: 15,
		})
		if err != nil {
			t.Fatalf("SearchAdvanced() error = %v", err)
		}

		if len(results) != 1 || len(results[0].Matches) != 1 {
			t.Fatalf("expected 1 result with 1 match")
		}
		if !results[0].Matches[0].IsTag {
			t.Error("match should be identified as tag")
		}
	})

	t.Run("pagination with offset", func(t *testing.T) {
		tmpDir, svc := setupTestVault(t)
		defer cleanupTestVault(t, tmpDir)

		for i := range 5 {
			os.WriteFile(
				filepath.Join(tmpDir, string(rune('a'+i))+".md"),
				[]byte("keyword here"),
				0o644,
			)
		}

		results, total, err := svc.SearchAdvanced(types.SearchParamsAdvanced{
			Query:  "keyword",
			Limit:  2,
			Offset: 2,
		})
		if err != nil {
			t.Fatalf("SearchAdvanced() error = %v", err)
		}

		if total != 5 {
			t.Errorf("total = %d, want 5", total)
		}
		if len(results) != 2 {
			t.Errorf("results = %d, want 2", len(results))
		}
	})

	t.Run("stable sort by path", func(t *testing.T) {
		tmpDir, svc := setupTestVault(t)
		defer cleanupTestVault(t, tmpDir)

		os.WriteFile(filepath.Join(tmpDir, "z.md"), []byte("keyword"), 0o644)
		os.WriteFile(filepath.Join(tmpDir, "a.md"), []byte("keyword"), 0o644)
		os.WriteFile(filepath.Join(tmpDir, "m.md"), []byte("keyword"), 0o644)

		results, _, err := svc.SearchAdvanced(types.SearchParamsAdvanced{
			Query: "keyword",
			Limit: 15,
		})
		if err != nil {
			t.Fatalf("SearchAdvanced() error = %v", err)
		}

		if len(results) != 3 {
			t.Fatalf("results = %d, want 3", len(results))
		}
		if results[0].Path != "a.md" || results[1].Path != "m.md" || results[2].Path != "z.md" {
			t.Errorf("results not sorted by path: %v, %v, %v", results[0].Path, results[1].Path, results[2].Path)
		}
	})

	t.Run("invalid regex returns error", func(t *testing.T) {
		tmpDir, svc := setupTestVault(t)
		defer cleanupTestVault(t, tmpDir)

		_, _, err := svc.SearchAdvanced(types.SearchParamsAdvanced{
			Query:    "[invalid",
			UseRegex: true,
		})
		if err == nil {
			t.Error("expected error for invalid regex")
		}
	})
}
