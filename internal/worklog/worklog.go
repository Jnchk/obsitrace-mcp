package worklog

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

const (
	defaultIndexPath  = "Obsitrace/Index/Task Index.md"
	defaultTaskDir    = "Obsitrace/Tasks"
	defaultLogDir     = "Obsitrace/Logs"
	defaultSummaryDir = "Obsitrace/Summaries"
)

var (
	validID             = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-8][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)
	validStatuses       = map[string]bool{"todo": true, "in_progress": true, "done": true, "blocked": true, "cancelled": true}
	indexHeader         = []string{"ID", "Title", "Status", "Project", "Priority", "Created", "Updated", "Due", "Completed", "Details"}
	logLine             = regexp.MustCompile(`^- (\d{4}-\d{2}-\d{2})T\S+ \[([^\]]*)\] \[([^\]]*)\] \[tasks:([^\]]*)\] ?(.*) \[event:[0-9a-f-]+\]$`)
	readableLogLine     = regexp.MustCompile(`^- (.*) %%worklog:([^|]*)\|([^|]*)\|([^|]*)\|([^|]*)\|([0-9a-f-]+)%%$`)
	htmlReadableLogLine = regexp.MustCompile(`^- <!--worklog:([^|]*)\|([^|]*)\|([^|]*)\|([^|]*)\|([0-9a-f-]+)--> (.*)$`)
	bareURL             = regexp.MustCompile(`https?://[A-Za-z0-9\-._~:/?#\[\]@!$&'()*+,;=%]+`)
	autoLinkedURL       = regexp.MustCompile(`\[([^\]]+)\]\(<(https?://[^>]+)>\)`)
)

// Config contains worklog paths relative to the vault root.
type Config struct {
	Timezone   string
	IndexPath  string
	TaskDir    string
	LogDir     string
	SummaryDir string
}

// Task is the compact representation stored in the task index.
type Task struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Status       string `json:"status"`
	Project      string `json:"project,omitempty"`
	Priority     string `json:"priority,omitempty"`
	Created      string `json:"created"`
	Updated      string `json:"updated"`
	Due          string `json:"due,omitempty"`
	Completed    string `json:"completed,omitempty"`
	DetailPath   string `json:"detailPath"`
	Description  string `json:"description,omitempty"`
	LatestUpdate string `json:"latestUpdate,omitempty"`
}

type TaskSyncResult struct {
	Total        int
	Updated      int
	UpdatedPaths []string
}

// TaskFilter limits task queries.
type TaskFilter struct {
	Statuses []string
	Project  string
	Query    string
	Limit    int
	Offset   int
}

// TaskUpdate contains optional fields to update and a progress event.
type TaskUpdate struct {
	RequestID   string
	ID          string
	Title       *string
	Status      *string
	Project     *string
	Priority    *string
	Due         *string
	Description *string
	Progress    string
}

// WorkEntry is a dated, user-provided work record.
type WorkEntry struct {
	RequestID string
	Date      string
	Category  string
	Text      string
	TaskIDs   []string
}

// SummaryOptions selects a period. From and To override the named period.
type SummaryOptions struct {
	Period string
	From   string
	To     string
}

// Fact is a concise source item returned for a period summary.
type Fact struct {
	Date     string   `json:"date"`
	Category string   `json:"category"`
	Text     string   `json:"text"`
	TaskIDs  []string `json:"taskIds,omitempty"`
	Source   string   `json:"source"`
}

// Summary is the filtered result that the Skill turns into prose.
type Summary struct {
	Period       string   `json:"period"`
	From         string   `json:"from"`
	To           string   `json:"to"`
	Timezone     string   `json:"timezone"`
	Facts        []Fact   `json:"facts"`
	ActiveTasks  []Task   `json:"activeTasks"`
	SourcePaths  []string `json:"sourcePaths"`
	SourceDigest string   `json:"sourceDigest"`
	Truncated    bool     `json:"truncated"`
	SavedPath    string   `json:"savedPath,omitempty"`
}

// Service manages the Markdown worklog within one Obsidian vault.
type Service struct {
	root       string
	indexPath  string
	taskDir    string
	logDir     string
	summaryDir string
	location   *time.Location
	mu         sync.Mutex
}

type indexSnapshot struct {
	content string
	exists  bool
}

// New validates configuration and creates a worklog service.
func New(vaultRoot string, config Config) (*Service, error) {
	root, err := filepath.Abs(vaultRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve vault root: %w", err)
	}
	locationName := strings.TrimSpace(config.Timezone)
	if locationName == "" {
		locationName = "Asia/Shanghai"
	}
	location, err := time.LoadLocation(locationName)
	if err != nil {
		return nil, fmt.Errorf("invalid worklog timezone %q: %w", locationName, err)
	}
	if config.IndexPath == "" {
		config.IndexPath = defaultIndexPath
	}
	if config.TaskDir == "" {
		config.TaskDir = defaultTaskDir
	}
	if config.LogDir == "" {
		config.LogDir = defaultLogDir
	}
	if config.SummaryDir == "" {
		config.SummaryDir = defaultSummaryDir
	}

	for name, path := range map[string]string{
		"index path": config.IndexPath, "task directory": config.TaskDir,
		"log directory": config.LogDir, "summary directory": config.SummaryDir,
	} {
		if err := validateRelativePath(path); err != nil {
			return nil, fmt.Errorf("invalid %s: %w", name, err)
		}
	}
	if filepath.Ext(config.IndexPath) != ".md" {
		return nil, fmt.Errorf("invalid index path: must end in .md")
	}
	return &Service{
		root: root, indexPath: filepath.Clean(config.IndexPath),
		taskDir: filepath.Clean(config.TaskDir), logDir: filepath.Clean(config.LogDir),
		summaryDir: filepath.Clean(config.SummaryDir), location: location,
	}, nil
}

// QueryTasks returns matching tasks from the index and reads details only for matches.
func (s *Service) QueryTasks(filter TaskFilter) ([]Task, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tasks, _, _, _, err := s.readIndex()
	if err != nil {
		return nil, 0, err
	}
	filtered := make([]Task, 0, len(tasks))
	for _, task := range tasks {
		if len(filter.Statuses) > 0 && !contains(filter.Statuses, task.Status) {
			continue
		}
		if filter.Project != "" && !strings.EqualFold(task.Project, filter.Project) {
			continue
		}
		if filter.Query != "" && !strings.Contains(strings.ToLower(task.ID+" "+task.Title+" "+task.Project), strings.ToLower(filter.Query)) {
			continue
		}
		filtered = append(filtered, task)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].Status != filtered[j].Status {
			return filtered[i].Status < filtered[j].Status
		}
		if filtered[i].Due != filtered[j].Due {
			if filtered[i].Due == "" {
				return false
			}
			if filtered[j].Due == "" {
				return true
			}
			return filtered[i].Due < filtered[j].Due
		}
		return filtered[i].ID < filtered[j].ID
	})
	total := len(filtered)
	offset := max(filter.Offset, 0)
	if offset >= total {
		return []Task{}, total, nil
	}
	limit := filter.Limit
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	end := min(offset+limit, total)
	for i := offset; i < end; i++ {
		task, err := s.readTaskDetail(filtered[i])
		if err != nil {
			return nil, total, err
		}
		filtered[i].Description = task.Description
		filtered[i].LatestUpdate = task.LatestUpdate
	}
	return filtered[offset:end], total, nil
}

