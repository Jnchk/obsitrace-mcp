package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/Jnchk/obsitrace-mcp/internal/worklog"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type (
	QueryTasksInput struct {
		Query    string   `json:"query,omitempty" jsonschema:"Match task ID, title, or project"`
		Statuses []string `json:"statuses,omitempty" jsonschema:"Filter by task status"`
		Project  string   `json:"project,omitempty" jsonschema:"Exact project filter"`
		Limit    int      `json:"limit,omitempty" jsonschema:"Maximum results (default 20, max 100)"`
		Offset   int      `json:"offset,omitempty" jsonschema:"Number of matching tasks to skip"`
	}

	QueryTasksOutput struct {
		Tasks   []worklog.Task `json:"tasks"`
		Total   int            `json:"total"`
		HasMore bool           `json:"hasMore"`
	}

	CreateTaskInput struct {
		RequestID        string `json:"requestId" jsonschema:"UUID reused when retrying the same create request"`
		Title            string `json:"title" jsonschema:"Task title"`
		Description      string `json:"description,omitempty" jsonschema:"Task description"`
		Project          string `json:"project" jsonschema:"Project name; required because task details are stored under the project directory"`
		DueDate          string `json:"dueDate,omitempty" jsonschema:"Due date in YYYY-MM-DD format"`
		Priority         string `json:"priority,omitempty" jsonschema:"Priority label"`
		ConfirmDuplicate bool   `json:"confirmDuplicate,omitempty" jsonschema:"Confirm creation despite same-title candidates"`
	}

	CreateTaskOutput struct {
		Task                *worklog.Task  `json:"task,omitempty"`
		DuplicateCandidates []worklog.Task `json:"duplicateCandidates,omitempty"`
		CreatedPaths        []string       `json:"createdPaths,omitempty"`
		Success             bool           `json:"success"`
		PartialFailure      bool           `json:"partialFailure,omitempty"`
	}

	UpdateTaskInput struct {
		RequestID   string  `json:"requestId" jsonschema:"UUID reused when retrying the same update"`
		TaskID      string  `json:"taskId" jsonschema:"Task ID from query_tasks"`
		Title       *string `json:"title,omitempty" jsonschema:"New title"`
		Status      *string `json:"status,omitempty" jsonschema:"One of todo, in_progress, done, blocked, cancelled"`
		Progress    string  `json:"progress,omitempty" jsonschema:"Progress event or reason for the change"`
		Project     *string `json:"project,omitempty" jsonschema:"New project"`
		Priority    *string `json:"priority,omitempty" jsonschema:"New priority"`
		DueDate     *string `json:"dueDate,omitempty" jsonschema:"New due date in YYYY-MM-DD format; empty clears it"`
		Description *string `json:"description,omitempty" jsonschema:"Replace task description"`
	}

	UpdateTaskOutput struct {
		Task         worklog.Task `json:"task"`
		EventID      string       `json:"eventId"`
		UpdatedPaths []string     `json:"updatedPaths"`
		Success      bool         `json:"success"`
	}

	RecordWorkInput struct {
		RequestID string   `json:"requestId" jsonschema:"UUID reused when retrying the same record"`
		Text      string   `json:"text" jsonschema:"Concise work record"`
		Category  string   `json:"category,omitempty" jsonschema:"achievement, progress, blocker, or next_step"`
		Date      string   `json:"date,omitempty" jsonschema:"Optional record date in YYYY-MM-DD format"`
		TaskIDs   []string `json:"taskIds,omitempty" jsonschema:"Existing related task IDs"`
	}

	RecordWorkOutput struct {
		Date    string `json:"date"`
		EventID string `json:"eventId"`
		Path    string `json:"path"`
		Success bool   `json:"success"`
	}

	GetWorkSummaryInput struct {
		Granularity string `json:"granularity" jsonschema:"day, week, month, year, all, or custom"`
		From        string `json:"from,omitempty" jsonschema:"Start date in YYYY-MM-DD format"`
		To          string `json:"to,omitempty" jsonschema:"Inclusive end date in YYYY-MM-DD format"`
	}

	SaveWorkSummaryInput struct {
		Granularity   string `json:"granularity" jsonschema:"day, week, month, year, all, or custom"`
		From          string `json:"from" jsonschema:"Start date from get_work_summary"`
		To            string `json:"to" jsonschema:"End date from get_work_summary"`
		Content       string `json:"content" jsonschema:"Human-readable summary to save"`
		SourceDigest  string `json:"sourceDigest" jsonschema:"Digest returned by get_work_summary"`
		AlternateName string `json:"alternateName,omitempty" jsonschema:"Optional safe filename in the same summary directory"`
	}

	SaveWorkSummaryOutput struct {
		Path    string `json:"path"`
		Success bool   `json:"success"`
	}

	SyncTasksOutput struct {
		Total        int      `json:"total"`
		Updated      int      `json:"updated"`
		UpdatedPaths []string `json:"updatedPaths"`
		Success      bool     `json:"success"`
	}
)

