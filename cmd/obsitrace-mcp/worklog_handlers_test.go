package main

import (
	"context"
	"testing"

	"github.com/Jnchk/obsitrace-mcp/internal/worklog"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestRegisterWorklogTools(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	registerWorklogTools(server)
}

func TestWorklogToolHandlers(t *testing.T) {
	service, err := worklog.New(t.TempDir(), worklog.Config{Timezone: "Asia/Shanghai"})
	if err != nil {
		t.Fatal(err)
	}
	oldService := worklogService
	worklogService = service
	t.Cleanup(func() { worklogService = oldService })

	_, created, err := handleCreateTask(context.Background(), nil, CreateTaskInput{
		RequestID: "550e8400-e29b-41d4-a716-446655440100", Title: "Write handler tests", Project: "Team Management",
	})
	if err != nil || !created.Success || created.Task == nil || len(created.CreatedPaths) != 5 {
		t.Fatalf("create handler: output=%+v err=%v", created, err)
	}

	_, synced, err := handleSyncTasks(context.Background(), nil, struct{}{})
	if err != nil || !synced.Success || synced.Total != 1 || synced.Updated != 0 || len(synced.UpdatedPaths) != 3 {
		t.Fatalf("sync handler: output=%+v err=%v", synced, err)
	}

	_, updated, err := handleUpdateTask(context.Background(), nil, UpdateTaskInput{
		RequestID: "550e8400-e29b-41d4-a716-446655440101",
		TaskID:    created.Task.ID, Progress: "Tests are underway",
	})
	if err != nil || !updated.Success || updated.Task.Status != "in_progress" || len(updated.UpdatedPaths) != 5 {
		t.Fatalf("update handler: output=%+v err=%v", updated, err)
	}

	_, recorded, err := handleRecordWork(context.Background(), nil, RecordWorkInput{
		RequestID: "550e8400-e29b-41d4-a716-446655440102", Text: "Added lifecycle coverage",
	})
	if err != nil || !recorded.Success || recorded.Path == "" {
		t.Fatalf("record handler: output=%+v err=%v", recorded, err)
	}

	_, query, err := handleQueryTasks(context.Background(), nil, QueryTasksInput{Statuses: []string{"in_progress"}})
	if err != nil || query.Total != 1 || len(query.Tasks) != 1 || query.Tasks[0].ID != created.Task.ID {
		t.Fatalf("query handler: output=%+v err=%v", query, err)
	}

	_, summary, err := handleGetWorkSummary(context.Background(), nil, GetWorkSummaryInput{Granularity: "day"})
	if err != nil || len(summary.Facts) < 2 || summary.Timezone != "Asia/Shanghai" {
		t.Fatalf("summary handler: output=%+v err=%v", summary, err)
	}
	_, saved, err := handleSaveWorkSummary(context.Background(), nil, SaveWorkSummaryInput{
		Granularity: "day", From: summary.From, To: summary.To,
		Content: "## Achievements\n- Added worklog handler tests", SourceDigest: summary.SourceDigest,
	})
	if err != nil || !saved.Success || saved.Path == "" {
		t.Fatalf("save handler: output=%+v err=%v", saved, err)
	}
}