// SyncTasks copies each task detail H1 title into the index and regenerates its views.
func (s *Service) SyncTasks() (TaskSyncResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tasks, prefix, suffix, snapshot, err := s.readIndex()
	if err != nil {
		return TaskSyncResult{}, err
	}
	result := TaskSyncResult{
		Total:        len(tasks),
		UpdatedPaths: append([]string{s.IndexRelativePath()}, s.TaskViewPaths()...),
	}
	for i := range tasks {
		note, err := s.readRaw(tasks[i].DetailPath)
		if err != nil {
			return TaskSyncResult{}, fmt.Errorf("sync task %q detail %q: %w", tasks[i].ID, tasks[i].DetailPath, err)
		}
		var metadata map[string]any
		body, err := parseFrontmatter(note, &metadata)
		if err != nil || metadata["type"] != "work-task" || metadata["id"] != tasks[i].ID {
			return TaskSyncResult{}, fmt.Errorf("sync task %q detail %q has invalid frontmatter or mismatched ID", tasks[i].ID, tasks[i].DetailPath)
		}
		title, ok := taskH1(body)
		if !ok {
			return TaskSyncResult{}, fmt.Errorf("sync task %q detail %q has no non-empty H1 title", tasks[i].ID, tasks[i].DetailPath)
		}
		if tasks[i].Title != title {
			tasks[i].Title = title
			result.Updated++
		}
	}
	if err := s.writeIndex(tasks, prefix, suffix, time.Now().In(s.location), snapshot); err != nil {
		return TaskSyncResult{}, fmt.Errorf("sync task titles and regenerate task views: %w", err)
	}
	return result, nil
}

// CreateTask creates a task. Repeating the same request ID safely completes missing writes.
func (s *Service) CreateTask(requestID, title, description, project, priority, due string, confirmDuplicate bool) (Task, []Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validID.MatchString(requestID) {
		return Task{}, nil, fmt.Errorf("requestId must be a UUID")
	}
	title = cleanText(title)
	if title == "" {
		return Task{}, nil, fmt.Errorf("title cannot be empty")
	}
	if due != "" {
		if _, err := time.Parse("2006-01-02", due); err != nil {
			return Task{}, nil, fmt.Errorf("due must use YYYY-MM-DD")
		}
	}
	now := time.Now().In(s.location)
	id := "task-" + strings.ToLower(requestID)
	project, err := validateProjectName(project)
	if err != nil {
		return Task{}, nil, err
	}
	detailPath := s.taskRelativePath(id, project)
	task := Task{
		ID: id, Title: title, Status: "todo", Project: cleanText(project), Priority: cleanText(priority),
		Created: now.Format("2006-01-02"), Updated: now.Format("2006-01-02"), Due: due,
		DetailPath: detailPath,
	}
	indexTasks, prefix, suffix, snapshot, err := s.readIndex()
	if errors.Is(err, fs.ErrNotExist) {
		indexTasks = []Task{}
		prefix = "# Worklog Task Index\n\nThis index tracks the current status of all work tasks. See each linked task note for details.\n\n"
	} else if err != nil {
		return Task{}, nil, err
	}
	for _, existing := range indexTasks {
		if existing.ID == id {
			if existing.Title != task.Title {
				return Task{}, nil, fmt.Errorf("requestId already belongs to task %s with different input", id)
			}
			detailPath, resolveErr := s.resolve(existing.DetailPath)
			if resolveErr != nil {
				return Task{}, nil, resolveErr
			}
			detailContent, readErr := os.ReadFile(detailPath)
			if errors.Is(readErr, fs.ErrNotExist) {
				created, _ := time.ParseInLocation("2006-01-02", existing.Created, s.location)
				detail := taskDetail(existing, description, created.Format(time.RFC3339), requestID, "created", "Task created.")
				if writeErr := s.writeAtomicChecked(detailPath, []byte(detail), nil, false); writeErr != nil {
					return Task{}, nil, fmt.Errorf("restore missing task detail %q: %w", existing.DetailPath, writeErr)
				}
			} else if readErr != nil {
				return Task{}, nil, readErr
			} else if err := validateTaskDetail(string(detailContent), existing); err != nil {
				return Task{}, nil, err
			}
			created, parseErr := time.ParseInLocation("2006-01-02", existing.Created, s.location)
			if parseErr != nil {
				created = now
			}
			if logErr := s.appendEvent(created, requestID, "task", "created", existing.Title, []string{id}); logErr != nil {
				return Task{}, nil, fmt.Errorf("task exists but daily log recovery failed: %w", logErr)
			}
			if err := s.writeIndex(indexTasks, prefix, suffix, now, snapshot); err != nil {
				return Task{}, nil, fmt.Errorf("task exists but task views could not be refreshed: %w", err)
			}
			return existing, nil, nil
		}
	}
	var duplicates []Task
	for _, candidate := range indexTasks {
		if strings.EqualFold(candidate.Title, title) {
			duplicates = append(duplicates, candidate)
		}
	}
	if len(duplicates) > 0 && !confirmDuplicate {
		return Task{}, duplicates, nil
	}
	ts := now.Format(time.RFC3339)
	detail := taskDetail(task, description, ts, requestID, "created", "Task created.")
	resolvedDetailPath, err := s.resolve(task.DetailPath)
	if err != nil {
		return Task{}, nil, err
	}
	if existing, readErr := os.ReadFile(resolvedDetailPath); readErr == nil {
		if err := validateTaskDetail(string(existing), task); err != nil || !strings.Contains(string(existing), "[event:"+strings.ToLower(requestID)+"]") {
			return Task{}, nil, fmt.Errorf("refusing to overwrite existing task detail %q", task.DetailPath)
		}
		detail = string(existing)
	} else if errors.Is(readErr, fs.ErrNotExist) {
		if err := s.writeAtomicChecked(resolvedDetailPath, []byte(detail), nil, false); err != nil {
			return Task{}, nil, fmt.Errorf("write task detail %q: %w", task.DetailPath, err)
		}
	} else {
		return Task{}, nil, readErr
	}
	task.Description = cleanText(description)
	indexTasks = append(indexTasks, task)
	if err := s.writeIndex(indexTasks, prefix, suffix, now, snapshot); err != nil {
		return Task{}, nil, fmt.Errorf("task detail %q created but index update %q failed: %w", task.DetailPath, s.indexPath, err)
	}
	if err := s.appendEvent(now, requestID, "task", "created", task.Title, []string{id}); err != nil {
		return Task{}, nil, fmt.Errorf("task and index created but daily log %q update failed: %w", s.logRelativePath(now), err)
	}
	return task, nil, nil
}

