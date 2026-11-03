// Package filesystem provides file system operations for the Obsidian vault.
package filesystem

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/taigrr/obsidian-mcp/internal/frontmatter"
	"github.com/taigrr/obsidian-mcp/internal/pathfilter"
	"github.com/taigrr/obsidian-mcp/internal/types"
)

// Service provides file system operations for the Obsidian vault.
type Service struct {
	vaultPath          string
	pathFilter         *pathfilter.PathFilter
	frontmatterHandler *frontmatter.Handler
}

// New creates a new FileSystemService.
func New(vaultPath string, pf *pathfilter.PathFilter, fh *frontmatter.Handler) *Service {
	absPath, _ := filepath.Abs(vaultPath)
	if pf == nil {
		pf = pathfilter.New(nil)
	}
	if fh == nil {
		fh = frontmatter.New()
	}
	return &Service{
		vaultPath:          absPath,
		pathFilter:         pf,
		frontmatterHandler: fh,
	}
}

// ResolvePath resolves a relative path within the vault and validates it.
func (s *Service) ResolvePath(relativePath string) (string, error) {
	if relativePath == "" {
		relativePath = ""
	}

	// Trim whitespace
	relativePath = strings.TrimSpace(relativePath)

	// Normalize and resolve the path within the vault
	normalizedPath := relativePath
	normalizedPath = strings.TrimPrefix(normalizedPath, "/")

	fullPath := filepath.Join(s.vaultPath, normalizedPath)
	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		return "", err
	}

	// Security check: ensure path is within vault
	relPath, err := filepath.Rel(s.vaultPath, absPath)
	if err != nil {
		return "", err
	}
	if relPath == ".." || strings.HasPrefix(relPath, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path traversal not allowed: %s", relativePath)
	}

	return absPath, nil
}

// ReadNote reads a note from the vault.
func (s *Service) ReadNote(path string) (types.ParsedNote, error) {
	fullPath, err := s.ResolvePath(path)
	if err != nil {
		return types.ParsedNote{}, err
	}

	if !s.pathFilter.IsAllowed(path) {
		return types.ParsedNote{}, fmt.Errorf("access denied: %s", path)
	}

	// Check if the path is a directory first
	isDir, _ := s.IsDirectory(path)
	if isDir {
		return types.ParsedNote{}, fmt.Errorf("cannot read directory as file: %s. Use list_directory tool instead", path)
	}

	content, err := os.ReadFile(fullPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return types.ParsedNote{}, fmt.Errorf("file not found: %s", path)
		}
		if errors.Is(err, fs.ErrPermission) {
			return types.ParsedNote{}, fmt.Errorf("permission denied: %s", path)
		}
		return types.ParsedNote{}, fmt.Errorf("failed to read file: %s - %w", path, err)
	}

	return s.frontmatterHandler.Parse(string(content)), nil
}

