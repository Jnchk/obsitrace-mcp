package filesystem

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/taigrr/obsidian-mcp/internal/types"
)

func setupTestVault(t *testing.T) (string, *Service) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "mcp-obsidian-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	svc := New(tmpDir, nil, nil)
	return tmpDir, svc
}

func cleanupTestVault(t *testing.T, path string) {
	t.Helper()
	os.RemoveAll(path)
}

func TestService_ResolvePath(t *testing.T) {
	t.Run("allows dot-prefixed directory names inside vault", func(t *testing.T) {
		tmpDir, svc := setupTestVault(t)
		defer cleanupTestVault(t, tmpDir)

		got, err := svc.ResolvePath("..notes/project.md")
		if err != nil {
			t.Fatalf("ResolvePath() error = %v", err)
		}

		want := filepath.Join(tmpDir, "..notes", "project.md")
		if got != want {
			t.Fatalf("ResolvePath() = %q, want %q", got, want)
		}
	})

	t.Run("rejects parent directory traversal", func(t *testing.T) {
		tmpDir, svc := setupTestVault(t)
		defer cleanupTestVault(t, tmpDir)

		_, err := svc.ResolvePath("../outside.md")
		if err == nil {
			t.Fatal("ResolvePath() error = nil, want traversal error")
		}
		if !strings.Contains(err.Error(), "path traversal not allowed") {
			t.Fatalf("ResolvePath() error = %q, want traversal error", err)
		}
	})
}

func TestService_PatchNote(t *testing.T) {
	t.Run("single occurrence", func(t *testing.T) {
		tmpDir, svc := setupTestVault(t)
		defer cleanupTestVault(t, tmpDir)

		content := "# Test Note\n\nThis is the old content.\n\nMore text here."
		testPath := "test-note.md"
		os.WriteFile(filepath.Join(tmpDir, testPath), []byte(content), 0o644)

		result := svc.PatchNote(types.PatchNoteParams{
			Path:       testPath,
			OldString:  "old content",
			NewString:  "new content",
			ReplaceAll: false,
		})

		if !result.Success {
			t.Errorf("Success = false, want true. Message: %s", result.Message)
		}
		if result.MatchCount != 1 {
			t.Errorf("MatchCount = %d, want 1", result.MatchCount)
		}

		note, _ := svc.ReadNote(testPath)
		if !strings.Contains(note.Content, "new content") {
			t.Error("Content should contain 'new content'")
		}
		if strings.Contains(note.Content, "old content") {
			t.Error("Content should not contain 'old content'")
		}
	})

	t.Run("multiple occurrences requires replaceAll", func(t *testing.T) {
		tmpDir, svc := setupTestVault(t)
		defer cleanupTestVault(t, tmpDir)

		content := "# Test\n\nrepeat word repeat word repeat"
		testPath := "test-note.md"
		os.WriteFile(filepath.Join(tmpDir, testPath), []byte(content), 0o644)

		result := svc.PatchNote(types.PatchNoteParams{
			Path:       testPath,
			OldString:  "repeat",
			NewString:  "unique",
			ReplaceAll: false,
		})

		if result.Success {
			t.Error("Success = true, want false")
		}
		if result.MatchCount != 3 {
			t.Errorf("MatchCount = %d, want 3", result.MatchCount)
		}
		if !strings.Contains(result.Message, "Found 3 occurrences") {
			t.Errorf("Message should mention 3 occurrences: %s", result.Message)
		}
	})

	t.Run("replaceAll replaces all occurrences", func(t *testing.T) {
		tmpDir, svc := setupTestVault(t)
		defer cleanupTestVault(t, tmpDir)

		content := "# Test\n\nrepeat word repeat word repeat"
		testPath := "test-note.md"
		os.WriteFile(filepath.Join(tmpDir, testPath), []byte(content), 0o644)

		result := svc.PatchNote(types.PatchNoteParams{
			Path:       testPath,
			OldString:  "repeat",
			NewString:  "unique",
			ReplaceAll: true,
		})

		if !result.Success {
			t.Errorf("Success = false, want true. Message: %s", result.Message)
		}
		if result.MatchCount != 3 {
			t.Errorf("MatchCount = %d, want 3", result.MatchCount)
		}

		note, _ := svc.ReadNote(testPath)
		if strings.Contains(note.Content, "repeat") {
			t.Error("Content should not contain 'repeat'")
		}
		if strings.Count(note.Content, "unique") != 3 {
			t.Errorf("Content should contain 3 'unique', got %d", strings.Count(note.Content, "unique"))
		}
	})

	t.Run("fails when string not found", func(t *testing.T) {
		tmpDir, svc := setupTestVault(t)
		defer cleanupTestVault(t, tmpDir)

		content := "# Test Note\n\nSome content here."
		testPath := "test-note.md"
		os.WriteFile(filepath.Join(tmpDir, testPath), []byte(content), 0o644)

		result := svc.PatchNote(types.PatchNoteParams{
			Path:       testPath,
			OldString:  "non-existent string",
			NewString:  "replacement",
			ReplaceAll: false,
		})

		if result.Success {
			t.Error("Success = true, want false")
		}
		if result.MatchCount != 0 {
			t.Errorf("MatchCount = %d, want 0", result.MatchCount)
		}
		if !strings.Contains(result.Message, "String not found") {
			t.Errorf("Message should mention string not found: %s", result.Message)
		}
	})

	t.Run("fails when oldString equals newString", func(t *testing.T) {
		tmpDir, svc := setupTestVault(t)
		defer cleanupTestVault(t, tmpDir)

		content := "# Test\n\nSome content"
		testPath := "test-note.md"
		os.WriteFile(filepath.Join(tmpDir, testPath), []byte(content), 0o644)

		result := svc.PatchNote(types.PatchNoteParams{
			Path:       testPath,
			OldString:  "same",
			NewString:  "same",
			ReplaceAll: false,
		})

		if result.Success {
			t.Error("Success = true, want false")
		}
		if !strings.Contains(result.Message, "must be different") {
			t.Errorf("Message should mention strings must be different: %s", result.Message)
		}
	})

	t.Run("fails for filtered paths", func(t *testing.T) {
		tmpDir, svc := setupTestVault(t)
		defer cleanupTestVault(t, tmpDir)

		result := svc.PatchNote(types.PatchNoteParams{
			Path:       ".obsidian/config.json",
			OldString:  "old",
			NewString:  "new",
			ReplaceAll: false,
		})

		if result.Success {
			t.Error("Success = true, want false")
		}
		if !strings.Contains(result.Message, "Access denied") {
			t.Errorf("Message should mention access denied: %s", result.Message)
		}
	})

	t.Run("handles regex special characters literally", func(t *testing.T) {
		tmpDir, svc := setupTestVault(t)
		defer cleanupTestVault(t, tmpDir)

		content := "Price: $10.50 (special)"
		testPath := "test-note.md"
		os.WriteFile(filepath.Join(tmpDir, testPath), []byte(content), 0o644)

		result := svc.PatchNote(types.PatchNoteParams{
			Path:       testPath,
			OldString:  "$10.50",
			NewString:  "$15.75",
			ReplaceAll: false,
		})

		if !result.Success {
			t.Errorf("Success = false, want true. Message: %s", result.Message)
		}

		note, _ := svc.ReadNote(testPath)
		if !strings.Contains(note.Content, "$15.75") {
			t.Error("Content should contain '$15.75'")
		}
	})
}