// UpdateTask updates index fields and appends a progress event once per request ID.
func (s *Service) UpdateTask(update TaskUpdate) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validID.MatchString(update.RequestID) {
		return Task{}, fmt.Errorf("requestId must be a UUID")
	}
	tasks, prefix, suffix, snapshot, err := s.readIndex()
	if err != nil {
		return Task{}, err
	}
	idx := taskIndex(tasks, update.ID)
	if idx < 0 {
		return Task{}, fmt.Errorf("task %q not found", update.ID)
	}
	task := &tasks[idx]
	oldDetailPath := task.DetailPath
	detail, err := s.readRaw(task.DetailPath)
	if err != nil {
		return Task{}, err
	}
	eventTime := time.Now().In(s.location)
	if originalTime, ok := taskEventTime(detail, update.RequestID, s.location); ok {
		eventTime = originalTime
	}
	movingDetail := false
	if update.Project != nil {
		project, err := validateProjectName(*update.Project)
		if err != nil {
			return Task{}, err
		}
		if project != task.Project {
			newDetailPath := s.taskRelativePath(task.ID, project)
			if newDetailPath != task.DetailPath {
				if existing, readErr := s.readRaw(newDetailPath); readErr == nil {
					if err := validateTaskDetail(existing, *task); err != nil {
						return Task{}, fmt.Errorf("refusing to overwrite task detail %q: %w", newDetailPath, err)
					}
					if originalTime, ok := taskEventTime(existing, update.RequestID, s.location); ok {
						eventTime = originalTime
					} else {
						return Task{}, fmt.Errorf("refusing to overwrite existing task detail %q", newDetailPath)
					}
				} else if !errors.Is(readErr, fs.ErrNotExist) {
					return Task{}, readErr
				}
				task.DetailPath = newDetailPath
				movingDetail = true
			}
		}
		task.Project = project
	}
	if update.Title != nil {
		task.Title = cleanText(*update.Title)
		if task.Title == "" {
			return Task{}, fmt.Errorf("title cannot be empty")
		}
	}
	if update.Status != nil {
		if !validStatuses[*update.Status] {
			return Task{}, fmt.Errorf("invalid status %q", *update.Status)
		}
		previousStatus := task.Status
		task.Status = *update.Status
		if task.Status == "done" {
			if previousStatus != "done" || task.Completed == "" {
				task.Completed = eventTime.Format("2006-01-02")
			}
		} else {
			task.Completed = ""
		}
	}
	if update.Priority != nil {
		task.Priority = cleanText(*update.Priority)
	}
	if update.Due != nil {
		if *update.Due != "" {
			if _, err := time.Parse("2006-01-02", *update.Due); err != nil {
				return Task{}, fmt.Errorf("due must use YYYY-MM-DD")
			}
		}
		task.Due = *update.Due
	}
	task.Updated = eventTime.Format("2006-01-02")
	event := update.Progress
	if event == "" {
		event = "Task fields updated."
	}
	updated, err := appendTaskEvent(detail, update.RequestID, eventTime.Format(time.RFC3339), statusLabel(update.Status, task.Status), event, update.Description, update.Title)
	if err != nil {
		return Task{}, err
	}
	detailPath, err := s.resolve(task.DetailPath)
	if err != nil {
		return Task{}, err
	}
	expected, expectedExists := []byte(detail), true
	if movingDetail {
		if existing, readErr := os.ReadFile(detailPath); readErr == nil {
			expected = existing
		} else if errors.Is(readErr, fs.ErrNotExist) {
			expected, expectedExists = nil, false
		} else {
			return Task{}, readErr
		}
	}
	if err := s.writeAtomicChecked(detailPath, []byte(updated), expected, expectedExists); err != nil {
		return Task{}, fmt.Errorf("write task detail %q: %w", task.DetailPath, err)
	}
	if err := s.writeIndex(tasks, prefix, suffix, eventTime, snapshot); err != nil {
		return Task{}, fmt.Errorf("task detail %q updated but index update %q failed: %w", task.DetailPath, s.indexPath, err)
	}
	if movingDetail {
		oldPath, err := s.resolve(oldDetailPath)
		if err != nil {
			return Task{}, fmt.Errorf("task index updated to %q but old detail path %q could not be resolved: %w", task.DetailPath, oldDetailPath, err)
		}
		if err := os.Remove(oldPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return Task{}, fmt.Errorf("task index updated to %q but old detail %q remains: %w", task.DetailPath, oldDetailPath, err)
		}
	}
	if err := s.appendEvent(eventTime, update.RequestID, "task", "progress", event, []string{task.ID}); err != nil {
		return Task{}, fmt.Errorf("task updated but daily log %q update failed: %w", s.logRelativePath(eventTime), err)
	}
	if update.Description != nil {
		task.Description = cleanText(*update.Description)
	}
	return *task, nil
}

// RecordWork appends a dated work record; request IDs make retries idempotent.
func (s *Service) RecordWork(entry WorkEntry) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validID.MatchString(entry.RequestID) {
		return "", fmt.Errorf("requestId must be a UUID")
	}
	if entry.Date == "" {
		entry.Date = time.Now().In(s.location).Format("2006-01-02")
	}
	date, err := time.ParseInLocation("2006-01-02", entry.Date, s.location)
	if err != nil || date.Format("2006-01-02") != entry.Date {
		return "", fmt.Errorf("date must use YYYY-MM-DD")
	}
	entry.Text = cleanText(entry.Text)
	if entry.Text == "" {
		return "", fmt.Errorf("text cannot be empty")
	}
	if entry.Category != "" && !contains([]string{"achievement", "progress", "blocker", "next_step"}, entry.Category) {
		return "", fmt.Errorf("category must be achievement, progress, blocker, or next_step")
	}
	for _, id := range entry.TaskIDs {
		if !strings.HasPrefix(id, "task-") || !validID.MatchString(strings.TrimPrefix(id, "task-")) {
			return "", fmt.Errorf("invalid task ID %q", id)
		}
	}
	tasks, _, _, snapshot, indexErr := s.readIndex()
	if errors.Is(indexErr, fs.ErrNotExist) {
		if len(entry.TaskIDs) > 0 {
			return "", fmt.Errorf("cannot link tasks before the worklog task index is initialized")
		}
		if err := s.writeIndex([]Task{}, "# Worklog Task Index\n\nThis index tracks the current status of all work tasks. See each linked task note for details.\n\n", "", time.Now().In(s.location), snapshot); err != nil {
			return "", fmt.Errorf("initialize task index: %w", err)
		}
		tasks = []Task{}
	} else if indexErr != nil {
		return "", indexErr
	}
	for _, id := range entry.TaskIDs {
		if taskIndex(tasks, id) < 0 {
			return "", fmt.Errorf("task %q not found", id)
		}
	}
	if err := s.appendEvent(date, entry.RequestID, cleanText(entry.Category), "recorded", entry.Text, entry.TaskIDs); err != nil {
		return "", err
	}
	return s.logRelativePath(date), nil
}

// GetSummary returns only facts and current active tasks for the requested period.
func (s *Service) GetSummary(options SummaryOptions) (Summary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getSummary(options)
}

