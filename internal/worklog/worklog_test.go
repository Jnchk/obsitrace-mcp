package worklog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTaskLifecycleAndSummary(t *testing.T) {
	root := t.TempDir()
	service, err := New(root, Config{Timezone: "Asia/Shanghai"})
	if err != nil {
		t.Fatal(err)
	}
	requestID := "550e8400-e29b-41d4-a716-446655440000"
	task, duplicates, err := service.CreateTask(requestID, "Prepare release", "Prepare notes", "Platform", "high", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(duplicates) != 0 || task.ID != "task-"+requestID {
		t.Fatalf("unexpected create result: task=%+v duplicates=%v", task, duplicates)
	}
	if task.DetailPath != "Obsitrace/Tasks/Platform/"+task.ID+".md" {
		t.Fatalf("detail path = %q, want project-specific path", task.DetailPath)
	}
	assertTaskView(t, root, "Obsitrace/Index/Task Ongoing.md", task.ID, true)
	assertTaskView(t, root, "Obsitrace/Index/Task Done.md", task.ID, false)

	tasks, total, err := service.QueryTasks(TaskFilter{Statuses: []string{"todo"}, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(tasks) != 1 || tasks[0].Description != "Prepare notes" {
		t.Fatalf("unexpected query result: total=%d tasks=%+v", total, tasks)
	}

	status := "in_progress"
	progress := "Implementation is underway"
	updated, err := service.UpdateTask(TaskUpdate{
		RequestID: "550e8400-e29b-41d4-a716-446655440001", ID: task.ID,
		Status: &status, Progress: progress,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != status {
		t.Fatalf("status = %q, want %q", updated.Status, status)
	}

	logDate := time.Now().In(service.location).Format("2006-01-02")
	logPath, err := service.RecordWork(WorkEntry{
		RequestID: "550e8400-e29b-41d4-a716-446655440002",
		Date:      logDate, Category: "achievement", Text: "Published release notes", TaskIDs: []string{task.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	summary, err := service.GetSummary(SummaryOptions{Period: "day"})
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.Facts) < 3 {
		t.Fatalf("expected create, update and work facts; got %+v", summary.Facts)
	}
	if len(summary.SourcePaths) != 2 || !contains(summary.SourcePaths, logPath) {
		t.Fatalf("unexpected summary source paths: %v", summary.SourcePaths)
	}
	logContent, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(logPath)))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(logContent), "[tasks:") || !strings.Contains(string(logContent), "%%worklog:") {
		t.Fatalf("daily log is not in readable, parseable format:\n%s", logContent)
	}
	if !strings.Contains(string(logContent), "[[Obsitrace/Tasks/Platform/"+task.ID+".md|"+task.ID+"]]") {
		t.Fatalf("daily log does not link to the task's actual detail path:\n%s", logContent)
	}
	for _, fact := range summary.Facts {
		if fact.Text == "" {
			t.Fatalf("readable log parser returned an empty fact: %+v", fact)
		}
	}
}

func TestProgressMovesTodoToInProgressWithoutOverridingExplicitStatus(t *testing.T) {
	service, err := New(t.TempDir(), Config{})
	if err != nil {
		t.Fatal(err)
	}

	todoTask, _, err := service.CreateTask("550e8400-e29b-41d4-a716-446655440040", "Start work", "", "Platform", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := service.UpdateTask(TaskUpdate{
		RequestID: "550e8400-e29b-41d4-a716-446655440041",
		ID:        todoTask.ID, Progress: "Started implementation",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != "in_progress" {
		t.Fatalf("status after progress update = %q, want in_progress", updated.Status)
	}

	blockedTask, _, err := service.CreateTask("550e8400-e29b-41d4-a716-446655440042", "Keep blocked", "", "Platform", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	blocked := "blocked"
	blockedTask, err = service.UpdateTask(TaskUpdate{
		RequestID: "550e8400-e29b-41d4-a716-446655440043",
		ID:        blockedTask.ID, Status: &blocked,
	})
	if err != nil {
		t.Fatal(err)
	}
	blockedTask, err = service.UpdateTask(TaskUpdate{
		RequestID: "550e8400-e29b-41d4-a716-446655440044",
		ID:        blockedTask.ID, Progress: "Investigated the blocker",
	})
	if err != nil {
		t.Fatal(err)
	}
	if blockedTask.Status != "blocked" {
		t.Fatalf("status after blocked task progress = %q, want blocked", blockedTask.Status)
	}

	explicitTask, _, err := service.CreateTask("550e8400-e29b-41d4-a716-446655440045", "Respect explicit status", "", "Platform", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	done := "done"
	explicitTask, err = service.UpdateTask(TaskUpdate{
		RequestID: "550e8400-e29b-41d4-a716-446655440046",
		ID:        explicitTask.ID, Status: &done, Progress: "Completed and verified",
	})
	if err != nil {
		t.Fatal(err)
	}
	if explicitTask.Status != done {
		t.Fatalf("explicit status = %q, want %q", explicitTask.Status, done)
	}

	emptyProgressTask, _, err := service.CreateTask("550e8400-e29b-41d4-a716-446655440047", "Ignore blank progress", "", "Platform", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	emptyProgressTask, err = service.UpdateTask(TaskUpdate{
		RequestID: "550e8400-e29b-41d4-a716-446655440048",
		ID:        emptyProgressTask.ID, Progress: "   ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if emptyProgressTask.Status != "todo" {
		t.Fatalf("status after blank progress = %q, want todo", emptyProgressTask.Status)
	}
}

func TestTaskViewsFollowStatusAndPreserveTaskIndex(t *testing.T) {
	root := t.TempDir()
	service, err := New(root, Config{})
	if err != nil {
		t.Fatal(err)
	}
	task, _, err := service.CreateTask("550e8400-e29b-41d4-a716-446655440003", "Move task to done", "", "Team Management", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	status := "done"
	project := "Product Platform"
	task, err = service.UpdateTask(TaskUpdate{
		RequestID: "550e8400-e29b-41d4-a716-446655440004", ID: task.ID, Status: &status, Project: &project,
	})
	if err != nil {
		t.Fatal(err)
	}
	if task.DetailPath != "Obsitrace/Tasks/Product Platform/"+task.ID+".md" {
		t.Fatalf("updated detail path = %q, want project-specific path", task.DetailPath)
	}
	if _, err := os.Stat(filepath.Join(root, "Worklog", "Tasks", "Team Management", task.ID+".md")); !os.IsNotExist(err) {
		t.Fatalf("old project detail should be moved, stat error = %v", err)
	}
	assertTaskView(t, root, "Obsitrace/Index/Task Index.md", task.ID, true)
	assertTaskView(t, root, "Obsitrace/Index/Task Ongoing.md", task.ID, false)
	assertTaskView(t, root, "Obsitrace/Index/Task Done.md", task.ID, true)

	cancelled, _, err := service.CreateTask("550e8400-e29b-41d4-a716-446655440007", "Cancelled task", "", "Team Management", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	cancelledStatus := "cancelled"
	cancelled, err = service.UpdateTask(TaskUpdate{
		RequestID: "550e8400-e29b-41d4-a716-446655440008", ID: cancelled.ID, Status: &cancelledStatus,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertTaskView(t, root, "Obsitrace/Index/Task Ongoing.md", cancelled.ID, false)
	assertTaskView(t, root, "Obsitrace/Index/Task Done.md", cancelled.ID, true)

	blocked, _, err := service.CreateTask("550e8400-e29b-41d4-a716-446655440009", "Blocked task", "", "Team Management", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	blockedStatus := "blocked"
	blocked, err = service.UpdateTask(TaskUpdate{
		RequestID: "550e8400-e29b-41d4-a716-44665544000a", ID: blocked.ID, Status: &blockedStatus,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertTaskView(t, root, "Obsitrace/Index/Task Ongoing.md", blocked.ID, true)
	assertTaskView(t, root, "Obsitrace/Index/Task Done.md", blocked.ID, false)
}

func TestSyncTasksUsesDetailH1AndRefreshesViews(t *testing.T) {
	root := t.TempDir()
	service, err := New(root, Config{})
	if err != nil {
		t.Fatal(err)
	}
	task, _, err := service.CreateTask("550e8400-e29b-41d4-a716-446655440030", "Original title", "", "Platform", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	detailPath := filepath.Join(root, filepath.FromSlash(task.DetailPath))
	detail, err := os.ReadFile(detailPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(detailPath, []byte(strings.Replace(string(detail), "# Original title", "# Updated title", 1)), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := service.SyncTasks()
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || result.Updated != 1 || len(result.UpdatedPaths) != 3 {
		t.Fatalf("unexpected sync result: %+v", result)
	}
	tasks, total, err := service.QueryTasks(TaskFilter{Query: "Updated title"})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(tasks) != 1 || tasks[0].Title != "Updated title" {
		t.Fatalf("index title was not synchronized: total=%d tasks=%+v", total, tasks)
	}
	for _, path := range []string{service.IndexRelativePath(), service.TaskViewPaths()[0]} {
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(content), "Updated title") || strings.Contains(string(content), "Original title") {
			t.Fatalf("view %q was not synchronized:\n%s", path, content)
		}
	}
	donePath := filepath.Join(root, filepath.FromSlash(service.TaskViewPaths()[1]))
	done, err := os.ReadFile(donePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(done), "Original title") {
		t.Fatalf("done view contains a stale title:\n%s", done)
	}
}

func TestSyncTasksValidatesAllDetailsBeforeWriting(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(string) string
	}{
		{name: "missing H1", edit: func(note string) string { return strings.Replace(note, "# Original title", "## Original title", 1) }},
		{name: "mismatched ID", edit: func(note string) string {
			return strings.Replace(note, "id: task-550e8400-e29b-41d4-a716-446655440031", "id: task-other", 1)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			service, err := New(root, Config{})
			if err != nil {
				t.Fatal(err)
			}
			task, _, err := service.CreateTask("550e8400-e29b-41d4-a716-446655440031", "Original title", "", "Platform", "", "", false)
			if err != nil {
				t.Fatal(err)
			}
			paths := append([]string{service.IndexRelativePath()}, service.TaskViewPaths()...)
			before := make([][]byte, len(paths))
			for i, path := range paths {
				before[i], err = os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
				if err != nil {
					t.Fatal(err)
				}
			}
			detailPath := filepath.Join(root, filepath.FromSlash(task.DetailPath))
			detail, err := os.ReadFile(detailPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(detailPath, []byte(test.edit(string(detail))), 0o600); err != nil {
				t.Fatal(err)
			}

			if _, err := service.SyncTasks(); err == nil {
				t.Fatal("expected invalid task detail error")
			}
			for i, path := range paths {
				after, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
				if err != nil {
					t.Fatal(err)
				}
				if string(after) != string(before[i]) {
					t.Fatalf("%s changed despite invalid task detail", path)
				}
			}
		})
	}
}

func TestReadIndexParsesTableTaskRecords(t *testing.T) {
	root := t.TempDir()
	service, err := New(root, Config{})
	if err != nil {
		t.Fatal(err)
	}
	task, _, err := service.CreateTask("550e8400-e29b-41d4-a716-446655440006", "Legacy task", "", "General", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(root, "Obsitrace", "Tasks", "General", task.ID+".md")
	legacyPath := filepath.Join(root, "Obsitrace", "Tasks", "Project", task.ID+".md")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(oldPath, legacyPath); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(root, filepath.FromSlash(service.IndexRelativePath()))
	index, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	indexText := strings.Replace(string(index), "[["+task.DetailPath+"\\|"+task.ID+"]]",
		"[[Obsitrace/Tasks/Project/"+task.ID+".md\\|"+task.ID+"]]", 1)
	indexText = strings.Replace(indexText, "| todo |", "| done |", 1)
	index = []byte(indexText)
	if err := os.WriteFile(indexPath, index, 0o644); err != nil {
		t.Fatal(err)
	}
	tasks, _, err := service.QueryTasks(TaskFilter{Query: task.ID})
	if err != nil || len(tasks) != 1 || tasks[0].DetailPath != "Obsitrace/Tasks/Project/"+task.ID+".md" || tasks[0].Status != "done" {
		t.Fatalf("task table row was not parsed: tasks=%+v err=%v", tasks, err)
	}
}

func TestParseTableRowPreservesEscapedPipesAndWikiAliases(t *testing.T) {
	row := `| task-id | a title with \| escaped pipe | todo | project |  | created | updated |  |  | [[Tasks/Project/task.md\|task-id]] | |`
	cells := trimTrailingEmptyCells(parseTableRow(row))
	if len(cells) != len(indexHeader) || cells[1] != "a title with | escaped pipe" || cells[9] != "[[Tasks/Project/task.md|task-id]]" {
		t.Fatalf("unexpected parsed cells: %#v", cells)
	}
	path, ok := parseDetailLink(cells[9], "task-id")
	if !ok || path != "Tasks/Project/task.md" {
		t.Fatalf("unexpected detail path %q, valid=%t", path, ok)
	}
}

func TestURLsBecomeClickableInTaskHistoryAndLogs(t *testing.T) {
	root := t.TempDir()
	service, err := New(root, Config{})
	if err != nil {
		t.Fatal(err)
	}
	task, _, err := service.CreateTask(
		"550e8400-e29b-41d4-a716-446655440040",
		"Redmine task",
		"Ticket: https://redmine.example/issues/14331#note-64.",
		"General", "", "", false,
	)
	if err != nil {
		t.Fatal(err)
	}
	status := "in_progress"
	progress := "Posted an update at https://redmine.example/issues/14331#note-64."
	description := "Details: https://redmine.example/issues/14331#note-64."
	task, err = service.UpdateTask(TaskUpdate{
		RequestID: "550e8400-e29b-41d4-a716-446655440041",
		ID:        task.ID, Status: &status, Progress: progress, Description: &description,
	})
	if err != nil {
		t.Fatal(err)
	}
	detail, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(task.DetailPath)))
	if err != nil {
		t.Fatal(err)
	}
	clickableURL := "[https://redmine.example/issues/14331#note-64](<https://redmine.example/issues/14331#note-64>)"
	if !strings.Contains(string(detail), clickableURL) ||
		!strings.Contains(string(detail), "Details: "+clickableURL+".") {
		t.Fatalf("task note URLs were not rendered as clickable links:\n%s", detail)
	}

	logPath, err := service.DailyLogPath(task.Updated)
	if err != nil {
		t.Fatal(err)
	}
	log, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(logPath)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), clickableURL) {
		t.Fatalf("daily log URL was not rendered as a clickable link:\n%s", log)
	}
	if _, err := service.UpdateTask(TaskUpdate{
		RequestID: "550e8400-e29b-41d4-a716-446655440041",
		ID:        task.ID, Status: &status, Progress: progress,
	}); err != nil {
		t.Fatalf("retry with URL failed: %v", err)
	}
}

func TestLinkifyURLsPreservesExistingMarkdownLinks(t *testing.T) {
	input := "Bare https://example.com/page. Existing [label](https://already.example/path) and <https://auto.example/path>."
	want := "Bare [https://example.com/page](<https://example.com/page>). Existing [label](https://already.example/path) and <https://auto.example/path>."
	if got := linkifyURLs(input); got != want {
		t.Fatalf("linkifyURLs() = %q, want %q", got, want)
	}
}

func TestLinkifyURLsSeparatesAdjacentText(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{
			input: "https://example.com/issues/1后续内容",
			want:  "[https://example.com/issues/1](<https://example.com/issues/1>) 后续内容",
		},
		{
			input: "讨论（https://example.com/issues/1#note-9）的结果",
			want:  "讨论（[https://example.com/issues/1#note-9](<https://example.com/issues/1#note-9>)） 的结果",
		},
		{
			input: "See https://example.com/path, then continue",
			want:  "See [https://example.com/path](<https://example.com/path>), then continue",
		},
		{
			input: "[https://example.com/path](<https://example.com/path>)",
			want:  "[https://example.com/path](<https://example.com/path>)",
		},
	}
	for _, test := range tests {
		if got := linkifyURLs(test.input); got != test.want {
			t.Errorf("linkifyURLs(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}

func TestParseLegacyLogMissingTextSeparator(t *testing.T) {
	line := "- 2026-11-09T16:31:09+08:00 [task] [created] [tasks:task-550e8400-e29b-41d4-a716-446655440000]Task title [event:550e8400-e29b-41d4-a716-446655440000]"
	fact, ok := parseLogLine(line, "Obsitrace/Logs/2026/11/2026-11-09.md")
	if !ok || fact.Text != "Task title" {
		t.Fatalf("legacy log line was not parsed: fact=%+v ok=%t", fact, ok)
	}
}

func TestCreateTaskRequiresSafeProjectName(t *testing.T) {
	service, err := New(t.TempDir(), Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, project := range []string{"", "../outside", `Team/Management`, "CON"} {
		if _, _, err := service.CreateTask("550e8400-e29b-41d4-a716-446655440005", "Task", "", project, "", "", false); err == nil {
			t.Fatalf("expected project %q to be rejected", project)
		}
	}
}

func assertTaskView(t *testing.T, root, relative, taskID string, want bool) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Contains(string(content), taskID)
	if got != want {
		t.Fatalf("%s contains task %q = %t, want %t", relative, taskID, got, want)
	}
}

func TestCreateTaskDuplicateAndRetry(t *testing.T) {
	service, err := New(t.TempDir(), Config{})
	if err != nil {
		t.Fatal(err)
	}
	task, _, err := service.CreateTask("550e8400-e29b-41d4-a716-446655440010", "Same title", "", "General", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	_, candidates, err := service.CreateTask("550e8400-e29b-41d4-a716-446655440011", "Same title", "", "General", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].ID != task.ID {
		t.Fatalf("unexpected duplicate candidates: %+v", candidates)
	}
	retried, _, err := service.CreateTask("550e8400-e29b-41d4-a716-446655440010", "Same title", "", "General", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if retried.ID != task.ID {
		t.Fatalf("retry created a different task: %s vs %s", retried.ID, task.ID)
	}
}

func TestAllTimeSummaryUsesPreservedStartedDate(t *testing.T) {
	root := t.TempDir()
	service, err := New(root, Config{})
	if err != nil {
		t.Fatal(err)
	}
	task, _, err := service.CreateTask("550e8400-e29b-41d4-a716-446655440015", "Long-running task", "", "General", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(root, filepath.FromSlash(service.IndexRelativePath()))
	index, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	index = []byte(strings.Replace(string(index), "started: "+task.Created, "started: 2020-01-02", 1))
	if err := os.WriteFile(indexPath, index, 0o644); err != nil {
		t.Fatal(err)
	}
	status := "in_progress"
	if _, err := service.UpdateTask(TaskUpdate{
		RequestID: "550e8400-e29b-41d4-a716-446655440016",
		ID:        task.ID, Status: &status, Progress: "Started",
	}); err != nil {
		t.Fatal(err)
	}
	summary, err := service.GetSummary(SummaryOptions{Period: "all"})
	if err != nil {
		t.Fatal(err)
	}
	if summary.From != "2020-01-02" {
		t.Fatalf("all-time summary starts %q, want 2020-01-02", summary.From)
	}
}

func TestConfigRejectsInvalidTimezoneAndTraversal(t *testing.T) {
	if _, err := New(t.TempDir(), Config{Timezone: "Not/AZone"}); err == nil {
		t.Fatal("expected invalid timezone to fail")
	}
	if _, err := New(t.TempDir(), Config{TaskDir: "inside\\..\\outside"}); err == nil {
		t.Fatal("expected path traversal to fail")
	}
}

func TestUpdateRetryDoesNotDuplicateEvent(t *testing.T) {
	root := t.TempDir()
	service, err := New(root, Config{})
	if err != nil {
		t.Fatal(err)
	}
	task, _, err := service.CreateTask("550e8400-e29b-41d4-a716-446655440020", "Retry task", "", "General", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	status := "done"
	update := TaskUpdate{
		RequestID: "550e8400-e29b-41d4-a716-446655440021",
		ID:        task.ID, Status: &status, Progress: "Finished and verified",
	}
	if _, err := service.UpdateTask(update); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(service.logRelativePath(time.Now().In(service.location)))))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateTask(update); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(service.logRelativePath(time.Now().In(service.location)))))
	if err != nil {
		t.Fatal(err)
	}
	marker := "|" + update.RequestID + "%%"
	if strings.Count(string(second), marker) != 1 {
		t.Fatalf("expected one update event; got %d marker occurrences:\n%s", strings.Count(string(second), marker), second)
	}
	if strings.Count(string(first), marker) != strings.Count(string(second), marker) {
		t.Fatal("retry changed daily log event count")
	}
}

func TestSaveSummaryRejectsOverwriteAndStaleDigest(t *testing.T) {
	root := t.TempDir()
	service, err := New(root, Config{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RecordWork(WorkEntry{
		RequestID: "550e8400-e29b-41d4-a716-446655440030", Text: "Completed a review",
	}); err != nil {
		t.Fatal(err)
	}
	summary, err := service.GetSummary(SummaryOptions{Period: "day"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SaveSummary(summary, "Daily summary", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SaveSummary(summary, "Replacement", ""); err == nil {
		t.Fatal("expected existing summary to be protected")
	}
	if _, err := service.SaveSummary(summary, "Alternate", "daily-copy"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RecordWork(WorkEntry{
		RequestID: "550e8400-e29b-41d4-a716-446655440031", Text: "Added another record",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SaveSummary(summary, "Stale", "stale"); err == nil {
		t.Fatal("expected stale source digest to be rejected")
	}
}

func TestWriteAtomicReplacesExistingFile(t *testing.T) {
	root := t.TempDir()
	service, err := New(root, Config{})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "note.md")
	if err := os.WriteFile(path, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := service.writeAtomicChecked(path, []byte("after"), []byte("before"), true); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "after" {
		t.Fatalf("content = %q, want after", content)
	}
}
