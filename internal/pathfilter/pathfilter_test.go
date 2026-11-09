package pathfilter

import (
	"strings"
	"testing"

	"github.com/Jnchk/obsitrace-mcp/internal/types"
)

func TestPathFilter_AllowsMarkdownFiles(t *testing.T) {
	filter := New(nil)

	tests := []struct {
		path string
		want bool
	}{
		{"notes/test.md", true},
		{"test.markdown", true},
		{"folder/subfolder/note.txt", true},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := filter.IsAllowed(tt.path); got != tt.want {
				t.Errorf("IsAllowed(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestPathFilter_BlocksObsidianDirectory(t *testing.T) {
	filter := New(nil)

	tests := []string{
		".obsidian/app.json",
		".obsidian/plugins/plugin/main.js",
	}

	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			if filter.IsAllowed(path) {
				t.Errorf("IsAllowed(%q) = true, want false", path)
			}
		})
	}
}

func TestPathFilter_BlocksGitDirectory(t *testing.T) {
	filter := New(nil)

	tests := []string{
		".git/config",
		".git/objects/abc123",
	}

	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			if filter.IsAllowed(path) {
				t.Errorf("IsAllowed(%q) = true, want false", path)
			}
		})
	}
}

func TestPathFilter_BlocksNodeModules(t *testing.T) {
	filter := New(nil)
	path := "node_modules/package/index.js"

	if filter.IsAllowed(path) {
		t.Errorf("IsAllowed(%q) = true, want false", path)
	}
}

func TestPathFilter_BlocksSystemFiles(t *testing.T) {
	filter := New(nil)

	tests := []string{
		".DS_Store",
		"Thumbs.db",
	}

	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			if filter.IsAllowed(path) {
				t.Errorf("IsAllowed(%q) = true, want false", path)
			}
		})
	}
}

func TestPathFilter_BlocksNonAllowedExtensions(t *testing.T) {
	filter := New(nil)

	tests := []string{
		"script.js",
		"data.json",
		"image.png",
	}

	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			if filter.IsAllowed(path) {
				t.Errorf("IsAllowed(%q) = true, want false", path)
			}
		})
	}
}