func (s *Service) getSummary(options SummaryOptions) (Summary, error) {
	from, to, label, err := s.period(options)
	if err != nil {
		return Summary{}, err
	}
	result := Summary{
		Period: label, From: from.Format("2006-01-02"), To: to.Format("2006-01-02"),
		Timezone: s.location.String(), Facts: []Fact{}, SourcePaths: []string{},
	}
	digest := sha256.New()
	base, err := s.resolve(s.logDir)
	if err != nil {
		return Summary{}, err
	}
	err = filepath.WalkDir(base, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, fs.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || filepath.Ext(path) != ".md" {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(s.root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		for _, line := range strings.Split(string(content), "\n") {
			fact, ok := parseLogLine(line, rel)
			if !ok {
				continue
			}
			date, err := time.ParseInLocation("2006-01-02", fact.Date, s.location)
			if err != nil || date.Before(from) || date.After(to) {
				continue
			}
			if truncated := truncateText(fact.Text, 500); truncated != fact.Text {
				fact.Text = truncated
				result.Truncated = true
			}
			result.Facts = append(result.Facts, fact)
			_, _ = digest.Write([]byte(rel + "\n" + line + "\n"))
			result.SourcePaths = appendUnique(result.SourcePaths, rel)
		}
		return nil
	})
	if err != nil {
		return Summary{}, fmt.Errorf("read worklog records: %w", err)
	}
	sort.SliceStable(result.Facts, func(i, j int) bool {
		if result.Facts[i].Date != result.Facts[j].Date {
			return result.Facts[i].Date < result.Facts[j].Date
		}
		return result.Facts[i].Text < result.Facts[j].Text
	})
	indexPath, err := s.resolve(s.indexPath)
	if err != nil {
		return Summary{}, err
	}
	indexData, indexErr := os.ReadFile(indexPath)
	if indexErr == nil {
		rel := filepath.ToSlash(s.indexPath)
		_, _ = digest.Write([]byte(rel + "\n"))
		_, _ = digest.Write(indexData)
		result.SourcePaths = appendUnique(result.SourcePaths, rel)
		result.SourceDigest = hex.EncodeToString(digest.Sum(nil))
	} else if !errors.Is(indexErr, fs.ErrNotExist) {
		return Summary{}, fmt.Errorf("read task index for summary: %w", indexErr)
	}
	tasks, _, _, _, err := s.readIndex()
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Summary{}, err
	}
	for _, task := range tasks {
		if task.Status == "in_progress" || task.Status == "blocked" || task.Status == "todo" {
			result.ActiveTasks = append(result.ActiveTasks, task)
		}
	}
	sort.Strings(result.SourcePaths)
	if len(result.SourcePaths) > 500 {
		result.SourcePaths = result.SourcePaths[:500]
		result.Truncated = true
	}
	if len(result.Facts) > 500 {
		result.Facts = result.Facts[:500]
		result.Truncated = true
	}
	if len(result.ActiveTasks) > 50 {
		result.ActiveTasks = result.ActiveTasks[:50]
		result.Truncated = true
	}
	return result, nil
}

// SaveSummary stores a generated summary only if its source digest is still current.
func (s *Service) SaveSummary(summary Summary, content, alternateName string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	period := map[string]string{
		"day": "day", "week": "week", "month": "month", "year": "year",
		"all": "all", "custom": "custom",
	}[summary.Period]
	if period == "" {
		return "", fmt.Errorf("invalid summary period %q", summary.Period)
	}
	options := SummaryOptions{Period: period, From: summary.From, To: summary.To}
	if period == "all" {
		options.From, options.To = "", ""
	}
	current, err := s.getSummary(options)
	if err != nil {
		return "", err
	}
	if current.From != summary.From || current.To != summary.To {
		return "", fmt.Errorf("summary date range no longer matches the selected period")
	}
	if current.SourceDigest != summary.SourceDigest {
		return "", fmt.Errorf("worklog changed after summary generation; fetch a fresh summary before saving")
	}
	rel, err := s.summaryRelativePath(summary, alternateName)
	if err != nil {
		return "", err
	}
	path, err := s.resolve(rel)
	if err != nil {
		return "", err
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return "", fmt.Errorf("summary content cannot be empty")
	}
	frontmatter := map[string]any{
		"type": "work-summary", "period": summary.Period, "from": summary.From,
		"to": summary.To, "generatedAt": time.Now().In(s.location).Format(time.RFC3339),
		"sourcePaths": current.SourcePaths, "sourceDigest": summary.SourceDigest, "facts": current.Facts,
	}
	encoded, err := yaml.Marshal(frontmatter)
	if err != nil {
		return "", fmt.Errorf("encode summary metadata: %w", err)
	}
	note := "---\n" + string(encoded) + "---\n\n" + content + "\n"
	if _, statErr := os.Lstat(path); statErr == nil {
		return "", fmt.Errorf("summary note %q already exists; choose an alternateName", rel)
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		return "", statErr
	}
	if err := s.writeAtomicChecked(path, []byte(note), nil, false); err != nil {
		return "", err
	}
	return rel, nil
}

func (s *Service) period(options SummaryOptions) (time.Time, time.Time, string, error) {
	now := time.Now().In(s.location)
	if options.From != "" || options.To != "" {
		if options.From == "" || options.To == "" {
			return time.Time{}, time.Time{}, "", fmt.Errorf("both from and to are required for an explicit date range")
		}
		from, err := parseDateDefault(options.From, now)
		if err != nil {
			return time.Time{}, time.Time{}, "", fmt.Errorf("invalid from date: %w", err)
		}
		to, err := parseDateDefault(options.To, now)
		if err != nil {
			return time.Time{}, time.Time{}, "", fmt.Errorf("invalid to date: %w", err)
		}
		if to.Before(from) {
			return time.Time{}, time.Time{}, "", fmt.Errorf("to must be on or after from")
		}
		label := map[string]string{
			"day": "day", "week": "week", "month": "month", "year": "year",
			"all": "all", "custom": "custom",
		}[options.Period]
		if label == "" {
			label = "custom"
		}
		switch options.Period {
		case "day":
			if !from.Equal(to) {
				return time.Time{}, time.Time{}, "", fmt.Errorf("day summaries require identical from and to dates")
			}
		case "week":
			if from.Weekday() != time.Sunday || !to.Equal(from.AddDate(0, 0, 6)) {
				return time.Time{}, time.Time{}, "", fmt.Errorf("week range must run Sunday through Saturday")
			}
		case "month":
			if from.Day() != 1 || !to.Equal(time.Date(from.Year(), from.Month()+1, 0, 0, 0, 0, 0, s.location)) {
				return time.Time{}, time.Time{}, "", fmt.Errorf("month range must cover a full calendar month")
			}
		case "year":
			if from.Month() != time.January || from.Day() != 1 || to.Year() != from.Year() || to.Month() != time.December || to.Day() != 31 {
				return time.Time{}, time.Time{}, "", fmt.Errorf("year range must cover a full calendar year")
			}
		}
		return from, to, label, nil
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, s.location)
	switch options.Period {
	case "day":
		return today, today, "day", nil
	case "week":
		start := today.AddDate(0, 0, -int(today.Weekday()))
		return start, start.AddDate(0, 0, 6), "week", nil
	case "month":
		start := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, s.location)
		return start, start.AddDate(0, 1, -1), "month", nil
	case "year":
		start := time.Date(today.Year(), 1, 1, 0, 0, 0, 0, s.location)
		return start, time.Date(today.Year(), 12, 31, 0, 0, 0, 0, s.location), "year", nil
	case "all":
		start, err := s.indexStarted()
		if err != nil {
			return time.Time{}, time.Time{}, "", err
		}
		return start, today, "all", nil
	default:
		return time.Time{}, time.Time{}, "", fmt.Errorf("period must be day, week, month, year, all, or provide from/to")
	}
}

