# Obsidian Worklog与总结技能设计文档

- **状态：** 已实现（首版）
- **日期：** 2026-11-09
- **依据：** [需求分析报告](./00requirement.md)

## 1. 概述

本设计采用“**Skill 负责自然语言交互，Go 版 Obsidian MCP 负责数据读写、筛选与聚合，Obsidian Markdown 负责持久化**”的结构。筛选和日期计算在 MCP 服务进程内执行，模型只接收命中的任务或精简的工作事实，不需要通过 Skill 脚本另建客户端连接 MCP。

初始版本覆盖任务创建、任务查询与更新、独立Worklog、按日期范围总结及可选保存总结。默认不迁移已有笔记；实际 Vault 若已有目录或模板，则沿用其结构并配置对应路径。

## 2. 设计原则

1. **服务端先过滤：** 不把全库或完整索引返回给模型。
2. **单一事实来源：** 总索引保存任务当前状态；任务详情保存任务说明和进展历史；日志保存非任务工作并索引当日发生的任务事件。
3. **可追溯：** 总结中的工作事实均能追溯到源笔记和日期。
4. **人机可读：** Markdown 笔记可在 Obsidian 中直接查看和维护。
5. **失败显式且可恢复：** 多笔记更新部分失败时报告已完成步骤，重试不重复记入事件。
6. **渐进实现：** 复用现有 Go MCP 服务和 YAML 依赖；Worklog只访问配置范围内、由服务生成的相对路径，不增加独立脚本客户端或数据库。

## 3. 组件与边界

```text
用户
  │ 自然语言请求 / 最终总结
  ▼
Skill (SKILL.md)
  │ MCP tool call（由宿主管理）
  ▼
Go MCP server（现有 obsitrace-mcp，stdio）
  ├─ Worklog工具 handler
  ├─ internal/worklog：索引、任务、日志、日期范围与汇总逻辑
  └─ Worklog路径校验与 Markdown/YAML 读写
  │
  ▼
Obsidian Vault（Markdown）
```

### 3.1 Skill

- 把自然语言转换为工具参数；缺少可选字段时不打断快速记录。
- 使用工具返回的候选结果解决同名/近似任务歧义；无法唯一确认时询问用户。
- 对 `get_work_summary` 返回的事实进行归纳和润色，不添加工具结果中没有的事实。
- 仅在用户明确要求保存总结时调用保存工具。
- 汇报 MCP 实际成功、失败或部分成功的操作，不将部分成功描述为整体成功。

Skill 中不实现 Vault 遍历、日期过滤或重复的 Markdown 解析逻辑。

### 3.2 Go MCP 服务

- 在 Vault 本地解析指定索引和笔记，执行筛选、日期边界计算、历史关联和结果压缩。
- 通过 MCP 工具暴露Worklog语义，而不是让 Skill 组合通用 `search`、`read`、`write` 工具完成多步数据处理。
- 继续使用现有 stdio 传输；Worklog工具不开放任意路径，校验相对路径、Vault 边界和符号链接，并以快照校验避免覆盖处理中被外部改动的文件。
- 新逻辑放入 `internal/worklog`，工具输入/输出及注册沿用 `cmd/obsitrace-mcp` 现有模式。

## 4. Vault 数据模型

### 4.1 路径约定

默认相对 Vault 根目录：

| 用途 | 默认路径 |
|---|---|
| Task index | `Worklog/Index/Task Index.md` |
| Task ongoing view | `Worklog/Index/Task Ongoing.md` |
| Task done view | `Worklog/Index/Task Done.md` |
| Task detail | `Worklog/Tasks/<project>/<task-id>.md` |
| Daily log | `Worklog/Logs/YYYY/MM/YYYY-MM-DD.md` |
| Summary | `Worklog/Summaries/Daily/YYYY-MM-DD.md`, `Worklog/Summaries/Weekly/YYYY-MM-DD.md` (week start), `Worklog/Summaries/Monthly/YYYY-MM.md`, `Worklog/Summaries/Yearly/YYYY.md`, `Worklog/Summaries/All Time/All Time.md`, `Worklog/Summaries/Custom/YYYY-MM-DD_to_YYYY-MM-DD.md` |

完整目录结构：

