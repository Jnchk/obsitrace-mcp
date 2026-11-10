---
name: obsitrace
description: Manage work tasks and create traceable daily, weekly, monthly, yearly, and all-time work summaries in an Obsidian vault through the obsitrace-mcp worklog tools. Use whenever the user wants to record work, create or update a task, mark progress or completion, review work for a period, prepare a status report, or save a work summary.
compatibility: Requires the obsitrace-mcp server with worklog tools enabled and connected to the user's Obsidian vault.
---

# Obsitrace Worklog

Use the dedicated worklog MCP tools for work records. The Go server handles indexing, date ranges, persistence, and filtering locally; do not read or search the whole vault to assemble a work summary.

## Record or update work

- For a new task, collect a project name if the user did not provide one; task details are stored under the project directory. Then call `create_task` with a fresh UUID `requestId`, title, project, and only the other details the user provided. If `duplicateCandidates` are returned, show the likely matches and ask whether to proceed; retry with the same `requestId` and `confirmDuplicate: true` only after confirmation.
- Task statuses are `todo`, `in_progress`, `done`, `blocked`, and `cancelled`; use `done` when marking a task complete.
- For task progress, first call `query_tasks` to resolve the task by ID, title, or project. If more than one result could match, ask the user which task they mean. Extract a concise, factual progress summary from the user's message and pass it as `progress` to `update_task`. Preserve any user-provided Redmine ticket or reply URL in that same progress entry; do not infer missing details. If the message contains no meaningful progress to summarize, omit `progress` so the server uses its default text. Preserve the same UUID `requestId` when retrying.
- Use `record_work` for a standalone work fact or a fact linked to existing task IDs. Choose `achievement`, `progress`, `blocker`, or `next_step` when the category is clear; otherwise omit it.
- A request ID identifies one logical write. Reuse it for retries; use a new UUID for a new action.
- Report partial failures as partial. Do not claim a write succeeded unless the tool reports success.

## Summarize work

Call `get_work_summary` with `granularity` set to `day`, `week`, `month`, `year`, or `all`. For a custom range, use `custom` and provide both inclusive `from` and `to` dates in `YYYY-MM-DD` form. Do not calculate week boundaries yourself; the server uses its configured timezone and weeks run Sunday through Saturday.

Use the returned facts and source paths as the evidence. Organize the response into:

1. **成果** — completed outcomes and delivered work.
2. **进行中 / 阻塞** — current task states and recorded blockers, clearly distinguishing current state from what happened during the selected period.
3. **下一步** — recorded next steps and open tasks.
4. **覆盖范围** — include the date range and mention `truncated` results when true.

Do not invent accomplishments, reasons, estimates, or next steps. If the records do not support a section, say so briefly rather than filling it with assumptions.

Only call `save_work_summary` when the user explicitly asks to save the generated summary. Pass the exact `from`, `to`, and `sourceDigest` returned by `get_work_summary`. If the source has changed or the target already exists, report that and ask whether they want a fresh summary or an alternate filename.