func (s *Service) readIndex() ([]Task, string, string, indexSnapshot, error) {
	path, err := s.resolve(s.indexPath)
	if err != nil {
		return nil, "", "", indexSnapshot{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", "", indexSnapshot{}, err
	}
	var metadata map[string]any
	body, err := parseFrontmatter(string(data), &metadata)
	if err != nil {
		return nil, "", "", indexSnapshot{}, fmt.Errorf("invalid task index frontmatter: %w", err)
	}
	if metadata["type"] != "work-task-index" || metadata["schemaVersion"] != 1 {
		return nil, "", "", indexSnapshot{}, fmt.Errorf("task index has unexpected type")
	}
	frontmatterEnd := strings.Index(string(data)[4:], "\n---")
	if frontmatterEnd < 0 {
		return nil, "", "", indexSnapshot{}, fmt.Errorf("task index frontmatter closing delimiter is missing")
	}
	header := string(data[:4+frontmatterEnd+len("\n---")]) + "\n\n"
	lines := strings.Split(body, "\n")
	headerAt := -1
	for i, line := range lines {
		cells := parseTableRow(line)
		if len(cells) >= len(indexHeader) && equalCells(cells[:len(indexHeader)], indexHeader) && trailingCellsEmpty(cells[len(indexHeader):]) {
			headerAt = i
			break
		}
	}
	if headerAt < 0 || headerAt+1 >= len(lines) || !isSeparator(lines[headerAt+1]) {
		return nil, "", "", indexSnapshot{}, fmt.Errorf("task index table header is missing or invalid")
	}
	var tasks []Task
	seen := map[string]bool{}
	end := headerAt + 2
	for end < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[end]), "|") {
		cells := trimTrailingEmptyCells(parseTableRow(lines[end]))
		if len(cells) != len(indexHeader) {
			return nil, "", "", indexSnapshot{}, fmt.Errorf("invalid task index row %d: expected %d cells, got %d", end+1, len(indexHeader), len(cells))
		}
		row := end + 1
		task := Task{
			ID: cells[0], Title: cells[1], Status: cells[2], Project: cells[3], Priority: cells[4],
			Created: cells[5], Updated: cells[6], Due: cells[7], Completed: cells[8],
		}
		if task.ID == "" || seen[task.ID] {
			return nil, "", "", indexSnapshot{}, fmt.Errorf("task index row %d has empty or duplicate ID", row)
		}
		if !strings.HasPrefix(task.ID, "task-") || !validID.MatchString(strings.TrimPrefix(task.ID, "task-")) {
			return nil, "", "", indexSnapshot{}, fmt.Errorf("task index row %d has invalid task ID", row)
		}
		if !validStatuses[task.Status] {
			return nil, "", "", indexSnapshot{}, fmt.Errorf("task index row %d has invalid status %q", row, task.Status)
		}
		detailPath, ok := parseDetailLink(cells[9], task.ID)
		if !ok {
			return nil, "", "", indexSnapshot{}, fmt.Errorf("task index row %d has invalid detail link %q", row, cells[9])
		}
		if err := s.validateTaskDetailPath(detailPath); err != nil {
			return nil, "", "", indexSnapshot{}, fmt.Errorf("task index row %d has invalid detail path %q: %w", row, detailPath, err)
		}
		task.DetailPath = filepath.ToSlash(filepath.Clean(filepath.FromSlash(detailPath)))
		seen[task.ID] = true
		tasks = append(tasks, task)
		end++
	}
	prefix := header + strings.Join(lines[:headerAt], "\n")
	suffix := strings.Join(lines[end:], "\n")
	return tasks, prefix, suffix, indexSnapshot{content: string(data), exists: true}, nil
}

func (s *Service) indexStarted() (time.Time, error) {
	path, err := s.resolve(s.indexPath)
	if err != nil {
		return time.Time{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}, fmt.Errorf("all-time summary requires an initialized task index: %w", err)
	}
	var metadata map[string]any
	if _, err := parseFrontmatter(string(data), &metadata); err != nil {
		return time.Time{}, fmt.Errorf("invalid task index frontmatter: %w", err)
	}
	var value string
	switch started := metadata["started"].(type) {
	case time.Time:
		value = started.Format("2006-01-02")
	case string:
		value = started
	default:
		return time.Time{}, fmt.Errorf("task index started date is missing or invalid")
	}
	date, err := time.ParseInLocation("2006-01-02", value, s.location)
	if err != nil || date.Format("2006-01-02") != value {
		return time.Time{}, fmt.Errorf("task index started date is missing or invalid")
	}
	return date, nil
}

func (s *Service) writeIndex(tasks []Task, prefix, suffix string, now time.Time, snapshot indexSnapshot) error {
	path, err := s.resolve(s.indexPath)
	if err != nil {
		return err
	}
	if prefix == "" {
		prefix = "# Worklog Task Index\n\nThis index tracks the current status of all work tasks. See each linked task note for details.\n\n"
	}
	if !strings.HasPrefix(strings.TrimSpace(prefix), "---") {
		prefix = "---\ntype: work-task-index\nschemaVersion: 1\nstarted: " + now.Format("2006-01-02") + "\n---\n\n" + prefix
	}
	output := strings.TrimRight(prefix, "\n") + "\n\n" + renderTaskTable(tasks)
	if suffix != "" {
		output += "\n" + suffix
	}
	if err := s.writeAtomicChecked(path, []byte(output+"\n"), []byte(snapshot.content), snapshot.exists); err != nil {
		return err
	}
	started, err := s.indexStarted()
	if err != nil {
		return fmt.Errorf("task index updated but its started date could not be read: %w", err)
	}
	for _, view := range []struct {
		name     string
		filename string
		statuses []string
	}{
		{name: "ongoing", filename: "Task Ongoing.md", statuses: []string{"todo", "in_progress", "blocked"}},
		{name: "done", filename: "Task Done.md", statuses: []string{"done", "cancelled"}},
	} {
		viewPath := filepath.Join(filepath.Dir(s.indexPath), view.filename)
		if err := s.writeTaskView(viewPath, view.name, view.statuses, tasks, started); err != nil {
			return fmt.Errorf("task index %q updated but view %q failed: %w", s.indexPath, filepath.ToSlash(viewPath), err)
		}
	}
	return nil
}

func (s *Service) writeTaskView(relative, view string, statuses []string, tasks []Task, started time.Time) error {
	path, err := s.resolve(relative)
	if err != nil {
		return err
	}
	var snapshot indexSnapshot
	if data, readErr := os.ReadFile(path); readErr == nil {
		var metadata map[string]any
		body, err := parseFrontmatter(string(data), &metadata)
		title := "# Task Ongoing"
		if view == "done" {
			title = "# Task Done"
		}
		if err != nil || metadata["type"] != "work-task-index" ||
			metadata["schemaVersion"] != 1 || !strings.Contains(body, title+"\n") ||
			!strings.Contains(body, "Generated from [["+filepath.Base(s.indexPath)+"]].") {
			return fmt.Errorf("refusing to overwrite non-generated task view %q", relative)
		}
		snapshot = indexSnapshot{content: string(data), exists: true}
	} else if !errors.Is(readErr, fs.ErrNotExist) {
		return readErr
	}
	filtered := make([]Task, 0, len(tasks))
	for _, task := range tasks {
		if contains(statuses, task.Status) {
			filtered = append(filtered, task)
		}
	}
	title := "Task Ongoing"
	if view == "done" {
		title = "Task Done"
	}
	content := fmt.Sprintf("---\ntype: work-task-index\nschemaVersion: 1\nstarted: %s\n---\n\n# %s\n\nGenerated from [[%s]].\n\n%s\n",
		started.Format("2006-01-02"), title, filepath.Base(s.indexPath), renderTaskTable(filtered))
	return s.writeAtomicChecked(path, []byte(content), []byte(snapshot.content), snapshot.exists)
}

func renderTaskTable(tasks []Task) string {
	lines := []string{"| " + strings.Join(indexHeader, " | ") + " |", "| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |"}
	for _, task := range tasks {
		cells := []string{
			task.ID, task.Title, task.Status, task.Project, task.Priority,
			task.Created, task.Updated, task.Due, task.Completed,
			"[[" + task.DetailPath + "\\|" + task.ID + "]]",
		}
		for i := 0; i < len(cells)-1; i++ {
			cells[i] = escapeCell(cells[i])
		}
		lines = append(lines, "| "+strings.Join(cells, " | ")+" |")
	}
	return strings.Join(lines, "\n")
}