func TestPathFilter_RegexSpecialCharacters(t *testing.T) {
	filter := New(nil)

	tests := []struct {
		name string
		path string
		want bool
	}{
		{"dots in filenames", "file.name.md", true},
		{"version notes", "v1.0.0-notes.md", true},
		{"parentheses in paths", "notes/(archived)/old.md", true},
		{"project copy", "project (copy).md", true},
		{"square brackets", "notes/[2024]/january.md", true},
		{"inbox brackets", "[inbox]/task.md", true},
		{"curly braces", "templates/{daily}.md", true},
		{"plus signs", "C++/notes.md", true},
		{"topic plus", "topic+subtopic.md", true},
		{"question mark", "FAQ?.md", true},
		{"asterisk in filename", "important*.md", true},
		{"pipe character", "option|choice.md", true},
		{"caret", "version^2.md", true},
		{"dollar sign", "price$100.md", true},
		{"dollar HOME", "$HOME/notes.md", true},
		{"backslash Windows", "folder\\subfolder\\note.md", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := filter.IsAllowed(tt.path); got != tt.want {
				t.Errorf("IsAllowed(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestPathFilter_CustomIgnoredPatterns(t *testing.T) {
	t.Run("asterisk glob matches", func(t *testing.T) {
		filter := New(&types.PathFilterConfig{
			IgnoredPatterns: []string{"temp*/**"},
		})

		tests := []struct {
			path string
			want bool
		}{
			{"temp/file.md", false},
			{"temp1/file.md", false},
			{"temporary/file.md", false},
			{"atemp/file.md", true},
		}

		for _, tt := range tests {
			if got := filter.IsAllowed(tt.path); got != tt.want {
				t.Errorf("IsAllowed(%q) = %v, want %v", tt.path, got, tt.want)
			}
		}
	})

	t.Run("double asterisk matches nested", func(t *testing.T) {
		filter := New(&types.PathFilterConfig{
			IgnoredPatterns: []string{"archive/**"},
		})

		tests := []struct {
			path string
			want bool
		}{
			{"archive/old.md", false},
			{"archive/2024/jan/note.md", false},
			{"other/archive/note.md", true},
		}

		for _, tt := range tests {
			if got := filter.IsAllowed(tt.path); got != tt.want {
				t.Errorf("IsAllowed(%q) = %v, want %v", tt.path, got, tt.want)
			}
		}
	})

	t.Run("custom pattern with dots", func(t *testing.T) {
		filter := New(&types.PathFilterConfig{
			IgnoredPatterns: []string{"backup.2024/**"},
		})

		tests := []struct {
			path string
			want bool
		}{
			{"backup.2024/notes.md", false},
			{"backup_2024/notes.md", true},
		}

		for _, tt := range tests {
			if got := filter.IsAllowed(tt.path); got != tt.want {
				t.Errorf("IsAllowed(%q) = %v, want %v", tt.path, got, tt.want)
			}
		}
	})

	t.Run("custom pattern with parentheses", func(t *testing.T) {
		filter := New(&types.PathFilterConfig{
			IgnoredPatterns: []string{"(archive)/**"},
		})

		tests := []struct {
			path string
			want bool
		}{
			{"(archive)/old.md", false},
			{"archive/old.md", true},
		}

		for _, tt := range tests {
			if got := filter.IsAllowed(tt.path); got != tt.want {
				t.Errorf("IsAllowed(%q) = %v, want %v", tt.path, got, tt.want)
			}
		}
	})

	t.Run("custom pattern with brackets", func(t *testing.T) {
		filter := New(&types.PathFilterConfig{
			IgnoredPatterns: []string{"[trash]/**"},
		})

		tests := []struct {
			path string
			want bool
		}{
			{"[trash]/deleted.md", false},
			{"trash/deleted.md", true},
		}

		for _, tt := range tests {
			if got := filter.IsAllowed(tt.path); got != tt.want {
				t.Errorf("IsAllowed(%q) = %v, want %v", tt.path, got, tt.want)
			}
		}
	})
}

func TestPathFilter_PathTraversal(t *testing.T) {
	filter := New(&types.PathFilterConfig{
		IgnoredPatterns: []string{"../**"},
	})

	tests := []struct {
		path string
		want bool
	}{
		{"../secret.md", false},
		{"../../etc/passwd", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := filter.IsAllowed(tt.path); got != tt.want {
				t.Errorf("IsAllowed(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestPathFilter_FilterPaths(t *testing.T) {
	t.Run("filters array correctly", func(t *testing.T) {
		filter := New(nil)
		paths := []string{
			"notes/valid.md",
			".obsidian/config.json",
			"archive/old.md",
			".git/HEAD",
			"readme.txt",
		}

		got := filter.FilterPaths(paths)
		want := []string{
			"notes/valid.md",
			"archive/old.md",
			"readme.txt",
		}

		if len(got) != len(want) {
			t.Errorf("FilterPaths() returned %d items, want %d", len(got), len(want))
			return
		}

		for i, path := range got {
			if path != want[i] {
				t.Errorf("FilterPaths()[%d] = %q, want %q", i, path, want[i])
			}
		}
	})

	t.Run("handles empty array", func(t *testing.T) {
		filter := New(nil)
		got := filter.FilterPaths([]string{})
		if len(got) != 0 {
			t.Errorf("FilterPaths([]) = %v, want empty", got)
		}
	})

	t.Run("handles all blocked paths", func(t *testing.T) {
		filter := New(nil)
		paths := []string{
			".obsidian/app.json",
			".git/config",
			"node_modules/pkg/index.js",
		}
		got := filter.FilterPaths(paths)
		if len(got) != 0 {
			t.Errorf("FilterPaths() = %v, want empty", got)
		}
	})
}

func TestPathFilter_EdgeCases(t *testing.T) {
	filter := New(nil)

	t.Run("empty path", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("IsAllowed(\"\") panicked: %v", r)
			}
		}()
		filter.IsAllowed("")
	})

	t.Run("path with only extension", func(t *testing.T) {
		if !filter.IsAllowed(".md") {
			t.Error("IsAllowed(\".md\") = false, want true")
		}
	})

	t.Run("very long paths", func(t *testing.T) {
		var longPath strings.Builder
		for range 100 {
			longPath.WriteString("a/")
		}
		longPath.WriteString("note.md")

		defer func() {
			if r := recover(); r != nil {
				t.Errorf("IsAllowed(longPath) panicked: %v", r)
			}
		}()

		if !filter.IsAllowed(longPath.String()) {
			t.Error("IsAllowed(longPath) = false, want true")
		}
	})

	t.Run("unicode characters", func(t *testing.T) {
		tests := []string{
			"notes/日本語.md",
			"émojis/🎉.md",
			"中文/笔记.md",
		}

		for _, path := range tests {
			if !filter.IsAllowed(path) {
				t.Errorf("IsAllowed(%q) = false, want true", path)
			}
		}
	})

	t.Run("spaces in paths", func(t *testing.T) {
		if !filter.IsAllowed("my notes/important file.md") {
			t.Error("IsAllowed(\"my notes/important file.md\") = false, want true")
		}
	})

	t.Run("directories no extension", func(t *testing.T) {
		tests := []struct {
			path string
			want bool
		}{
			{"folder/subfolder/", true},
			{"notes", true},
		}

		for _, tt := range tests {
			if got := filter.IsAllowed(tt.path); got != tt.want {
				t.Errorf("IsAllowed(%q) = %v, want %v", tt.path, got, tt.want)
			}
		}
	})

	t.Run("directories with dots in names", func(t *testing.T) {
		tests := []struct {
			path string
			want bool
		}{
			{"1. Project", true},
			{"2. Archive", true},
			{"3.5 Research", true},
			{"1. Project/subfolder", true},
			{"1. Project/note.md", true},
			{"1. Project/file.js", false},
		}

		for _, tt := range tests {
			if got := filter.IsAllowed(tt.path); got != tt.want {
				t.Errorf("IsAllowed(%q) = %v, want %v", tt.path, got, tt.want)
			}
		}
	})
}