```text
Worklog/
├── Index/
│   ├── Task Index.md
│   ├── Task Ongoing.md
│   └── Task Done.md
├── Tasks/
│   └── <project>/
│       └── <task-id>.md
├── Logs/
│   └── YYYY/
│       └── MM/
│           └── YYYY-MM-DD.md
└── Summaries/
    ├── Daily/
    │   └── YYYY-MM-DD.md
    ├── Weekly/
    │   └── YYYY-MM-DD.md
    ├── Monthly/
    │   └── YYYY-MM.md
    ├── Yearly/
    │   └── YYYY.md
    ├── All Time/
    │   └── All Time.md
    └── Custom/
        └── YYYY-MM-DD_to_YYYY-MM-DD.md
```

创建新任务时，Skill 必须从用户请求中取得项目名称；若未提供，先询问后再调用 `create_task`。项目名作为单个目录名使用；包含路径分隔符或文件系统保留字符时拒绝写入，不进行可能造成名称冲突的静默改写。已有任务笔记不自动移动，索引中的详情路径是其实际路径。

`Index/Task Index.md` 是全部任务的权威索引。`Index/Task Ongoing.md` 展示 `todo`、`in_progress`、`blocked`；`Index/Task Done.md` 展示 `done`、`cancelled`。两个列表页由 Go 服务根据总索引生成，在创建/更新任务时同步刷新；若同路径存在非生成文件，写入失败并保护原内容。三个页面使用相同的 Markdown 表格格式。

通过 `mcp-config.json` 的 Obsidian MCP server `env` 配置时区。服务端从环境变量 `OBSIDIAN_TIMEZONE` 读取 IANA 时区标识；未设置时使用 `Asia/Shanghai`。若时区无效，MCP 服务启动时报错并停止，不静默回退到系统时区。Worklog路径按表中默认值使用；可用 `OBSIDIAN_WORKLOG_INDEX_PATH`、`OBSIDIAN_WORKLOG_TASK_DIR`、`OBSIDIAN_WORKLOG_LOG_DIR` 和 `OBSIDIAN_WORKLOG_SUMMARY_DIR` 覆盖索引、任务、日志和总结目录，均为相对 Vault 根目录的路径。不得遍历全 Vault 猜测目录或自动移动旧文件。

`mcp-config.json` 配置示例：

```json
{
  "mcpServers": {
    "obsidian": {
      "command": "obsitrace-mcp",
      "args": ["<vault-path>"],
      "env": {
        "OBSIDIAN_TIMEZONE": "Asia/Shanghai",
        "OBSIDIAN_WORKLOG_INDEX_PATH": "Worklog/Index/Task Index.md",
        "OBSIDIAN_WORKLOG_TASK_DIR": "Worklog/Tasks",
        "OBSIDIAN_WORKLOG_LOG_DIR": "Worklog/Logs",
        "OBSIDIAN_WORKLOG_SUMMARY_DIR": "Worklog/Summaries"
      }
    }
  }
}
```

此配置由 MCP 宿主读取，并将 `env` 传给 stdio 子进程；MCP 服务不自行查找或解析宿主的配置文件。

目录只在首次创建记录时按需生成。首次写入前检查目标文件；目标已存在但不符合预期格式时返回可操作错误，不覆盖。`OBSIDIAN_WORKLOG_TASK_DIR` 是项目子目录的根目录，默认值为 `Worklog/Tasks`。

### 4.2 总任务索引

索引使用 Markdown 表格作为当前状态的唯一权威来源。全量索引和两个派生列表均使用相同的 YAML frontmatter；`started` 记录 Worklog 体系启用日：

```yaml
---
type: work-task-index
schemaVersion: 1
started: 2026-11-09
---
```

正文先包含标题和说明，随后是固定列顺序的任务表格。可选值为空时保留空单元格，不省略列：

```markdown
# Worklog Task Index

This index tracks the current status of all work tasks. See each linked task note for details.

| ID | Title | Status | Project | Priority | Created | Updated | Due | Completed | Details |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| task-550e8400-e29b-41d4-a716-446655440000 | Check customer data | in_progress | Customer project |  | 2026-11-09 | 2026-11-09 | 2026-11-12 |  | [[Worklog/Tasks/Customer project/task-550e8400-e29b-41d4-a716-446655440000.md\|task-550e8400-e29b-41d4-a716-446655440000]] |
```