// WriteNote writes a note to the vault.
func (s *Service) WriteNote(params types.NoteWriteParams) error {
	path := params.Path
	content := params.Content
	fm := params.Frontmatter
	mode := params.Mode
	if mode == "" {
		mode = "overwrite"
	}
	if mode != "overwrite" && mode != "append" && mode != "prepend" {
		return fmt.Errorf("invalid write mode %q: must be overwrite, append, or prepend", mode)
	}

	fullPath, err := s.ResolvePath(path)
	if err != nil {
		return err
	}

	if !s.pathFilter.IsAllowed(path) {
		return fmt.Errorf("access denied: %s", path)
	}

	// Validate frontmatter if provided
	if fm != nil {
		validation := s.frontmatterHandler.Validate(fm)
		if !validation.IsValid {
			return fmt.Errorf("invalid frontmatter: %s", strings.Join(validation.Errors, ", "))
		}
	}

	var finalContent string

	if mode == "overwrite" {
		if fm != nil {
			finalContent, err = s.frontmatterHandler.Stringify(fm, content)
			if err != nil {
				return err
			}
		} else {
			finalContent = content
		}
	} else {
		// For append/prepend, we need to read existing content
		existingNote, readErr := s.ReadNote(path)
		if readErr != nil {
			// File doesn't exist, treat as overwrite
			if fm != nil {
				finalContent, err = s.frontmatterHandler.Stringify(fm, content)
				if err != nil {
					return err
				}
			} else {
				finalContent = content
			}
		} else {
			// Merge frontmatter if provided
			mergedFrontmatter := existingNote.Frontmatter
			if fm != nil {
				if mergedFrontmatter == nil {
					mergedFrontmatter = make(map[string]any)
				}
				maps.Copy(mergedFrontmatter, fm)
			}

			var newContent string
			switch mode {
			case "append":
				newContent = existingNote.Content + content
			case "prepend":
				newContent = content + existingNote.Content
			}

			finalContent, err = s.frontmatterHandler.Stringify(mergedFrontmatter, newContent)
			if err != nil {
				return err
			}
		}
	}

	// Create directories if they don't exist
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if err := os.WriteFile(fullPath, []byte(finalContent), 0o644); err != nil {
		return fmt.Errorf("failed to write file: %s - %w", path, err)
	}

	return nil
}

// PatchNote patches a note by replacing a specific string.
func (s *Service) PatchNote(params types.PatchNoteParams) types.PatchNoteResult {
	path := params.Path
	oldString := params.OldString
	newString := params.NewString
	replaceAll := params.ReplaceAll

	if !s.pathFilter.IsAllowed(path) {
		return types.PatchNoteResult{
			Success: false,
			Path:    path,
			Message: fmt.Sprintf("Access denied: %s", path),
		}
	}

	// Validate that strings are not empty
	if oldString == "" || strings.TrimSpace(oldString) == "" {
		return types.PatchNoteResult{
			Success: false,
			Path:    path,
			Message: "oldString cannot be empty",
		}
	}

	if newString == "" {
		return types.PatchNoteResult{
			Success: false,
			Path:    path,
			Message: "newString cannot be empty",
		}
	}

	// Validate that oldString and newString are different
	if oldString == newString {
		return types.PatchNoteResult{
			Success: false,
			Path:    path,
			Message: "oldString and newString must be different",
		}
	}

	// Read the existing note
	note, err := s.ReadNote(path)
	if err != nil {
		return types.PatchNoteResult{
			Success: false,
			Path:    path,
			Message: fmt.Sprintf("Failed to patch note: %v", err),
		}
	}

	// Get the full content with frontmatter
	fullContent := note.OriginalContent

	// Count occurrences of oldString
	occurrences := strings.Count(fullContent, oldString)

	if occurrences == 0 {
		truncated := oldString
		if len(truncated) > 50 {
			truncated = truncated[:50] + "..."
		}
		return types.PatchNoteResult{
			Success:    false,
			Path:       path,
			Message:    fmt.Sprintf("String not found in note: \"%s\"", truncated),
			MatchCount: 0,
		}
	}

	// If not replaceAll and multiple occurrences exist, fail
	if !replaceAll && occurrences > 1 {
		return types.PatchNoteResult{
			Success:    false,
			Path:       path,
			Message:    fmt.Sprintf("Found %d occurrences of the string. Use replaceAll=true to replace all occurrences, or provide a more specific string to match exactly one occurrence.", occurrences),
			MatchCount: occurrences,
		}
	}

	// Perform the replacement
	var updatedContent string
	if replaceAll {
		updatedContent = strings.ReplaceAll(fullContent, oldString, newString)
	} else {
		updatedContent = strings.Replace(fullContent, oldString, newString, 1)
	}

	// Write the updated content
	fullPath, err := s.ResolvePath(path)
	if err != nil {
		return types.PatchNoteResult{
			Success: false,
			Path:    path,
			Message: fmt.Sprintf("Failed to resolve path: %v", err),
		}
	}

	if err := os.WriteFile(fullPath, []byte(updatedContent), 0o644); err != nil {
		return types.PatchNoteResult{
			Success: false,
			Path:    path,
			Message: fmt.Sprintf("Failed to write file: %v", err),
		}
	}

	replacedCount := 1
	if replaceAll {
		replacedCount = occurrences
	}

	plural := ""
	if replacedCount > 1 {
		plural = "s"
	}

	return types.PatchNoteResult{
		Success:    true,
		Path:       path,
		Message:    fmt.Sprintf("Successfully replaced %d occurrence%s", replacedCount, plural),
		MatchCount: occurrences,
	}
}

