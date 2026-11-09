package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Jnchk/obsitrace-mcp/internal/filesystem"
	"github.com/Jnchk/obsitrace-mcp/internal/frontmatter"
	"github.com/Jnchk/obsitrace-mcp/internal/pathfilter"
)

func setupTestVault(t *testing.T) string {
	t.Helper()

	vaultPath := t.TempDir()
	pf := pathfilter.New(nil)
	fh := frontmatter.New()
	fileSystem = filesystem.New(vaultPath, pf, fh)

	return vaultPath
}

func writeTestNote(t *testing.T, vaultPath, relPath, content string) {
	t.Helper()

	fullPath := filepath.Join(vaultPath, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func TestExtractTags(t *testing.T) {
	frontmatter := map[string]any{
		"tags": []any{"Project", "go/mcp", "Project"},
	}
	content := "# Title\n\nInline tags: #Daily #go/mcp #go_mcp"

	got := extractTags(frontmatter, content)
	want := []string{"daily", "go/mcp", "go_mcp", "project"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("extractTags() = %v, want %v", got, want)
	}
}

func TestExtractLinks(t *testing.T) {
	content := "See [[Note One]], [[note one|alias]], [[Second Note#Heading]], and [[Third]]."

	got := extractLinks(content)
	want := []string{"note one", "second note", "third"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("extractLinks() = %v, want %v", got, want)
	}
}

func TestAddRelation(t *testing.T) {
	tests := []struct {
		name     string
		existing string
		newRel   string
		want     string
	}{
		{name: "empty", existing: "", newRel: "backlink", want: "backlink"},
		{name: "append", existing: "shared-tags", newRel: "backlink", want: "shared-tags,backlink"},
		{name: "dedupe", existing: "shared-tags,backlink", newRel: "backlink", want: "shared-tags,backlink"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got := addRelation(testCase.existing, testCase.newRel)
			if got != testCase.want {
				t.Fatalf("addRelation(%q, %q) = %q, want %q", testCase.existing, testCase.newRel, got, testCase.want)
			}
		})
	}
}

func TestHandleListDefaultsToRoot(t *testing.T) {
	vaultPath := setupTestVault(t)

	writeTestNote(t, vaultPath, "note.md", "# test\n")
	if err := os.Mkdir(filepath.Join(vaultPath, "projects"), 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}

	_, got, err := handleList(context.Background(), nil, ListInput{})
	if err != nil {
		t.Fatalf("handleList() error = %v", err)
	}

	wantFiles := []string{"note.md"}
	wantDirs := []string{"projects"}
	if !reflect.DeepEqual(got.Files, wantFiles) {
		t.Fatalf("handleList().Files = %v, want %v", got.Files, wantFiles)
	}
	if !reflect.DeepEqual(got.Directories, wantDirs) {
		t.Fatalf("handleList().Directories = %v, want %v", got.Directories, wantDirs)
	}
}

func TestHandleRelatedDefaultsToTagsAndLinks(t *testing.T) {
	vaultPath := setupTestVault(t)

	writeTestNote(t, vaultPath, "source.md", "---\ntags:\n  - project\n---\nSee [[Friend]].\n")
	writeTestNote(t, vaultPath, "friend.md", "---\ntags: [project]\n---\nBacklink to [[Source]].\n")
	writeTestNote(t, vaultPath, "tag-only.md", "---\ntags: [project]\n---\nNo links here.\n")
	writeTestNote(t, vaultPath, "link-only.md", "No shared tags, but points to [[Source]].\n")
	writeTestNote(t, vaultPath, ".hidden/ignored.md", "---\ntags: [project]\n---\n[[Source]]\n")

	_, got, err := handleRelated(context.Background(), nil, RelatedInput{Path: "source.md"})
	if err != nil {
		t.Fatalf("handleRelated() error = %v", err)
	}

	want := []RelatedNote{
		{Path: "friend.md", Relation: "shared-tags,backlink,outgoing", Tags: []string{"project"}},
		{Path: "link-only.md", Relation: "backlink"},
		{Path: "tag-only.md", Relation: "shared-tags", Tags: []string{"project"}},
	}

	if !reflect.DeepEqual(got.Related, want) {
		t.Fatalf("handleRelated().Related = %#v, want %#v", got.Related, want)
	}
}

func TestHandleTagsCountsTaggedNotes(t *testing.T) {
	vaultPath := setupTestVault(t)

	writeTestNote(t, vaultPath, "one.md", "---\ntags: [project, alpha]\n---\nBody with #alpha\n")
	writeTestNote(t, vaultPath, "two.md", "Body with #project and #beta\n")
	writeTestNote(t, vaultPath, "three.md", "No tags here\n")
	writeTestNote(t, vaultPath, ".hidden/ignored.md", "Body with #project\n")

	_, got, err := handleTags(context.Background(), nil, TagsInput{})
	if err != nil {
		t.Fatalf("handleTags() error = %v", err)
	}

	want := TagsOutput{
		Tags: []TagInfo{
			{Tag: "alpha", Count: 1},
			{Tag: "beta", Count: 1},
			{Tag: "project", Count: 2},
		},
		TotalTags:     3,
		TotalNotes:    3,
		NotesWithTags: 2,
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("handleTags() = %#v, want %#v", got, want)
	}
}