- Status values are limited to `todo`, `in_progress`, `done`, `blocked`, and `cancelled`.
- 日期使用 `YYYY-MM-DD`；可选字段留空。
- 字段顺序固定；标题和项目中的换行规范化为空格，Markdown 表格分隔符转义；详情链接使用带任务 ID 别名的 Obsidian Wiki 链接。
- 不符合结构、重复 ID 或无效状态时，查询/更新返回格式错误并给出行号，不尝试重建索引。
- 写入时重绘任务表格并保留标题、说明等前置文本；任务顺序保持不变。
- 查询在 Go 服务端完成过滤，只返回命中行及必要字段。查询结果设定最大返回数并支持分页/`hasMore`，避免大结果集占满上下文。

首个 Worklog 写入时，若索引不存在，MCP 服务按规范创建带 `started` 日期的索引标题、说明和空表格；`started` 取写入时配置时区的当前日期，不受补记日志日期影响。索引创建与首次日志/任务写入按部分失败规则处理。

### 4.3 任务详情笔记

新任务详情使用 YAML frontmatter 和 Markdown 正文，写入 `Worklog/Tasks/<project>/<task-id>.md`。task ID 由服务端从调用方 `requestId` 确定性生成（`task-<requestId>`），以便在创建响应丢失后安全重试而不遍历任务目录：

```markdown
---
type: work-task
id: task-550e8400-e29b-41d4-a716-446655440000
created: 2026-11-09T14:30:00+08:00
---

# Check customer data

## Description
Confirm customer data discrepancies and record the results.

## Progress History
- 2026-11-09T14:30:00+08:00 [event:550e8400-e29b-41d4-a716-446655440000] [created] Task created.
- 2026-11-09T16:10:00+08:00 [event:550e8400-e29b-41d4-a716-446655440002] [in_progress] About 70% complete; confirming discrepancies.
```

- `id` 必须与索引 ID 一致；frontmatter `type` 固定为 `work-task`。
- 进展历史只追加，不重写既有事件；每个事件包含 RFC3339 时间、全局唯一事件 ID、事件类型和简短说明。为保持可解析，事件正文按单行存储，换行规范化为空格。
- 任务状态以索引为准；历史事件说明曾发生的变更，不反推当前状态。
- 更新前验证笔记 frontmatter 和 ID；ID 不匹配或目标内容损坏时停止写入并报告，不覆盖。

### 4.4 每日日志

日志标题和 frontmatter 标识本地日期。事件以 Markdown 列表呈现易读时间、事件类别/类型、关联任务链接和内容；机器所需的 RFC3339 时间、任务 ID 和幂等事件 ID 放在 Obsidian `%%...%%` 注释中，避免在阅读视图和实时预览中显示。读取器兼容旧的 HTML 注释和方括号事件格式：

```markdown
---
type: work-daily-log
date: 2026-11-09
---

# Work Log - 2026-11-09

- **15:00** · **achievement / recorded** — Integration testing passed in the test environment. %%worklog:2026-11-09T15:00:00+08:00|achievement|recorded||550e8400-e29b-41d4-a716-446655440003%%
- **16:10** · **task / progress** · [[Worklog/Tasks/Customer project/task-550e8400-e29b-41d4-a716-446655440000.md|task-550e8400-e29b-41d4-a716-446655440000]] — About 70% complete; confirming discrepancies. %%worklog:2026-11-09T16:10:00+08:00|task|progress|task-550e8400-e29b-41d4-a716-446655440000|550e8400-e29b-41d4-a716-446655440002%%
```

- `record_work` 创建的Worklog（可关联任务）及任务状态/进度事件均写入当日日志，事件正文同时写入任务详情历史，便于按时间范围筛选后直接生成总结。新日志带日期 frontmatter；事件机器元数据放在隐藏的 Obsidian 注释中，旧格式仍可解析。
- 每条记录均带唯一 event ID。事件 ID 用于任务详情和日志之间关联、查询去重及重试幂等；创建任务的首条事件 ID 与创建请求的 `requestId` 相同。
- 更新现有日志采用读取、解析、追加、写回流程；未知章节和普通正文保留。若无法安全解析，不做覆盖。

### 4.5 周期总结