func (s *Service) readTaskDetail(task Task) (Task, error) {
	content, err := s.readRaw(task.DetailPath)
	if err != nil {
		return Task{}, err
	}
	var metadata map[string]any
	body, err := parseFrontmatter(content, &metadata)
	if err != nil || metadata["type"] != "work-task" || metadata["id"] != task.ID {
		return Task{}, fmt.Errorf("task detail %q has invalid frontmatter", task.DetailPath)
	}
	description := section(body, "Description")
	history := section(body, "Progress History")
	lines := strings.Split(strings.TrimSpace(history), "\n")
	latest := ""
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			latest = strings.TrimSpace(lines[i])
			break
		}
	}
	task.Description = truncateText(strings.TrimSpace(description), 500)
	task.LatestUpdate = truncateText(latest, 300)
	return task, nil
}

func (s *Service) readRaw(relative string) (string, error) {
	path, err := s.resolve(relative)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	return string(data), err
}

func (s *Service) appendEvent(date time.Time, requestID, category, event, text string, taskIDs []string) error {
	relative := s.logRelativePath(date)
	path, err := s.resolve(relative)
	if err != nil {
		return err
	}
	content, err := os.ReadFile(path)
	existed := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	expected := append([]byte(nil), content...)
	ids := strings.Join(taskIDs, ",")
	recordedAt := date.In(s.location)
	now := time.Now().In(s.location)
	if date.Format("2006-01-02") == now.Format("2006-01-02") {
		recordedAt = now
	}
	displayRefs := make([]string, 0, len(taskIDs))
	if len(taskIDs) > 0 {
		tasks, _, _, _, err := s.readIndex()
		if err != nil {
			return err
		}
		for _, id := range taskIDs {
			if idx := taskIndex(tasks, id); idx >= 0 {
				displayRefs = append(displayRefs, "[["+tasks[idx].DetailPath+"|"+id+"]]")
			}
		}
	}
	displayCategory := sanitize(category)
	if displayCategory == "" {
		displayCategory = "work"
	}
	display := fmt.Sprintf("**%s** · **%s / %s**", recordedAt.Format("15:04"), displayCategory, sanitize(event))
	if len(displayRefs) > 0 {
		display += " · " + strings.Join(displayRefs, ", ")
	}
	display += " — " + linkifyURLs(sanitize(text))
	line := fmt.Sprintf("- %s %%%%worklog:%s|%s|%s|%s|%s%%%%\n",
		display, recordedAt.Format(time.RFC3339), sanitize(category), sanitize(event), ids, strings.ToLower(requestID))
	legacyMarker := "[event:" + strings.ToLower(requestID) + "]"
	readableMarker := "|" + strings.ToLower(requestID) + "%%"
	htmlReadableMarker := "|" + strings.ToLower(requestID) + "-->"
	for _, existing := range strings.Split(string(content), "\n") {
		if !strings.Contains(existing, legacyMarker) && !strings.Contains(existing, readableMarker) && !strings.Contains(existing, htmlReadableMarker) {
			continue
		}
		fact, ok := parseLogLine(existing, relative)
		if ok && fact.Category == sanitize(category)+"/"+sanitize(event) &&
			fact.Text == sanitize(text) && strings.Join(fact.TaskIDs, ",") == ids {
			return nil
		}
		return fmt.Errorf("event ID %q already exists with different content in %q", requestID, relative)
	}
	if len(content) == 0 {
		content = []byte("---\ntype: work-daily-log\ndate: " + date.Format("2006-01-02") + "\n---\n\n# Work Log - " + date.Format("2006-01-02") + "\n\n")
	}
	if len(content) > 0 && content[len(content)-1] != '\n' {
		content = append(content, '\n')
	}
	content = append(content, []byte(line)...)
	return s.writeAtomicChecked(path, content, expected, existed)
}

func (s *Service) writeAtomicChecked(path string, content, expected []byte, expectedExists bool) error {
	if err := s.ensureWithinRoot(path); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := s.ensureWithinRoot(path); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to write through symlink %q", path)
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".worklog-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	current, readErr := os.ReadFile(path)
	if expectedExists {
		if readErr != nil {
			return fmt.Errorf("write conflict for %q: expected existing content, got %w", path, readErr)
		}
		if !bytes.Equal(current, expected) {
			return fmt.Errorf("write conflict for %q: file changed during update", path)
		}
	} else if readErr == nil {
		return fmt.Errorf("write conflict for %q: file was created during update", path)
	} else if !errors.Is(readErr, fs.ErrNotExist) {
		return readErr
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace %q: %w", path, err)
	}
	return nil
}

func (s *Service) ensureWithinRoot(path string) error {
	root, err := filepath.EvalSymlinks(s.root)
	if err != nil {
		return err
	}
	parentPath := filepath.Dir(path)
	for {
		parent, evalErr := filepath.EvalSymlinks(parentPath)
		if evalErr == nil {
			rel, relErr := filepath.Rel(root, parent)
			if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return fmt.Errorf("resolved path escapes vault root")
			}
			break
		}
		if !errors.Is(evalErr, fs.ErrNotExist) {
			return evalErr
		}
		next := filepath.Dir(parentPath)
		if next == parentPath {
			return evalErr
		}
		parentPath = next
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to access symlink %q", path)
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func (s *Service) resolve(relative string) (string, error) {
	if err := validateRelativePath(relative); err != nil {
		return "", err
	}
	path := filepath.Join(s.root, filepath.Clean(relative))
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(s.root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes vault root")
	}
	if err := s.ensureWithinRoot(abs); err != nil {
		return "", err
	}
	return abs, nil
}

func (s *Service) taskRelativePath(id, project string) string {
	return filepath.ToSlash(filepath.Join(s.taskDir, project, id+".md"))
}

func validateProjectName(value string) (string, error) {
	project := cleanText(value)
	if project == "" {
		return "", fmt.Errorf("project cannot be empty")
	}
	if project == "." || project == ".." || strings.ContainsAny(project, `/\<>:"|?*`) ||
		strings.HasSuffix(project, ".") || strings.HasSuffix(project, " ") {
		return "", fmt.Errorf("project must be a valid single directory name")
	}
	for _, r := range project {
		if r < 32 {
			return "", fmt.Errorf("project must not contain control characters")
		}
	}
	reserved := strings.ToUpper(strings.TrimSuffix(strings.SplitN(project, ".", 2)[0], " "))
	switch reserved {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return "", fmt.Errorf("project name %q is reserved for a Windows device", project)
	}
	return project, nil
}

func (s *Service) validateTaskDetailPath(path string) error {
	if err := validateRelativePath(path); err != nil {
		return err
	}
	cleanPath := filepath.Clean(filepath.FromSlash(path))
	rel, err := filepath.Rel(s.taskDir, cleanPath)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("detail path must be under %q", filepath.ToSlash(s.taskDir))
	}
	return nil
}

// IndexRelativePath returns the configured task index path.
func (s *Service) IndexRelativePath() string {
	return filepath.ToSlash(s.indexPath)
}

// TaskViewPaths returns the generated ongoing and done task-view paths.
func (s *Service) TaskViewPaths() []string {
	return []string{
		filepath.ToSlash(filepath.Join(filepath.Dir(s.indexPath), "Task Ongoing.md")),
		filepath.ToSlash(filepath.Join(filepath.Dir(s.indexPath), "Task Done.md")),
	}
}

// DailyLogPath returns the configured log path for an ISO date.
func (s *Service) DailyLogPath(value string) (string, error) {
	date, err := time.ParseInLocation("2006-01-02", value, s.location)
	if err != nil || date.Format("2006-01-02") != value {
		return "", fmt.Errorf("date must use YYYY-MM-DD")
	}
	return s.logRelativePath(date), nil
}