func TestService_DeleteNote(t *testing.T) {
	t.Run("with correct confirmation", func(t *testing.T) {
		tmpDir, svc := setupTestVault(t)
		defer cleanupTestVault(t, tmpDir)

		content := "# Test Note\n\nThis is a test note to be deleted."
		testPath := "test-note.md"
		os.WriteFile(filepath.Join(tmpDir, testPath), []byte(content), 0o644)

		result := svc.DeleteNote(types.DeleteNoteParams{
			Path:        testPath,
			ConfirmPath: testPath,
		})

		if !result.Success {
			t.Errorf("Success = false, want true. Message: %s", result.Message)
		}
		if !strings.Contains(result.Message, "Successfully deleted") {
			t.Errorf("Message should mention successful deletion: %s", result.Message)
		}
	})

	t.Run("reject with incorrect confirmation", func(t *testing.T) {
		tmpDir, svc := setupTestVault(t)
		defer cleanupTestVault(t, tmpDir)

		content := "# Test Note\n\nThis note should not be deleted."
		testPath := "test-note.md"
		os.WriteFile(filepath.Join(tmpDir, testPath), []byte(content), 0o644)

		result := svc.DeleteNote(types.DeleteNoteParams{
			Path:        testPath,
			ConfirmPath: "wrong-path.md",
		})

		if result.Success {
			t.Error("Success = true, want false")
		}
		if !strings.Contains(result.Message, "confirmation path does not match") {
			t.Errorf("Message should mention confirmation mismatch: %s", result.Message)
		}

		// File should still exist
		if !svc.Exists(testPath) {
			t.Error("File should still exist")
		}
	})

	t.Run("handle non-existent file", func(t *testing.T) {
		tmpDir, svc := setupTestVault(t)
		defer cleanupTestVault(t, tmpDir)

		result := svc.DeleteNote(types.DeleteNoteParams{
			Path:        "non-existent.md",
			ConfirmPath: "non-existent.md",
		})

		if result.Success {
			t.Error("Success = true, want false")
		}
		if !strings.Contains(result.Message, "File not found") {
			t.Errorf("Message should mention file not found: %s", result.Message)
		}
	})

	t.Run("reject deletion of filtered paths", func(t *testing.T) {
		tmpDir, svc := setupTestVault(t)
		defer cleanupTestVault(t, tmpDir)

		result := svc.DeleteNote(types.DeleteNoteParams{
			Path:        ".obsidian/app.json",
			ConfirmPath: ".obsidian/app.json",
		})

		if result.Success {
			t.Error("Success = true, want false")
		}
		if !strings.Contains(result.Message, "Access denied") {
			t.Errorf("Message should mention access denied: %s", result.Message)
		}
	})

	t.Run("handle directory deletion attempt", func(t *testing.T) {
		tmpDir, svc := setupTestVault(t)
		defer cleanupTestVault(t, tmpDir)

		testDir := "test-directory"
		os.MkdirAll(filepath.Join(tmpDir, testDir), 0o755)

		result := svc.DeleteNote(types.DeleteNoteParams{
			Path:        testDir,
			ConfirmPath: testDir,
		})

		if result.Success {
			t.Error("Success = true, want false")
		}
		if !strings.Contains(result.Message, "is not a file") {
			t.Errorf("Message should mention not a file: %s", result.Message)
		}
	})
}