// ListDirectory lists files and directories in the vault.
func (s *Service) ListDirectory(path string) (types.DirectoryListing, error) {
	// Normalize path: treat '.' as root directory
	if path == "." {
		path = ""
	}

	fullPath, err := s.ResolvePath(path)
	if err != nil {
		return types.DirectoryListing{}, err
	}

	entries, err := os.ReadDir(fullPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return types.DirectoryListing{}, fmt.Errorf("directory not found: %s", path)
		}
		if errors.Is(err, fs.ErrPermission) {
			return types.DirectoryListing{}, fmt.Errorf("permission denied: %s", path)
		}
		return types.DirectoryListing{}, fmt.Errorf("failed to list directory: %s - %w", path, err)
	}

	var files, directories []string

	for _, entry := range entries {
		var entryPath string
		if path != "" {
			entryPath = path + "/" + entry.Name()
		} else {
			entryPath = entry.Name()
		}

		if !s.pathFilter.IsAllowed(entryPath) {
			continue
		}

		if entry.IsDir() {
			directories = append(directories, entry.Name())
		} else if entry.Type().IsRegular() {
			files = append(files, entry.Name())
		}
	}

	sort.Strings(files)
	sort.Strings(directories)

	return types.DirectoryListing{
		Files:       files,
		Directories: directories,
	}, nil
}

// Exists checks if a path exists in the vault.
func (s *Service) Exists(path string) bool {
	fullPath, err := s.ResolvePath(path)
	if err != nil {
		return false
	}

	if !s.pathFilter.IsAllowed(path) {
		return false
	}

	_, err = os.Stat(fullPath)
	return err == nil
}

// IsDirectory checks if a path is a directory.
func (s *Service) IsDirectory(path string) (bool, error) {
	fullPath, err := s.ResolvePath(path)
	if err != nil {
		return false, err
	}

	if !s.pathFilter.IsAllowed(path) {
		return false, nil
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		return false, nil
	}

	return info.IsDir(), nil
}

// DeleteNote deletes a note from the vault.
func (s *Service) DeleteNote(params types.DeleteNoteParams) types.DeleteResult {
	path := params.Path
	confirmPath := params.ConfirmPath

	// Confirmation check - paths must match exactly
	if path != confirmPath {
		return types.DeleteResult{
			Success: false,
			Path:    path,
			Message: "Deletion cancelled: confirmation path does not match. For safety, both 'path' and 'confirmPath' must be identical.",
		}
	}

	fullPath, err := s.ResolvePath(path)
	if err != nil {
		return types.DeleteResult{
			Success: false,
			Path:    path,
			Message: fmt.Sprintf("Failed to resolve path: %v", err),
		}
	}

	if !s.pathFilter.IsAllowed(path) {
		return types.DeleteResult{
			Success: false,
			Path:    path,
			Message: fmt.Sprintf("Access denied: %s", path),
		}
	}

	// Check if it's a directory first
	isDir, _ := s.IsDirectory(path)
	if isDir {
		return types.DeleteResult{
			Success: false,
			Path:    path,
			Message: fmt.Sprintf("Cannot delete: %s is not a file", path),
		}
	}

	// Perform the deletion
	if err := os.Remove(fullPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return types.DeleteResult{
				Success: false,
				Path:    path,
				Message: fmt.Sprintf("File not found: %s", path),
			}
		}
		if errors.Is(err, fs.ErrPermission) {
			return types.DeleteResult{
				Success: false,
				Path:    path,
				Message: fmt.Sprintf("Permission denied: %s", path),
			}
		}
		return types.DeleteResult{
			Success: false,
			Path:    path,
			Message: fmt.Sprintf("Failed to delete file: %s - %v", path, err),
		}
	}

	return types.DeleteResult{
		Success: true,
		Path:    path,
		Message: fmt.Sprintf("Successfully deleted note: %s. This action cannot be undone.", path),
	}
}