func registerWorklogTools(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "query_tasks", Description: "Query the compact work task index by ID, title, project, or status. Returns only matched task details.",
	}, handleQueryTasks)
	mcp.AddTool(server, &mcp.Tool{
		Name: "create_task", Description: "Create a task in the worklog. Reuse requestId on retries; confirmDuplicate only after reviewing returned candidates.",
	}, handleCreateTask)
	mcp.AddTool(server, &mcp.Tool{
		Name: "update_task", Description: "Update task fields and append a progress event to task history and its daily log.",
	}, handleUpdateTask)
	mcp.AddTool(server, &mcp.Tool{
		Name: "record_work", Description: "Record a dated work fact in the daily log, optionally linked to existing tasks.",
	}, handleRecordWork)
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_work_summary", Description: "Return filtered work facts, source paths, and current open tasks for a day, week, month, year, all time, or custom date range.",
	}, handleGetWorkSummary)
	mcp.AddTool(server, &mcp.Tool{
		Name: "save_work_summary", Description: "Save a user-requested summary only when its source digest is still current; existing notes are never overwritten.",
	}, handleSaveWorkSummary)
	mcp.AddTool(server, &mcp.Tool{
		Name: "sync_tasks", Description: "Synchronize task titles from each task detail H1 to the task index and regenerate task status views.",
	}, handleSyncTasks)
}

func handleSyncTasks(ctx context.Context, req *mcp.CallToolRequest, input struct{}) (*mcp.CallToolResult, SyncTasksOutput, error) {
	result, err := worklogService.SyncTasks()
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SyncTasksOutput{}, err
	}
	return nil, SyncTasksOutput{
		Total: result.Total, Updated: result.Updated, UpdatedPaths: result.UpdatedPaths, Success: true,
	}, nil
}

func handleQueryTasks(ctx context.Context, req *mcp.CallToolRequest, input QueryTasksInput) (*mcp.CallToolResult, QueryTasksOutput, error) {
	for _, status := range input.Statuses {
		switch status {
		case "todo", "in_progress", "done", "blocked", "cancelled":
		default:
			return &mcp.CallToolResult{IsError: true}, QueryTasksOutput{}, fmt.Errorf("invalid status %q", status)
		}
	}
	limit := input.Limit
	if limit <= 0 {
		limit = 20
	} else if limit > 100 {
		limit = 100
	}
	offset := max(input.Offset, 0)
	tasks, total, err := worklogService.QueryTasks(worklog.TaskFilter{
		Query: strings.TrimSpace(input.Query), Statuses: input.Statuses,
		Project: strings.TrimSpace(input.Project), Limit: limit, Offset: offset,
	})
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, QueryTasksOutput{}, err
	}
	return nil, QueryTasksOutput{Tasks: tasks, Total: total, HasMore: offset+len(tasks) < total}, nil
}

func handleCreateTask(ctx context.Context, req *mcp.CallToolRequest, input CreateTaskInput) (*mcp.CallToolResult, CreateTaskOutput, error) {
	task, duplicates, err := worklogService.CreateTask(
		input.RequestID, input.Title, input.Description, input.Project, input.Priority, input.DueDate, input.ConfirmDuplicate,
	)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CreateTaskOutput{PartialFailure: strings.Contains(err.Error(), "created but")}, err
	}
	if len(duplicates) > 0 {
		return nil, CreateTaskOutput{DuplicateCandidates: duplicates}, nil
	}
	paths := []string{task.DetailPath, worklogService.IndexRelativePath()}
	paths = append(paths, worklogService.TaskViewPaths()...)
	logPath, err := worklogService.DailyLogPath(task.Created)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, CreateTaskOutput{Task: &task, PartialFailure: true}, err
	}
	paths = append(paths, logPath)
	return nil, CreateTaskOutput{Task: &task, CreatedPaths: paths, Success: true}, nil
}