按需保存至“总结”目录，frontmatter 包含 `type: work-summary`、`period`、`from`、`to`、`generatedAt`、`sourcePaths`、`sourceDigest` 和由 MCP 生成的结构化历史 `facts`。当前实现的 `sourceDigest` 基于范围内匹配的日志事件行及任务索引原始内容计算；`sourcePaths` 指向原始日志和任务索引，而非中间总结。正文由 Skill 生成，保存工具不校验正文结构。当前待办/进行中/阻塞状态始终从任务索引实时读取。

总结是派生数据而非事实来源。当前版本不会把已保存总结作为查询缓存；每次请求都从本地日志和索引重新筛选/计算。用户未明确要求保存前不持久化新总结。目标总结文件已存在时，保存只允许“文件不存在时创建”；绝不静默覆盖已有总结。

## 5. MCP 工具接口

Worklog工具在现有 `registerTools` 中注册，输入输出使用显式 Go struct 和 JSON Schema 描述。工具名、必填字段和枚举值保持稳定；结果仅包含后续交互所需字段。

### 5.1 `query_tasks`

**输入**

| 字段 | 类型 | 说明 |
|---|---|---|
| `query` | string，可选 | 匹配 ID、标题或项目；空值表示仅按其他过滤条件查询 |
| `statuses` | string[]，可选 | 状态过滤；每项必须是受支持状态 |
| `project` | string，可选 | 项目精确过滤 |
| `limit` | integer，可选 | 默认 20，最大 100 |
| `offset` | integer，可选 | 默认 0，不能小于 0 |

**输出**：`tasks`（精简任务记录数组）、`total`、`hasMore`。默认按状态、截止日期和 ID 稳定排序；不得返回完整任务笔记正文。

### 5.2 `create_task`

**输入**：`requestId`、`title`、`project` 必填；`description`、`dueDate`、`priority` 可选；`confirmDuplicate` 可选，默认为 `false`。`requestId` 是调用方生成的 UUID，为此次逻辑操作提供唯一标识，重试时原样复用。日期以 `YYYY-MM-DD` 输入。未提供项目时 Skill 先询问，不创建默认项目目录。新任务初始状态固定为 `todo`，创建时间由服务端生成。

**处理**：校验字段和项目目录名；用索引检查可能重复项。若发现候选且 `confirmDuplicate=false`，返回 `duplicateCandidates` 而不写入；Skill 询问用户后使用原 `requestId` 和 `confirmDuplicate=true` 重试。由 `requestId` 生成稳定任务 ID，在 `Worklog/Tasks/<project>/` 创建详情笔记并更新总索引、状态视图和日志。修改项目名称时详情笔记随项目目录迁移；目标冲突时不覆盖。

**输出**：`task` 精简记录、`createdPaths`（任务详情、总索引、状态视图和日志路径）、`success`、`partialFailure`。若多文件写入只有一处成功，返回失败和已完成步骤，不返回成功形态。

### 5.3 `update_task`

**输入**：`requestId`、`taskId` 必填；`status` 可为 `todo`、`in_progress`、`done`、`blocked` 或 `cancelled`；`progress`、`dueDate`、`project`、`priority`、`title`、`description` 可选。`requestId` 在同一逻辑操作重试时必须保持不变。

**处理**：按 ID 在索引定位唯一任务；读取并验证详情笔记；以 `requestId` 作为 event ID，使用服务端时间；更新详情历史、索引当前字段，并在当日日志登记事件引用。Skill 从用户本次更新描述中提炼有事实依据的进展摘要，并将用户提供的 Redmine 引用包含在同一条 `progress` 事件中；无法提炼有意义进展时省略 `progress`，由服务端使用默认文本。相同 `requestId` 重试时返回既有结果或补齐未完成写入，不重复追加。

**输出**：最新精简任务、event ID 和目标路径。索引、详情或日志任一操作失败均以 MCP 错误及部分失败上下文返回。

### 5.4 `record_work`

**输入**：`requestId`、`text` 必填；`category` 可选，枚举为 `achievement`、`progress`、`blocker`、`next_step`；`taskIds` 可选，引用的任务 ID 必须存在。`requestId` 在同一逻辑操作重试时必须保持不变。日期默认为服务端当前日期；如允许补记历史，`date` 必须明确指定且服务端校验格式。

