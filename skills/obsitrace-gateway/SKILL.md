---
name: obsitrace-gateway
description: Manage obsitrace work tasks and generate work summaries when MCP access goes through mcp-gateway. Use when recording work, creating or updating tasks, reviewing a period, or saving a summary, and the obsitrace tools are reachable only as gateway_call_tool(server="obsitrace", ...).
compatibility: Requires the mcp-gateway MCP server with an upstream named "obsitrace"; the direct obsitrace-mcp tools are not registered in this mode.
---

# Obsitrace Worklog (through mcp-gateway)

In gateway mode the client has three tools (`list_servers`, `list_tools`, `call_tool`) and the obsitrace
tools live behind them. Names like `create_task` or `query_tasks` are **arguments**, not tools:

```js
await tools.gateway.call_tool({
  server: "obsitrace",
  tool: "create_task",
  arguments: { requestId: "...", title: "...", project: "..." }
})
```

- Discover: `await tools.gateway.list_tools({ server: "obsitrace" })` gives a compact index.
- Need exact argument names or types: `{ server: "obsitrace", full: true }` returns the JSON Schemas.
- Never call `obsitrace_<tool>` — those tool names no longer exist in gateway mode. If a call fails
  with "unknown tool", list the tools again instead of guessing.
- Never read or search the whole vault to assemble a summary; the server has the index and date logic.

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
  fresh UUID for `requestId`; pass title, project, and only the details the user gave. If
  `duplicateCandidates` come back, show the likely matches and ask; retry with the *same* `requestId` and
  `confirmDuplicate: true` only after confirmation.
- **Progress**: first `query_tasks` to resolve the task by id, title or project. If several match, ask which
  one. Summarize the user's message into one factual `progress` string and pass it with `taskId` and a fresh
  `requestId`. A non-empty `progress` moves `todo` to `in_progress` automatically; do not set `status`
  unless the user explicitly asks. Preserve any Redmine ticket or reply URL in that same entry; never infer
  missing details. If there is no meaningful progress, omit `progress` so the server applies its default.
- Statuses: `todo`, `in_progress`, `done`, `blocked`, `cancelled`; use `done` for completion.
- `record_work` is for a standalone fact (optionally linked via `taskIds`): choose `achievement`,
  `progress`, `blocker` or `next_step` when clear, otherwise omit `category`.
- `sync_tasks` only when the user explicitly asks to sync titles; it takes no arguments.
- One `requestId` per logical write; reuse it for retries, never across different actions.
- Report partial failures as partial. Do not claim a write succeeded unless the gateway result says so.

## Summarize work

Call `get_work_summary` with `granularity` = `day`, `week`, `month`, `year` or `all`; for an arbitrary range
use `custom` plus inclusive `from`/`to` (`YYYY-MM-DD`). Do not compute week boundaries yourself — the server
uses its own timezone, weeks run Sunday through Saturday.

Use only the returned facts and source paths as evidence, and structure the reply:

1. **成果** — delivered outcomes.
2. **进行中 / 阻塞** — current task states and recorded blockers, separated from what happened in the period.
3. **下一步** — recorded next steps and open tasks.
4. **覆盖范围** — the date range, and note `truncated` when true.

Never invent accomplishments, reasons, estimates or next steps. If the records do not support a section, say so.

Call `save_work_summary` only when the user explicitly asks to save; pass the exact `from`, `to` and
`sourceDigest` from `get_work_summary`. If the source changed or the target exists, report it and ask about a
fresh summary or an alternate name.

## Fallbacks

- `unknown server "obsitrace"` → the gateway config is missing that upstream; do not try a direct connection.
- Gateway unreachable or token rejected → report the gateway error; do not fall back to `curl` or to reading
  the vault directly.
- For read-only inspection use the same `call_tool` path; the vault tools are also behind the gateway.
