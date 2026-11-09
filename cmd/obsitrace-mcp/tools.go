package main

import "github.com/modelcontextprotocol/go-sdk/mcp"

type (
	// ReadInput contains parameters for reading a note.
	ReadInput struct {
		Path   string `json:"path" jsonschema:"Path to the note relative to vault root"`
		Offset int    `json:"offset,omitempty" jsonschema:"Line offset to start reading from (default: 0)"`
		Limit  int    `json:"limit,omitempty" jsonschema:"Maximum number of lines to return (default: all)"`
	}

	// ReadOutput contains the result of reading a note.
	ReadOutput struct {
		Frontmatter map[string]any `json:"fm,omitempty"`
		Content     string         `json:"content"`
		TotalLines  int            `json:"totalLines"`
		Truncated   bool           `json:"truncated,omitempty"`
	}

	// WriteInput contains parameters for writing a note.
	WriteInput struct {
		Path        string         `json:"path" jsonschema:"Path to the note relative to vault root"`
		Content     string         `json:"content" jsonschema:"Content of the note"`
		Frontmatter map[string]any `json:"frontmatter,omitempty" jsonschema:"Frontmatter object (optional)"`
	}

	// WriteOutput contains the result of writing a note.
	WriteOutput struct {
		Success bool   `json:"success"`
		Path    string `json:"path"`
	}

	// DeleteInput contains parameters for deleting a note.
	DeleteInput struct {
		Path    string `json:"path" jsonschema:"Path to the note relative to vault root"`
		Confirm string `json:"confirm" jsonschema:"Must be set to 'yes' to confirm deletion"`
	}

	// DeleteOutput contains the result of deleting a note.
	DeleteOutput struct {
		Success bool   `json:"success"`
		Path    string `json:"path"`
	}

	// RenameInput contains parameters for renaming/moving a note.
	RenameInput struct {
		Path      string `json:"path" jsonschema:"Current path of the note"`
		NewPath   string `json:"newPath" jsonschema:"New path for the note"`
		Overwrite bool   `json:"overwrite,omitempty" jsonschema:"Allow overwriting existing file (default: false)"`
	}

	// RenameOutput contains the result of renaming a note.
	RenameOutput struct {
		Success bool   `json:"success"`
		OldPath string `json:"oldPath"`
		NewPath string `json:"newPath"`
	}

	// EditInput contains parameters for editing a note.
	EditInput struct {
		Path        string         `json:"path" jsonschema:"Path to the note relative to vault root"`
		OldText     string         `json:"oldText,omitempty" jsonschema:"Exact text to replace (for content edits)"`
		NewText     string         `json:"newText,omitempty" jsonschema:"New text to insert in place of oldText"`
		ReplaceAll  bool           `json:"replaceAll,omitempty" jsonschema:"If true, replace all occurrences of oldText"`
		Frontmatter map[string]any `json:"frontmatter,omitempty" jsonschema:"Frontmatter fields to update (merged with existing)"`
	}

	// EditOutput contains the result of editing a note.
	EditOutput struct {
		Success      bool   `json:"success"`
		Path         string `json:"path"`
		Replacements int    `json:"replacements,omitempty"`
	}

	// SearchInput contains parameters for searching notes.
	SearchInput struct {
		Query         string `json:"query" jsonschema:"Search query (plain text or regex if useRegex=true)"`
		UseRegex      bool   `json:"useRegex,omitempty" jsonschema:"Treat query as regex pattern (default: false)"`
		CaseSensitive bool   `json:"caseSensitive,omitempty" jsonschema:"Case sensitive search (default: false)"`
		ContextLines  int    `json:"contextLines,omitempty" jsonschema:"Lines of context before/after match (default: 2)"`
		Limit         int    `json:"limit,omitempty" jsonschema:"Maximum results (default: 15)"`
		Offset        int    `json:"offset,omitempty" jsonschema:"Skip first N results for pagination (default: 0)"`
	}

	// SearchMatch represents a single match within a file.
	SearchMatch struct {
		Line    int    `json:"line"`
		Context string `json:"context"`
		IsTag   bool   `json:"isTag,omitempty"`
	}

	// SearchResultItem represents search results for a single file.
	SearchResultItem struct {
		Path    string        `json:"path"`
		Matches []SearchMatch `json:"matches"`
	}

	// SearchOutput contains search results.
	SearchOutput struct {
		Results    []SearchResultItem `json:"results"`
		TotalFiles int                `json:"totalFiles"`
		HasMore    bool               `json:"hasMore,omitempty"`
	}

	// RelatedInput contains parameters for finding related notes.
	RelatedInput struct {
		Path  string `json:"path" jsonschema:"Path to the note relative to vault root"`
		Tags  bool   `json:"tags,omitempty" jsonschema:"Find notes sharing tags with this note (default: false)"`
		Links bool   `json:"links,omitempty" jsonschema:"Find notes linked to/from this note (default: false)"`
	}

	// RelatedNote represents a related note.
	RelatedNote struct {
		Path     string   `json:"path"`
		Relation string   `json:"relation"`
		Tags     []string `json:"tags,omitempty"`
	}

	// RelatedOutput contains related notes.
	RelatedOutput struct {
		Path    string        `json:"path"`
		Related []RelatedNote `json:"related"`
	}

	// TagsInput contains parameters for listing all tags.
	TagsInput struct{}

	// TagInfo represents a tag with its occurrence count.
	TagInfo struct {
		Tag   string `json:"tag"`
		Count int    `json:"count"`
	}

	// TagsOutput contains all unique tags in the vault with counts.
	TagsOutput struct {
		Tags          []TagInfo `json:"tags"`
		TotalTags     int       `json:"totalTags"`
		TotalNotes    int       `json:"totalNotes"`
		NotesWithTags int       `json:"notesWithTags"`
	}

	// ListInput contains parameters for listing a directory.
	ListInput struct {
		Path string `json:"path,omitempty" jsonschema:"Directory path relative to vault root (default: root)"`
	}

	// ListOutput contains directory listing results.
	ListOutput struct {
		Files       []string `json:"files"`
		Directories []string `json:"directories"`
	}
)

func registerTools(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "read",
		Description: "Read a note from the vault. Returns frontmatter and content. Supports pagination with offset/limit for large files.",
	}, handleRead)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "write",
		Description: "Create or overwrite a note in the vault with the given content and optional frontmatter.",
	}, handleWrite)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete",
		Description: "Delete a note from the vault. Requires confirm='yes' for safety.",
	}, handleDelete)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "rename",
		Description: "Move or rename a note to a new path.",
	}, handleRename)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "edit",
		Description: "Edit a note by replacing text and/or updating frontmatter. For text replacement, oldText must match exactly. For frontmatter, fields are merged with existing.",
	}, handleEdit)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "search",
		Description: "Full-text search across all notes. Supports regex and case-insensitive search. Results sorted by tag matches first, then content matches. Returns matching lines with context.",
	}, handleSearch)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "related",
		Description: "Find notes related to a given note. Use tags=true to find notes sharing tags, links=true to find notes that link to or are linked from this note.",
	}, handleRelated)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "tags",
		Description: "List all unique tags across the vault with occurrence counts. Returns tags from both frontmatter and inline #tags.",
	}, handleTags)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list",
		Description: "List files and subdirectories in a vault directory. Defaults to vault root if no path provided.",
	}, handleList)

	registerWorklogTools(server)
}