**处理**：只追加到对应日期日志，以 `requestId` 作为 event ID；保留现有内容。若任务索引尚不存在且未关联任务，先按规范初始化索引并设置 `started`。引用的任务 ID 必须已存在于索引。

**输出**：日期、event ID、日志路径和写入结果。

### 5.5 `get_work_summary`

**输入**：`granularity` 必填，枚举 `day`、`week`、`month`、`year`、`all`、`custom`。对 `day`、`week`、`month`、`year`，`from`、`to` 均可省略；省略时服务端按配置时区计算当前对应周期范围。`custom` 必须提供 `from`、`to`（闭区间，`YYYY-MM-DD`）；其他粒度也可提供两者以查询历史范围，但必须与粒度边界一致。`all` 不传日期，由服务端从索引 frontmatter 的 `started` 字段取起始日，并以当前配置时区日期为结束日。索引不存在或缺少合法 `started` 日期时返回初始化错误，不猜测起点。Skill 将“今天”“本周”等转换为 `granularity`，由服务端使用同一配置时区计算边界。

**处理**：

1. 读取服务启动时校验过的时区位置；若缺少显式日期，使用该位置计算当前粒度边界；校验起止日期为闭区间日期，不使用本机默认时区。
2. 在 Go 服务端本地遍历配置的日志目录，按记录时间戳过滤闭区间日期范围；不搜索其他 Vault 目录。
3. 读取任务索引以获取当前待办、进行中、阻塞事项；索引内容参与 `sourceDigest` 校验，不将当前状态误标成期间完成。
4. 整理带日期、类别、文本、关联任务和日志来源路径的事实列表。
5. 返回数据覆盖范围、事实数组和是否截断标志；最多返回 500 条事实和 50 条当前任务，达到上限时明确提示。

**输出**：`from`、`to`、`timezone`（实际生效的 IANA 时区）、`facts[]`、`activeTasks[]`、`sourcePaths[]`、`sourceDigest`、`truncated`。`activeTasks` 来自实时索引，包含待办、进行中、阻塞状态，最多返回 50 条并与 `facts` 共用截断标志。来源摘要由范围内日志事件及索引原始内容计算。MCP 服务返回事实，不代替模型撰写主观绩效判断。

### 5.6 `save_work_summary`

仅在用户明确要求保存总结时调用。输入包括 `granularity`、`from`、`to`、总结正文和 `get_work_summary` 返回的 `sourceDigest`；可选 `alternateName` 只能是文件名，不能包含目录分隔符。服务端重新读取并校验当前来源摘要与该 digest 一致，来源路径和结构化 `facts` 由服务端重算而非信任调用者提供；日期合法、正文非空且目标文件不存在时才写入对应周期总结笔记。若默认目标已存在，调用者可指定安全文件名在同一总结目录另存；绝不覆盖。

### 5.7 `sync_tasks`

无输入。显式读取总索引中的所有任务详情，验证 frontmatter 类型及任务 ID，并从每篇详情提取第一个 H1（标题必须非空）；所有详情均通过校验后，才将标题写回总索引并重新生成 `Task Ongoing`、`Task Done`。返回任务总数、标题变更数及三个目标路径。该操作不修改任务更新时间、不追加 Progress History 或日志事件；遇到详情缺失、frontmatter 无效或缺少有效 H1 时在任何写入前报错。同步写入阶段仍复用索引快照校验及现有多文件写入错误处理。

## 6. 主要操作流程

### 6.1 创建任务

1. Skill 从用户请求提取标题和已提供的可选信息。
2. 调用 `create_task`；服务端检查索引和重复候选。
3. 若有多个候选，Skill 展示标题/状态并询问是复用还是新建。
4. 服务端创建任务详情、索引行；返回路径和任务 ID。
5. Skill 只在完整成功时确认任务已创建；部分失败时准确说明并保留错误信息供重试。

### 6.2 更新任务/记录工作

1. Skill 先以 `query_tasks` 匹配请求；唯一匹配后调用 `update_task`，不唯一则让用户选择。
2. 服务端验证 ID 和目标笔记后生成事件并更新任务状态、详情历史和日记引用。
3. 不关联任务的成果或计划使用 `record_work` 写入当日日志。
4. 更新操作失败时返回各文件的完成状态；重试沿用同一 `requestId`，由此保证事件幂等。