// MoveNote moves or renames a note in the vault.
func (s *Service) MoveNote(params types.MoveNoteParams) types.MoveResult {
	oldPath := params.OldPath
	newPath := params.NewPath
	overwrite := params.Overwrite

	if !s.pathFilter.IsAllowed(oldPath) {
		return types.MoveResult{
			Success: false,
			OldPath: oldPath,
			NewPath: newPath,
			Message: fmt.Sprintf("Access denied: %s", oldPath),
		}
	}

	if !s.pathFilter.IsAllowed(newPath) {
		return types.MoveResult{
			Success: false,
			OldPath: oldPath,
			NewPath: newPath,
			Message: fmt.Sprintf("Access denied: %s", newPath),
		}
	}

	oldFullPath, err := s.ResolvePath(oldPath)
	if err != nil {
		return types.MoveResult{
			Success: false,
			OldPath: oldPath,
			NewPath: newPath,
			Message: fmt.Sprintf("Failed to resolve old path: %v", err),
		}
	}

	newFullPath, err := s.ResolvePath(newPath)
	if err != nil {
		return types.MoveResult{
			Success: false,
			OldPath: oldPath,
			NewPath: newPath,
			Message: fmt.Sprintf("Failed to resolve new path: %v", err),
		}
	}

	// Read source content
	content, err := os.ReadFile(oldFullPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return types.MoveResult{
				Success: false,
				OldPath: oldPath,
				NewPath: newPath,
				Message: fmt.Sprintf("Source file not found: %s", oldPath),
			}
		}
		return types.MoveResult{
			Success: false,
			OldPath: oldPath,
			NewPath: newPath,
			Message: fmt.Sprintf("Failed to read source file: %v", err),
		}
	}

	// Create directories if needed
	dir := filepath.Dir(newFullPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return types.MoveResult{
			Success: false,
			OldPath: oldPath,
			NewPath: newPath,
			Message: fmt.Sprintf("Failed to create directory: %v", err),
		}
	}

	// Check if target exists
	if !overwrite {
		if _, err := os.Stat(newFullPath); err == nil {
			return types.MoveResult{
				Success: false,
				OldPath: oldPath,
				NewPath: newPath,
				Message: fmt.Sprintf("Target file already exists: %s. Use overwrite=true to replace it.", newPath),
			}
		}
	}

	// Write to new location
	if err := os.WriteFile(newFullPath, content, 0o644); err != nil {
		return types.MoveResult{
			Success: false,
			OldPath: oldPath,
			NewPath: newPath,
			Message: fmt.Sprintf("Failed to write to new location: %v", err),
		}
	}

	// Delete the source file
	if err := os.Remove(oldFullPath); err != nil {
		return types.MoveResult{
			Success: false,
			OldPath: oldPath,
			NewPath: newPath,
			Message: fmt.Sprintf("Failed to delete source file: %v", err),
		}
	}

	return types.MoveResult{
		Success: true,
		OldPath: oldPath,
		NewPath: newPath,
		Message: fmt.Sprintf("Successfully moved note from %s to %s", oldPath, newPath),
	}
}

// GetVaultPath returns the vault path.
func (s *Service) GetVaultPath() string {
	return s.vaultPath
}
