---
name: obsitrace
description: Manage work tasks, record work, and produce daily, weekly, monthly, yearly and all-time work summaries in an Obsidian vault, including reading and writing vault notes and pushing local Markdown files into the vault. Use when the user wants to record work, create or update a task, mark progress or completion, review a period, prepare a status report, save a summary, or import a document into the vault.
compatibility: Requires obsitrace-mcp — directly, or as an upstream named "obsitrace" on mcp-gateway.
---

# Obsitrace

Use the dedicated worklog tools. The server owns the task index, date ranges, persistence and filtering:
never read or search the whole vault to assemble a summary, and never hand-edit index or status notes.

## Mode

- Tools appear directly (`query_tasks`, `create_task`, `read`, `write`, …): call them as tools.
- Only `list_servers` / `list_tools` / `call_tool` exist: tool names become arguments —

  ```js
  await tools.gateway.call_tool({
    server: "obsitrace",
    tool: "create_task",
    arguments: { requestId: "...", title: "...", project: "..." }
  })
  ```

  `list_tools({ server: "obsitrace" })` gives the compact index, `full: true` the JSON Schemas.
  On "unknown tool", list the tools again instead of guessing a name or prefix.

## Tool map (16)

| Tool | Purpose | Arguments |
|---|---|---|
| `create_task` | create a task under a project directory | `requestId`, `title`, `project`, `description?`, `dueDate?`, `priority?`, `confirmDuplicate?` |
| `query_tasks` | search the compact task index | `query?`, `project?`, `statuses?`, `limit?`, `offset?` |
| `update_task` | update fields and append progress | `taskId`, `requestId`, `progress?`, `status?`, `title?`, `description?`, `dueDate?`, `priority?`, `project?` |
| `record_work` | record one dated work fact | `requestId`, `text`, `date?`, `category?`, `taskIds?` |
| `get_work_summary` | filtered facts + open tasks for a period | `granularity`, `from?`, `to?` |
| `save_work_summary` | persist a summary when the digest is current | `content`, `granularity`, `from`, `to`, `sourceDigest`, `alternateName?` |
| `sync_tasks` | rebuild titles from each task's H1 | — |
| vault tools | `list`, `read`, `write`, `edit`, `search`, `tags`, `related`, `rename`, `delete` | see `full: true`; `delete` needs `confirm: "yes"` |

## Record or update work

- **Create**: collect a project name if missing (task details live under the project directory). Generate a
  fresh UUID for `requestId`; pass title, project and only the details the user gave. If
  `duplicateCandidates` come back, show the likely matches and ask; retry with the *same* `requestId` and
  `confirmDuplicate: true` only after confirmation.
- **Progress**: first `query_tasks` to resolve the task by id, title or project. If several match, ask which
  one. Summarize the user's message into one factual `progress` string and pass it with `taskId` and a fresh
  `requestId`. A non-empty `progress` moves `todo` to `in_progress` automatically; do not set `status`
  unless the user explicitly asks. Preserve any user-provided ticket or reply URL in that same entry; never
  infer missing details. With nothing meaningful to record, omit `progress` so the server applies its
  default.
- Statuses: `todo`, `in_progress`, `done`, `blocked`, `cancelled`; use `done` for completion.
- `record_work` is for a standalone fact (optionally linked via `taskIds`): choose `achievement`,
  `progress`, `blocker` or `next_step` when clear, otherwise omit `category`.
- `sync_tasks` only when the user explicitly asks to sync titles; it takes no arguments.
- One `requestId` per logical write; reuse it for retries, never across different actions.
- Report partial failures as partial. Do not claim a write succeeded unless the tool says so.

## Summarize work

Call `get_work_summary` with `granularity` = `day`, `week`, `month`, `year` or `all`; for an arbitrary
range use `custom` plus inclusive `from`/`to` (`YYYY-MM-DD`). Do not compute week boundaries yourself — the
server uses its own timezone, weeks run Sunday through Saturday.

Use only the returned facts and source paths as evidence, and structure the reply:

1. **成果** — delivered outcomes.
2. **进行中 / 阻塞** — current task states and recorded blockers, separated from what happened in the period.
3. **下一步** — recorded next steps and open tasks.
4. **覆盖范围** — the date range, and note `truncated` when true.

Never invent accomplishments, reasons, estimates or next steps. If the records do not support a section,
say so.

Call `save_work_summary` only when the user explicitly asks to save; pass the exact `from`, `to` and
`sourceDigest` from `get_work_summary`. If the source changed or the target exists, report it and ask about a
fresh summary or an alternate name.

## Pushing a document from a local file

`write` takes the note body inline, which costs one token per character. For a real document, stage the
file and pass the id in `contentUploadId`; only the id crosses the model context.

```bash
# through mcp-gateway
python3 <mcp-gateway clone>/skills/mcp-upload/scripts/mcp_upload.py --server obsitrace --path ./report.md
# direct HTTP to obsitrace-mcp (token = that server's --auth-token)
curl -sS -X POST "http://<server>:8787/upload?filename=report.md" \
  -H "Authorization: Bearer <token>" --data-binary @report.md
```

```js
await tools.gateway.call_tool({ server: "obsitrace", tool: "write",
  arguments: { path: "Inbox/report.md", contentUploadId: "<id>",
               frontmatter: { title: "report" } } })
```

- Stage UTF-8 text only. `write` replaces the whole body; frontmatter stays a separate argument.
- The id is single use and expires (`--upload-ttl`, 15 minutes). A failed `write` keeps it; a successful
  one consumes it.
- Size limit is `--max-upload` (10 MiB). Through the gateway, its `upload_requires_any` must allow
  `write`, otherwise the upload answers 403.
- Direct stdio has no `/upload` route: pass `content` inline there.
- Read the note back through the same channel to confirm.

## Fallbacks

- `unknown server "obsitrace"` → the gateway config lacks that upstream; do not attempt a direct connection.
- Gateway unreachable or token rejected → report the gateway error; do not fall back to `curl` or to
  reading the vault directly.