func (s *Service) logRelativePath(date time.Time) string {
	return filepath.ToSlash(filepath.Join(s.logDir, date.Format("2006"), date.Format("01"), date.Format("2006-01-02")+".md"))
}

func (s *Service) summaryRelativePath(summary Summary, alternateName string) (string, error) {
	var folder, filename string
	from, _ := time.ParseInLocation("2006-01-02", summary.From, s.location)
	switch summary.Period {
	case "day":
		folder, filename = "Daily", summary.From+".md"
	case "week":
		folder, filename = "Weekly", summary.From+".md"
	case "month":
		folder, filename = "Monthly", from.Format("2006-01")+".md"
	case "year":
		folder, filename = "Yearly", from.Format("2006")+".md"
	case "all":
		folder, filename = "All Time", "All Time.md"
	default:
		folder, filename = "Custom", summary.From+"_to_"+summary.To+".md"
	}
	if alternateName != "" {
		alternateName = strings.TrimSpace(alternateName)
		if alternateName == "." || alternateName == ".." || strings.ContainsAny(alternateName, `/\:`) {
			return "", fmt.Errorf("alternateName must be a filename without directory separators")
		}
		if filepath.Ext(alternateName) == "" {
			alternateName += ".md"
		}
		if filepath.Ext(alternateName) != ".md" {
			return "", fmt.Errorf("alternateName must end in .md")
		}
		filename = alternateName
	}
	return filepath.ToSlash(filepath.Join(s.summaryDir, folder, filename)), nil
}

func validateRelativePath(path string) error {
	if strings.TrimSpace(path) == "" || filepath.IsAbs(path) {
		return fmt.Errorf("must be a non-empty relative path")
	}
	for _, segment := range strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' }) {
		if segment == ".." {
			return fmt.Errorf("path traversal is not allowed")
		}
	}
	clean := filepath.Clean(path)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path traversal is not allowed")
	}
	return nil
}

func cleanText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func truncateText(value string, maxRunes int) string {
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes]) + "..."
}

func sanitize(value string) string {
	return strings.NewReplacer("\r", " ", "\n", " ", "[", "(", "]", ")").Replace(cleanText(value))
}

func linkifyURLs(value string) string {
	matches := bareURL.FindAllStringIndex(value, -1)
	if len(matches) == 0 {
		return value
	}
	existingLinks := autoLinkedURL.FindAllStringIndex(value, -1)
	var result strings.Builder
	last := 0
	for _, match := range matches {
		start, end := match[0], match[1]
		insideExistingLink := false
		for _, link := range existingLinks {
			if start >= link[0] && start < link[1] {
				insideExistingLink = true
				break
			}
		}
		if insideExistingLink {
			continue
		}
		if start > 0 && (value[start-1] == '<' || value[start-1] == '(' && start > 1 && value[start-2] == ']') {
			continue
		}
		urlEnd := end
		for urlEnd > start && strings.ContainsRune(".,;:!?", rune(value[urlEnd-1])) {
			urlEnd--
		}
		for urlEnd > start && value[urlEnd-1] == ')' {
			urlText := value[start:urlEnd]
			if strings.Count(urlText, ")") <= strings.Count(urlText, "(") {
				break
			}
			urlEnd--
		}
		parsed, err := url.Parse(value[start:urlEnd])
		if urlEnd == start || err != nil || parsed.Host == "" {
			continue
		}
		result.WriteString(value[last:start])
		urlText := value[start:urlEnd]
		result.WriteByte('[')
		result.WriteString(urlText)
		result.WriteString("](<")
		result.WriteString(urlText)
		result.WriteString(">)")
		suffixEnd := urlEnd
		for suffixEnd < len(value) {
			r, size := utf8.DecodeRuneInString(value[suffixEnd:])
			if !strings.ContainsRune(".,;:!?)]}，。；：！？、）】」』》", r) {
				break
			}
			suffixEnd += size
		}
		result.WriteString(value[urlEnd:suffixEnd])
		if suffixEnd < len(value) {
			r, _ := utf8.DecodeRuneInString(value[suffixEnd:])
			if !unicode.IsSpace(r) {
				result.WriteByte(' ')
			}
		}
		last = suffixEnd
	}
	if last == 0 {
		return value
	}
	result.WriteString(value[last:])
	return result.String()
}

func plainAutoLinkedURLs(value string) string {
	return autoLinkedURL.ReplaceAllString(value, "$1")
}

func equalCells(cells, expected []string) bool {
	if len(cells) != len(expected) {
		return false
	}
	for i := range cells {
		if cells[i] != expected[i] {
			return false
		}
	}
	return true
}

func parseDetailLink(value, taskID string) (string, bool) {
	if !strings.HasPrefix(value, "[[") || !strings.HasSuffix(value, "]]") {
		return "", false
	}
	path := strings.TrimSuffix(strings.TrimPrefix(value, "[["), "]]")
	if aliasAt := strings.LastIndexByte(path, '|'); aliasAt >= 0 {
		if path[aliasAt+1:] != taskID {
			return "", false
		}
		path = path[:aliasAt]
	}
	return path, filepath.Ext(path) == ".md"
}

func escapeCell(value string) string {
	return strings.ReplaceAll(cleanText(value), "|", "&#124;")
}

func parseTableRow(line string) []string {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "|") {
		return nil
	}
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	var cells []string
	var cell strings.Builder
	inWikiLink := false
	for i := 0; i < len(line); i++ {
		if line[i] == '\\' && i+1 < len(line) && line[i+1] == '|' {
			cell.WriteByte('|')
			i++
			continue
		}
		if i+1 < len(line) && line[i:i+2] == "[[" {
			inWikiLink = true
			cell.WriteString("[[")
			i++
			continue
		}
		if inWikiLink && i+1 < len(line) && line[i:i+2] == "]]" {
			inWikiLink = false
			cell.WriteString("]]")
			i++
			continue
		}
		if line[i] == '|' && !inWikiLink {
			cells = append(cells, cell.String())
			cell.Reset()
			continue
		}
		cell.WriteByte(line[i])
	}
	cells = append(cells, cell.String())
	for i := range cells {
		cells[i] = strings.TrimSpace(strings.ReplaceAll(cells[i], "&#124;", "|"))
	}
	return cells
}

func isSeparator(line string) bool {
	cells := parseTableRow(line)
	if len(cells) < len(indexHeader) {
		return false
	}
	for _, cell := range cells {
		if strings.Trim(cell, "-: ") != "" {
			return false
		}
	}
	return true
}

func trimTrailingEmptyCells(cells []string) []string {
	for len(cells) > len(indexHeader) && strings.TrimSpace(cells[len(cells)-1]) == "" {
		cells = cells[:len(cells)-1]
	}
	return cells
}

func trailingCellsEmpty(cells []string) bool {
	for _, cell := range cells {
		if strings.TrimSpace(cell) != "" {
			return false
		}
	}
	return true
}

func parseFrontmatter(note string, metadata *map[string]any) (string, error) {
	if !strings.HasPrefix(note, "---\n") {
		return "", fmt.Errorf("frontmatter is missing")
	}
	end := strings.Index(note[4:], "\n---")
	if end < 0 {
		return "", fmt.Errorf("frontmatter closing delimiter is missing")
	}
	yamlText := note[4 : 4+end]
	if err := yaml.Unmarshal([]byte(yamlText), metadata); err != nil {
		return "", err
	}
	bodyStart := 4 + end + len("\n---")
	body := strings.TrimPrefix(note[bodyStart:], "\n")
	return body, nil
}