func TestService_WriteNote(t *testing.T) {
	t.Run("write with frontmatter", func(t *testing.T) {
		tmpDir, svc := setupTestVault(t)
		defer cleanupTestVault(t, tmpDir)

		err := svc.WriteNote(types.NoteWriteParams{
			Path:    "test.md",
			Content: "This is test content.",
			Frontmatter: map[string]any{
				"title":   "Test Note",
				"tags":    []string{"test", "example"},
				"created": "2023-01-01",
			},
		})
		if err != nil {
			t.Fatalf("WriteNote() error = %v", err)
		}

		note, err := svc.ReadNote("test.md")
		if err != nil {
			t.Fatalf("ReadNote() error = %v", err)
		}

		if note.Frontmatter["title"] != "Test Note" {
			t.Errorf("Frontmatter[title] = %v, want %q", note.Frontmatter["title"], "Test Note")
		}
		if !strings.Contains(note.Content, "This is test content.") {
			t.Error("Content should contain the written content")
		}
	})

	t.Run("append mode preserves frontmatter", func(t *testing.T) {
		tmpDir, svc := setupTestVault(t)
		defer cleanupTestVault(t, tmpDir)

		// Write initial note
		svc.WriteNote(types.NoteWriteParams{
			Path:        "append-test.md",
			Content:     "Original content.",
			Frontmatter: map[string]any{"title": "Original", "status": "draft"},
		})

		// Append to note
		err := svc.WriteNote(types.NoteWriteParams{
			Path:        "append-test.md",
			Content:     "\nAppended content.",
			Frontmatter: map[string]any{"updated": "2023-12-01"},
			Mode:        "append",
		})
		if err != nil {
			t.Fatalf("WriteNote(append) error = %v", err)
		}

		note, _ := svc.ReadNote("append-test.md")

		if note.Frontmatter["title"] != "Original" {
			t.Errorf("Frontmatter[title] = %v, want %q", note.Frontmatter["title"], "Original")
		}
		if note.Frontmatter["status"] != "draft" {
			t.Errorf("Frontmatter[status] = %v, want %q", note.Frontmatter["status"], "draft")
		}
		if note.Frontmatter["updated"] != "2023-12-01" {
			t.Errorf("Frontmatter[updated] = %v, want %q", note.Frontmatter["updated"], "2023-12-01")
		}
		if !strings.Contains(note.Content, "Original content.") || !strings.Contains(note.Content, "Appended content.") {
			t.Errorf("Content should contain both original and appended content: %s", note.Content)
		}
	})

	t.Run("invalid mode returns error", func(t *testing.T) {
		tmpDir, svc := setupTestVault(t)
		defer cleanupTestVault(t, tmpDir)

		err := svc.WriteNote(types.NoteWriteParams{
			Path:    "invalid-mode.md",
			Content: "content",
			Mode:    "merge",
		})
		if err == nil {
			t.Fatal("WriteNote() error = nil, want invalid mode error")
		}
		if !strings.Contains(err.Error(), "invalid write mode") {
			t.Fatalf("WriteNote() error = %q, want invalid write mode", err)
		}

		if svc.Exists("invalid-mode.md") {
			t.Fatal("WriteNote() created note for invalid mode")
		}
	})
}