### 6.3 生成总结

1. Skill 将“今天”“本周”“今年”等转换为 `granularity`；MCP 服务按 `OBSIDIAN_TIMEZONE` 计算日期范围，周按当地日历的周日至周六。
2. 调用 `get_work_summary` 获取筛选后的事实、当前事项及来源。
3. Skill 按“成果、进行中/阻塞、下一步”组织内容，区分历史事实与当前状态。
4. 如果用户明确要求保存，再调用 `save_work_summary`；目标存在时提示冲突，并询问是否使用同一目录下的新文件名另存。保存工具不覆盖已有笔记。

### 6.4 同步任务标题

1. 用户明确要求“同步任务信息”或调用 `sync_tasks` 时，服务端逐条校验所有详情笔记并读取 H1。
2. 全部校验通过后，更新总索引并重绘两个状态视图；若校验失败，不修改任何索引页面。
3. 返回标题变更数和更新路径；不把标题同步误记为任务进展。

## 7. 日期与状态规则

- 服务端启动时从 `OBSIDIAN_TIMEZONE` 读取 IANA 时区，并使用 `time.LoadLocation` 校验；未配置使用 `Asia/Shanghai`。加载失败时服务启动报错，不退回本机时区。`mcp-config.json` 通过该 MCP server 配置的 `env` 字段向 stdio 进程传递变量。
- 服务端本地日期、日志路径、记录时间戳和总结周期边界统一使用该配置时区。周范围由请求日期映射到当地周日 00:00 至下周日 00:00 前一日；使用日历日期计算，不按固定 7×24 小时推算。
- 日、月、年使用公历边界。自定义范围为包含 `from` 和 `to` 两端的日期集合。
- 全部工作期间从索引 frontmatter 的 `started` 日期起；索引创建时设置该日期。索引不存在或缺少合法 `started` 日期时，要求先初始化记录体系，不猜测起点。总结明确数据覆盖起止日期。
- 完成事件以完成日期归属；未完成事项按当前状态放入当前事项区域，不计入期间完成。
- 已取消任务保留历史；默认当前事项列表不展示，除非用户明确查询或在历史期间发生取消事件。
- 缺少任何记录的日期段应返回明确的空结果和数据覆盖说明。

## 8. 一致性、错误与并发

### 8.1 多笔记写入

任务更新需要写任务索引、任务详情和日记三处；文件系统无跨文件事务，因此按下列顺序处理：

1. 读取并校验目标现状，构造包含同一 `requestId`（event ID）的新内容。
2. 先写任务详情历史，再更新索引当前状态，最后写入日记事件引用。
3. 每次写入均记录结果。发生失败时返回 MCP 错误及已完成写入的路径/操作上下文。
4. 对已写入的 `requestId` 做检查，重试只补写缺失副本，不重复追加。

创建任务采用先创建详情再新增索引行；若索引写入失败，工具返回孤立详情路径和失败状态。后续重试以相同 task ID 完成索引写入；若无法安全恢复则报告人工处理，不自动删除文件。

首次 `record_work` 还可能同时创建任务索引和当日日志；若其中一步失败，返回已完成路径及相同 `requestId` 下可重试的步骤。

### 8.2 防止覆盖与并发冲突

- 所有路径由 MCP 配置和已校验的 task ID/date 生成，不接受任意绝对路径；继续通过现有路径过滤和 Vault 路径解析。
- 写入前重新读取目标内容，确认目标 ID、事件 ID 和版本未变化；索引目标 ID 必须唯一。
- 在 MCP 进程内对相同索引/笔记路径串行化读改写，避免并发 MCP 请求丢失更新。
- 对 Obsidian 等外部进程同时编辑的情况，若写入前内容与本次读取快照不同则中止并返回冲突；不覆盖外部修改。跨进程的最终检查与写入之间仍有竞态窗口，首版不声称提供跨进程事务；应避免同时人工编辑同一条目。
- 单文件替换先写入同目录临时文件并校验，再以目标平台支持的安全替换方式提交；不得先删除原文件再重命名。若平台无法安全替换则返回错误并保留原文件。
- 新建目标默认要求不存在。不得调用当前 `write` 的无条件 overwrite 模式写入任务/日志/总结。
- 修复或绕开底层 append 将任意读取错误视为文件不存在的行为；只有明确 `fs.ErrNotExist` 时才走新建路径。

