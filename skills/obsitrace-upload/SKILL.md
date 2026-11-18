---
name: obsitrace-upload
description: Push a local Markdown document into the Obsidian vault through obsitrace-mcp without sending its body through the model, using the mcp-gateway staging route. Use when the user says push/import/write this file into the vault.
---

# Pushing a document into the vault (through mcp-gateway)

`write` normally takes the note body inline, which spends one token per character. For a real document,
stage the file through the gateway and pass the returned id; only the id crosses the model context.

## Two steps

```bash
# run from the root of your mcp-gateway clone
python3 skills/mcp-upload/scripts/mcp_upload.py --server obsitrace --path ./report.md
# 9f1c0b7a4e2d4c3b8a5f6e7d0c1b2a39
```

```js
await tools.gateway.call_tool({
  server: "obsitrace",
  tool: "write",
  arguments: {
    path: "Inbox/report.md",              // vault-relative
    contentUploadId: "9f1c0b7a4e2d4c3b8a5f6e7d0c1b2a39",
    frontmatter: { title: "report" }      // optional, small, stays inline
  }
})
```

Then read it back through the same channel (`read` with the path) to confirm.

## Rules

- Stage only UTF-8 text. `write` replaces the whole note body; frontmatter is separate.
- The id is single-use and expires (server default `--upload-ttl`, 15 minutes). A failed `write`
  keeps the id so you can retry; a successful one consumes it.
- `--server` must be `obsitrace`; the gateway must allow its `write` tool
  (`upload_requires_any`), otherwise the upload answers 403 without contacting the server.
- Size limit is the server's `--max-upload` (10 MiB by default).
- Do not base64 a document into `content`, and do not read the vault wholesale to emulate a push.
- In stdio/direct mode there is no `/upload` route: use inline `content` there.