func handleUpdateTask(ctx context.Context, req *mcp.CallToolRequest, input UpdateTaskInput) (*mcp.CallToolResult, UpdateTaskOutput, error) {
	if input.Title == nil && input.Status == nil && input.Project == nil && input.Priority == nil && input.DueDate == nil && input.Description == nil && input.Progress == "" {
		return &mcp.CallToolResult{IsError: true}, UpdateTaskOutput{}, fmt.Errorf("provide at least one field or progress event to update")
	}
	task, err := worklogService.UpdateTask(worklog.TaskUpdate{
		RequestID: input.RequestID, ID: input.TaskID, Title: input.Title, Status: input.Status,
		Project: input.Project, Priority: input.Priority, Due: input.DueDate,
		Description: input.Description, Progress: input.Progress,
	})
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, UpdateTaskOutput{}, err
	}
	logPath, err := worklogService.DailyLogPath(task.Updated)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, UpdateTaskOutput{Task: task}, err
	}
	return nil, UpdateTaskOutput{
		Task: task, EventID: input.RequestID,
		UpdatedPaths: append([]string{task.DetailPath, worklogService.IndexRelativePath()}, append(worklogService.TaskViewPaths(), logPath)...),
		Success:      true,
	}, nil
}

func handleRecordWork(ctx context.Context, req *mcp.CallToolRequest, input RecordWorkInput) (*mcp.CallToolResult, RecordWorkOutput, error) {
	path, err := worklogService.RecordWork(worklog.WorkEntry{
		RequestID: input.RequestID, Text: input.Text, Category: input.Category, Date: input.Date, TaskIDs: input.TaskIDs,
	})
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, RecordWorkOutput{}, err
	}
	date := input.Date
	if date == "" {
		date = path[strings.LastIndex(path, "/")+1 : len(path)-3]
	}
	return nil, RecordWorkOutput{Date: date, EventID: input.RequestID, Path: path, Success: true}, nil
}

func handleGetWorkSummary(ctx context.Context, req *mcp.CallToolRequest, input GetWorkSummaryInput) (*mcp.CallToolResult, worklog.Summary, error) {
	period := map[string]string{"day": "day", "week": "week", "month": "month", "year": "year", "all": "all", "custom": "custom"}[input.Granularity]
	if period == "" {
		return &mcp.CallToolResult{IsError: true}, worklog.Summary{}, fmt.Errorf("invalid granularity %q", input.Granularity)
	}
	if period == "all" && (input.From != "" || input.To != "") {
		return &mcp.CallToolResult{IsError: true}, worklog.Summary{}, fmt.Errorf("all-time summaries do not accept from or to")
	}
	if period == "custom" && (input.From == "" || input.To == "") {
		return &mcp.CallToolResult{IsError: true}, worklog.Summary{}, fmt.Errorf("custom summaries require both from and to")
	}
	summary, err := worklogService.GetSummary(worklog.SummaryOptions{Period: period, From: input.From, To: input.To})
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, worklog.Summary{}, err
	}
	return nil, summary, nil
}

func handleSaveWorkSummary(ctx context.Context, req *mcp.CallToolRequest, input SaveWorkSummaryInput) (*mcp.CallToolResult, SaveWorkSummaryOutput, error) {
	period := input.Granularity
	if period == "" {
		return &mcp.CallToolResult{IsError: true}, SaveWorkSummaryOutput{}, fmt.Errorf("invalid granularity %q", input.Granularity)
	}
	path, err := worklogService.SaveSummary(worklog.Summary{
		Period: period, From: input.From, To: input.To, SourceDigest: input.SourceDigest,
	}, input.Content, input.AlternateName)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, SaveWorkSummaryOutput{}, err
	}
	return nil, SaveWorkSummaryOutput{Path: path, Success: true}, nil
}