### 8.3 错误分类

MCP 工具对调用者可见地返回参数校验错误、索引/笔记格式错误、目标不存在、匹配冲突、路径访问拒绝、读写错误、时区配置错误及部分写入错误。内部错误可附简洁上下文，但不得把访问拒绝或解析失败当作空结果。

## 9. 安全与隐私

- 只允许在配置的索引、任务、日志和总结路径范围内访问；Worklog服务验证相对 Vault 路径、拒绝越界符号链接，且不接受任意文件路径参数。
- 工具的用户输入是文本和业务字段，不解释为任意文件路径、正则表达式或文件操作；`alternateName` 仅接受一个经过校验的文件名，并固定写入已配置的总结目录。
- 日志/总结内容仅在用户当前配置的 Obsidian MCP Vault 内处理，不外传。
- 任务删除和批量迁移不在本工具范围内；保存总结只允许新建，不覆盖既有文件。
- 返回模型的事实保留必要来源，但不返回无关笔记正文或私人 Vault 的目录清单。

## 10. 测试设计

### 10.1 Go 单元测试

- 索引解析与渲染：空索引、合法行、转义管道符、重复 ID、无效状态、错列数、保留索引中的其他正文。
- 任务生命周期：创建、状态更新、完成日期、取消、详情 ID 不匹配、重复候选。
- 日志：追加独立记录、任务事件引用、未知章节保留、损坏日志拒写、event ID 幂等。
- 日期：默认 `Asia/Shanghai` 及 `mcp-config.json` 配置其他时区的日/Weekly/Monthly/年边界、日志路径和时间戳、周跨年、闰日、自定义闭区间、无效范围及无效时区配置。
- 总结：只选范围内日志，按引用读取任务事件，当前任务与历史完成分开，事件去重、来源完整、结果上限可见。
- 写入健壮性：权限错误与文件不存在区别、部分写入、重试恢复、并发同索引更新及外部内容变化冲突。

### 10.2 MCP handler 集成测试

使用临时 Vault 验证工具输入 schema、结果字段、错误 `CallToolResult.IsError`、路径约束及各工具完整调用链。测试不得访问用户真实 Vault。

### 10.3 Skill 场景验收

至少覆盖：

1. 创建一个缺少截止日期的任务，确认不追问可选字段。
2. 相似任务不唯一时先询问，不重复创建。
3. 更新任务进度并完成，检查状态和历史。
4. 记录不关联任务的当天工作。
5. 生成本周、年度、全期间总结，检查周边界、来源及当前事项分类。
6. MCP 部分失败时准确反馈，不声称成功。

## 11. 实施状态

已在项目根目录实现 `internal/worklog`、MCP handlers 和 Worklog Skill。当前提供七个工具：`query_tasks`、`create_task`、`update_task`、`record_work`、`get_work_summary`、`save_work_summary`、`sync_tasks`。写入按配置时区生成日期；创建/更新重试用 request UUID 去重；任务索引和总结目标通过快照检查避免静默覆盖；总结结果有 500 条事实和 50 条当前任务的上限。

Go 单元测试和 MCP handler 测试使用临时 Vault；在项目根目录运行 `go test ./...`、`go vet ./...` 和 `go build ./...` 验证。实际部署仍需由用户在 MCP 宿主配置中设置时区，并确认目标 Vault 中不存在需要保留的同路径笔记。

## 12. 设计约束与暂缓项

- 目前 MCP 服务仅通过 stdio 提供工具；Skill 不直接连接服务。若未来确有非 MCP 自动化客户端需求，应另行设计受控传输和认证，不在本项目中增加隐含端点。
- 不引入数据库、内存索引缓存或新依赖；首版每次查询都从本地日志和索引重算事实。已保存的周期总结仅供人在 Obsidian 中阅读，不作为当前查询缓存。
- 若全期间查询的本地读取成本成为瓶颈，再评估可验证的增量汇总；不能为了省本地读取而返回过期总结。
- Vault 的既有组织方式和启动参数兼容细节需在实施前核对；未经确认不迁移现有笔记。