func TestService_PathTraversal(t *testing.T) {
	tmpDir, svc := setupTestVault(t)
	defer cleanupTestVault(t, tmpDir)

	tests := []string{
		"../outside.md",
		"folder/../../outside.md",
	}

	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			_, err := svc.ReadNote(path)
			if err == nil {
				t.Error("ReadNote() should fail for path traversal")
			}
			if !strings.Contains(strings.ToLower(err.Error()), "path traversal not allowed") {
				t.Errorf("Error should mention path traversal: %v", err)
			}
		})
	}
}

func TestService_UnicodeAndEmoji(t *testing.T) {
	t.Run("unicode in file paths", func(t *testing.T) {
		tmpDir, svc := setupTestVault(t)
		defer cleanupTestVault(t, tmpDir)

		testPath := "日本語/ノート.md"
		content := "# Japanese note"

		os.MkdirAll(filepath.Join(tmpDir, "日本語"), 0o755)
		os.WriteFile(filepath.Join(tmpDir, testPath), []byte(content), 0o644)

		note, err := svc.ReadNote(testPath)
		if err != nil {
			t.Fatalf("ReadNote() error = %v", err)
		}
		if !strings.Contains(note.Content, "Japanese note") {
			t.Error("Content should contain 'Japanese note'")
		}
	})

	t.Run("emoji in file paths", func(t *testing.T) {
		tmpDir, svc := setupTestVault(t)
		defer cleanupTestVault(t, tmpDir)

		testPath := "📁/🎉.md"
		content := "# Emoji note"

		os.MkdirAll(filepath.Join(tmpDir, "📁"), 0o755)
		os.WriteFile(filepath.Join(tmpDir, testPath), []byte(content), 0o644)

		note, err := svc.ReadNote(testPath)
		if err != nil {
			t.Fatalf("ReadNote() error = %v", err)
		}
		if !strings.Contains(note.Content, "Emoji note") {
			t.Error("Content should contain 'Emoji note'")
		}
	})
}

func TestService_MoveNote(t *testing.T) {
	t.Run("move to new location", func(t *testing.T) {
		tmpDir, svc := setupTestVault(t)
		defer cleanupTestVault(t, tmpDir)

		content := "# Test Note"
		os.WriteFile(filepath.Join(tmpDir, "original.md"), []byte(content), 0o644)

		result := svc.MoveNote(types.MoveNoteParams{
			OldPath: "original.md",
			NewPath: "moved.md",
		})

		if !result.Success {
			t.Errorf("Success = false, want true. Message: %s", result.Message)
		}

		if svc.Exists("original.md") {
			t.Error("Original file should not exist")
		}
		if !svc.Exists("moved.md") {
			t.Error("Moved file should exist")
		}
	})

	t.Run("fail without overwrite when target exists", func(t *testing.T) {
		tmpDir, svc := setupTestVault(t)
		defer cleanupTestVault(t, tmpDir)

		os.WriteFile(filepath.Join(tmpDir, "source.md"), []byte("Source"), 0o644)
		os.WriteFile(filepath.Join(tmpDir, "target.md"), []byte("Target"), 0o644)

		result := svc.MoveNote(types.MoveNoteParams{
			OldPath:   "source.md",
			NewPath:   "target.md",
			Overwrite: false,
		})

		if result.Success {
			t.Error("Success = true, want false")
		}
		if !strings.Contains(result.Message, "already exists") {
			t.Errorf("Message should mention target exists: %s", result.Message)
		}
	})
}

func TestService_ListDirectory(t *testing.T) {
	tmpDir, svc := setupTestVault(t)
	defer cleanupTestVault(t, tmpDir)

	// Create some test files and directories
	os.MkdirAll(filepath.Join(tmpDir, "subdir"), 0o755)
	os.WriteFile(filepath.Join(tmpDir, "note1.md"), []byte("# Note 1"), 0o644)
	os.WriteFile(filepath.Join(tmpDir, "note2.md"), []byte("# Note 2"), 0o644)

	listing, err := svc.ListDirectory("")
	if err != nil {
		t.Fatalf("ListDirectory() error = %v", err)
	}

	if len(listing.Directories) != 1 || listing.Directories[0] != "subdir" {
		t.Errorf("Directories = %v, want [subdir]", listing.Directories)
	}

	hasNote1 := false
	hasNote2 := false
	for _, f := range listing.Files {
		if f == "note1.md" {
			hasNote1 = true
		}
		if f == "note2.md" {
			hasNote2 = true
		}
	}
	if !hasNote1 || !hasNote2 {
		t.Errorf("Files should contain note1.md and note2.md: %v", listing.Files)
	}
}