func taskH1(body string) (string, bool) {
	inFence := false
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence || !strings.HasPrefix(trimmed, "# ") {
			continue
		}
		title := strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
		hashes := len(title)
		for hashes > 0 && title[hashes-1] == '#' {
			hashes--
		}
		if hashes < len(title) && (hashes == 0 || title[hashes-1] == ' ' || title[hashes-1] == '\t') {
			title = strings.TrimSpace(title[:hashes])
		}
		title = cleanText(title)
		return title, title != ""
	}
	return "", false
}

func taskDetail(task Task, description, timestamp, requestID, event, text string) string {
	metadata, _ := yaml.Marshal(map[string]any{"type": "work-task", "id": task.ID, "created": timestamp})
	var b strings.Builder
	b.WriteString("---\n")
	b.Write(metadata)
	b.WriteString("---\n\n# ")
	b.WriteString(task.Title)
	b.WriteString("\n\n## Description\n")
	b.WriteString(linkifyURLs(strings.TrimSpace(description)))
	b.WriteString("\n\n## Progress History\n- ")
	b.WriteString(timestamp)
	b.WriteString(" [event:")
	b.WriteString(strings.ToLower(requestID))
	b.WriteString("] [")
	b.WriteString(event)
	b.WriteString("] ")
	b.WriteString(linkifyURLs(sanitize(text)))
	b.WriteString("\n")
	return b.String()
}

func validateTaskDetail(note string, task Task) error {
	var metadata map[string]any
	if _, err := parseFrontmatter(note, &metadata); err != nil {
		return fmt.Errorf("invalid task detail %q: %w", task.DetailPath, err)
	}
	if metadata["type"] != "work-task" || metadata["id"] != task.ID {
		return fmt.Errorf("task detail %q has mismatched type or ID", task.DetailPath)
	}
	return nil
}

func appendTaskEvent(note, requestID, timestamp, status, text string, description, title *string) (string, error) {
	var metadata map[string]any
	body, err := parseFrontmatter(note, &metadata)
	if err != nil {
		return "", err
	}
	if metadata["type"] != "work-task" {
		return "", fmt.Errorf("task detail has unexpected type")
	}
	eventID := "[event:" + strings.ToLower(requestID) + "]"
	expectedEvent := eventID + " [" + status + "] " + linkifyURLs(sanitize(text))
	for _, line := range strings.Split(body, "\n") {
		if !strings.Contains(line, eventID) {
			continue
		}
		if strings.Contains(line, expectedEvent) {
			return note, nil
		}
		return "", fmt.Errorf("event ID %q already exists with different task history", requestID)
	}
	frontmatterEnd := strings.Index(note[4:], "\n---")
	if frontmatterEnd < 0 {
		return "", fmt.Errorf("task detail frontmatter is invalid")
	}
	bodyStart := 4 + frontmatterEnd + len("\n---")
	prefix := note[:bodyStart] + "\n"
	if description != nil {
		start := strings.Index(body, "## Description")
		end := strings.Index(body, "## Progress History")
		if start < 0 || end < start {
			return "", fmt.Errorf("task detail sections are invalid")
		}
		body = body[:start+len("## Description\n")] + linkifyURLs(strings.TrimSpace(*description)) + "\n\n" + body[end:]
	}
	if title != nil {
		titleAt := strings.Index(body, "# ")
		if titleAt >= 0 {
			titleEnd := strings.Index(body[titleAt:], "\n")
			if titleEnd >= 0 {
				body = body[:titleAt+2] + cleanText(*title) + body[titleAt+titleEnd:]
			}
		}
	}
	line := "- " + timestamp + " " + eventID + " [" + status + "] " + linkifyURLs(sanitize(text)) + "\n"
	historyAt := strings.Index(body, "## Progress History")
	if historyAt < 0 {
		return "", fmt.Errorf("task detail progress history is missing")
	}
	historyEnd := historyAt + len("## Progress History")
	return prefix + body[:historyEnd] + "\n" + strings.TrimSpace(body[historyEnd:]) + "\n" + line, nil
}

func taskEventTime(note, requestID string, location *time.Location) (time.Time, bool) {
	marker := "[event:" + strings.ToLower(requestID) + "]"
	for _, line := range strings.Split(note, "\n") {
		if !strings.Contains(line, marker) || !strings.HasPrefix(line, "- ") {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(line, "- "))
		if len(fields) == 0 {
			return time.Time{}, false
		}
		value, err := time.Parse(time.RFC3339, fields[0])
		if err != nil {
			return time.Time{}, false
		}
		return value.In(location), true
	}
	return time.Time{}, false
}

func statusLabel(update *string, fallback string) string {
	if update != nil {
		return *update
	}
	return fallback
}

func section(body, title string) string {
	marker := "## " + title
	start := strings.Index(body, marker)
	if start < 0 {
		return ""
	}
	start += len(marker)
	end := strings.Index(body[start:], "\n## ")
	if end < 0 {
		return body[start:]
	}
	return body[start : start+end]
}

func taskIndex(tasks []Task, id string) int {
	for i := range tasks {
		if tasks[i].ID == id {
			return i
		}
	}
	return -1
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func appendUnique(values []string, value string) []string {
	if !contains(values, value) {
		return append(values, value)
	}
	return values
}

func parseDateDefault(value string, fallback time.Time) (time.Time, error) {
	if value == "" {
		return time.Date(fallback.Year(), fallback.Month(), fallback.Day(), 0, 0, 0, 0, fallback.Location()), nil
	}
	date, err := time.ParseInLocation("2006-01-02", value, fallback.Location())
	if err != nil || date.Format("2006-01-02") != value {
		return time.Time{}, fmt.Errorf("must use YYYY-MM-DD")
	}
	return date, nil
}

func parseLogLine(line, source string) (Fact, bool) {
	if match := readableLogLine.FindStringSubmatch(line); match != nil {
		if _, err := time.Parse(time.RFC3339, match[2]); err != nil {
			return Fact{}, false
		}
		date := match[2][:10]
		var taskIDs []string
		if match[5] != "" {
			taskIDs = strings.Split(match[5], ",")
		}
		text := match[1]
		if separator := strings.Index(text, " — "); separator >= 0 {
			text = text[separator+len(" — "):]
		}
		text = plainAutoLinkedURLs(text)
		return Fact{Date: date, Category: match[3] + "/" + match[4], Text: text, TaskIDs: taskIDs, Source: source}, true
	}
	if match := htmlReadableLogLine.FindStringSubmatch(line); match != nil {
		if _, err := time.Parse(time.RFC3339, match[1]); err != nil {
			return Fact{}, false
		}
		var taskIDs []string
		if match[4] != "" {
			taskIDs = strings.Split(match[4], ",")
		}
		text := match[6]
		if separator := strings.Index(text, " — "); separator >= 0 {
			text = text[separator+len(" — "):]
		}
		text = plainAutoLinkedURLs(text)
		return Fact{Date: match[1][:10], Category: match[2] + "/" + match[3], Text: text, TaskIDs: taskIDs, Source: source}, true
	}
	match := logLine.FindStringSubmatch(line)
	if match == nil {
		return Fact{}, false
	}
	if _, err := time.Parse("2006-01-02", match[1]); err != nil {
		return Fact{}, false
	}
	var taskIDs []string
	if match[4] != "" {
		taskIDs = strings.Split(match[4], ",")
	}
	if match[5] == "" {
		return Fact{}, false
	}
	return Fact{Date: match[1], Category: match[2] + "/" + match[3], Text: match[5], TaskIDs: taskIDs, Source: source}, true
}
