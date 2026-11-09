# obsitrace-mcp

A Model Context Protocol (MCP) server that enables AI assistants to read, write, and search your Obsidian vault — extended with **task management and worklog** tools so an AI can track your tasks, record your daily work, and generate period summaries.

> **Credits & provenance**
>
> This project is built on top of [obsidian-mcp](https://github.com/taigrr/obsidian-mcp) by [Tai Groot](https://github.com/taigrr), a wonderful Go implementation of an Obsidian MCP server. All the core vault capabilities (read/write/edit/search/delete/rename/list, frontmatter handling, and the security model) come from that project — full credit and thanks to Tai for the original work.
>
> What this fork adds on top is the **worklog task-management layer**: six dedicated tools, a `internal/worklog` service, and a companion [Skill](skills/obsitrace/SKILL.md) that let AI assistants manage tasks and work records directly in your vault. The design follows [docs/00requirement.md](docs/00requirement.md) and [docs/01design.md](docs/01design.md).

## Task Management (Worklog)

The headline feature of this fork: natural-language task tracking and work logging, persisted entirely as **human-readable Markdown** in your vault — no database, no lock-in. Ask your AI assistant in plain language:

- *"记一下，下周三前完成客户数据核对。"*
- *"把接口联调标记为完成，结果是测试环境通过。"*
- *"总结我这周做了什么，还有哪些没完成。"*

### How it works

```text
User (natural language)
  │
  ▼
Skill (skills/obsitrace) — intent recognition & clarification
  │
  ▼
Go MCP server — filtering, aggregation, date math, compact results
  │
  ▼
Obsidian vault (Markdown) — task index · task notes · daily logs · summaries
```

All filtering and aggregation happens **inside the MCP server process**: the model only ever receives compact, relevant results instead of scanning your whole vault, keeping token usage low and every fact traceable to its source note.

### Vault data model

| Data | Default path |
|---|---|
| Task index (authoritative status) | `Worklog/Index/Task Index.md` |
| Ongoing view (`todo` / `in_progress` / `blocked`) | `Worklog/Index/Task Ongoing.md` |
| Done view (`done` / `cancelled`) | `Worklog/Index/Task Done.md` |
| Task detail notes | `Worklog/Tasks/<project>/<task-id>.md` |
| Daily logs | `Worklog/Logs/YYYY/MM/YYYY-MM-DD.md` |
| Period summaries | `Worklog/Summaries/<Daily\|Weekly\|Monthly\|Yearly\|All Time\|Custom>/…` |

Key behaviors:

- **Five statuses** — `todo`, `in_progress`, `done`, `blocked`, `cancelled`. Completed tasks stay in place and are only marked done; cancelled tasks are kept for history.
- **Append-only history** — every progress/status change is appended to the task note with a dated, uniquely-identified event; retries never duplicate events.
- **Duplicate protection** — `create_task` returns candidate duplicates first (using a stable `requestId`-derived task ID) so your AI asks before creating a near-copy.
- **No silent overwrites** — summaries are saved only as new files (a source digest guards freshness), and index/log writes abort on external modifications rather than clobbering them.
- **Consistent time semantics** — period boundaries (week = Sunday–Saturday; Gregorian months and years; "all time" since the index's `started` date) are computed server-side in the configured timezone, so "today" and "this week" mean the same thing every time.
- **Traceable summaries** — each returned fact carries its date and source path; results are capped (500 facts / 50 active tasks) with explicit truncation flags.

### Worklog tools

| Tool | Description |
|---|---|
| `query_tasks` | Filter the compact task index by ID, title, project, or status; returns only matched tasks with pagination. |
| `create_task` | Create a task, its detail note, index entry, and daily log event; shows duplicate candidates first. |
| `update_task` | Update task fields/status and append progress history to the task note and daily log. |
| `record_work` | Append a dated work fact to the daily log, optionally linked to existing tasks. |
| `get_work_summary` | Return period-filtered work facts, source paths, and currently open tasks. |
| `save_work_summary` | Persist a summary as a new note only when its source digest is still current. |

### Environment configuration

- `OBSIDIAN_TIMEZONE` — IANA timezone used for worklog dates and period boundaries; defaults to `Asia/Shanghai`. An invalid timezone prevents startup (it never silently falls back to the system clock).
- `OBSIDIAN_WORKLOG_INDEX_PATH`, `OBSIDIAN_WORKLOG_TASK_DIR`, `OBSIDIAN_WORKLOG_LOG_DIR`, `OBSIDIAN_WORKLOG_SUMMARY_DIR` — optional overrides for the paths above. Each must be relative to the vault root.

```json
{
  "mcpServers": {
    "obsidian": {
      "command": "obsitrace-mcp",
      "args": ["/path/to/your/vault"],
      "env": {
        "OBSIDIAN_TIMEZONE": "Asia/Shanghai"
      }
    }
  }
}
```

### Example: creating a task

```json
{
  "tool": "create_task",
  "arguments": {
    "requestId": "550e8400-e29b-41d4-a716-446655440000",
    "title": "Check customer data",
    "project": "Customer project",
    "description": "Confirm data discrepancies and record the results.",
    "dueDate": "2026-11-12"
  }
}
```

If the title looks similar to an existing task, the server returns `duplicateCandidates` instead of writing — ask the user, then retry with the **same `requestId`** and `confirmDuplicate: true`.

### Example: getting a work summary

```json
{
  "tool": "get_work_summary",
  "arguments": {
    "granularity": "week"
  }
}
```

Returns the effective timezone, the resolved date range, dated work facts with source paths, current open tasks, and a source digest — ready for the assistant to turn into an *"成果、进行中/阻塞、下一步"* style report.

See [the worklog Skill](skills/obsitrace/SKILL.md) for the natural-language workflow, and [docs/01design.md](docs/01design.md) for full tool schemas, date rules, and consistency guarantees.

## Core vault features (from upstream obsidian-mcp)

Everything below originates in [taigrr/obsidian-mcp](https://github.com/taigrr/obsidian-mcp) and is preserved here:

- **Full vault access** — Read, write, edit, delete, rename, list, and search notes
- **Vault discovery** — Browse folders before opening a note
- **Frontmatter support** — Parse and update YAML frontmatter seamlessly
- **Token optimized** — Compact JSON responses for efficient AI interactions
- **Universal compatibility** — Works with any MCP-compatible AI (Claude, ChatGPT, etc.)

### Note tools

| Tool | Description |
|---|---|
| `read` | Read a note with frontmatter and content. Supports pagination. |
| `write` | Create or overwrite a note with content and optional frontmatter. |
| `edit` | Replace text and/or update frontmatter in an existing note. |
| `delete` | Delete a note (requires confirmation). |
| `rename` | Move or rename a note to a new path. |
| `search` | Full-text search with regex support and matching context. |
| `related` | Find notes related by tags or wiki-links. |
| `tags` | List unique tags across the vault. |
| `list` | List files and subdirectories in a vault directory. |

### Examples

#### Writing a note with frontmatter

```json
{
  "tool": "write",
  "arguments": {
    "path": "notes/new-note.md",
    "content": "# My Note\n\nThis is the content.",
    "frontmatter": {
      "tags": ["idea", "project"],
      "status": "draft"
    }
  }
}
```

#### Searching the vault

```json
{
  "tool": "search",
  "arguments": {
    "query": "TODO",
    "contextLines": 2
  }
}
```

## Installation

```bash
go install github.com/Jnchk/obsitrace-mcp/cmd/obsitrace-mcp@latest
```

Or build from source:

```bash
git clone https://github.com/Jnchk/obsitrace-mcp.git
cd obsitrace-mcp
go build -o obsitrace-mcp ./cmd/obsitrace-mcp
```

## Usage

```bash
obsitrace-mcp [vault-path]
```

The vault path is a **positional argument and optional**: if omitted, the server uses the current working directory as the vault root. At most one path may be given.

```bash
# Serve a specific vault
obsitrace-mcp ~/obsidian

# Serve the current directory
obsitrace-mcp
```

Once started, the server communicates over **stdio** using the MCP protocol — it has no network endpoint. Point your MCP client at the binary and pass the vault path via `args` (see the [MCP configuration](#environment-configuration) example above).

Worklog behavior (timezone and directory paths) is configured **entirely through environment variables**, not command-line flags — see [Environment configuration](#environment-configuration).

### Command-line options

Built on [cobra](https://github.com/spf13/cobra) with [fang](https://github.com/charmbracelet/fang) for styled help output:

| Flag | Description |
|---|---|
| `-h`, `--help` | Show usage and exit. |
| `--version` | Print the version and exit. Versions come from `-ldflags "-X main.version=..."` at build time, or fall back to the module/git build info; untagged builds report `dev`. |

Tab completions and manpage generation are explicitly disabled in this build.

## Security

- **Path filtering** — Blocks access to `.obsidian/`, `.git/`, `node_modules/`, and system files
- **File type restrictions** — Only allows `.md`, `.markdown`, and `.txt` files
- **Path traversal prevention** — All paths validated to stay within vault boundaries
- **Confirmation required** — Destructive operations require explicit confirmation
- **Worklog confinement** — Worklog tools only touch the configured index/task/log/summary paths and generate paths server-side

## Development

```bash
go build ./...        # Build
go test ./...         # Test
go vet ./...          # Lint
```

Design documentation lives in [docs/](docs/): [00requirement.md](docs/00requirement.md) (requirements) and [01design.md](docs/01design.md) (design & tool schemas).

## License

[0BSD](LICENSE) — same as the upstream project.

- **Original obsidian-mcp**: © Tai Groot — https://github.com/taigrr/obsidian-mcp
- **This task-management extension (Jnchk/obsitrace-mcp)**: built on the original work with gratitude.
