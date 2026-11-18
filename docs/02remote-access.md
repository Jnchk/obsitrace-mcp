# Remote access over Streamable HTTP

By default `obsitrace-mcp` speaks MCP over **stdio** and has no network endpoint.
Passing `--http` switches it to the MCP **Streamable HTTP** transport so a client
on another machine (or another process on the same machine) can connect.

This server is single-vault and single-credential: one bearer token grants the
same full vault access as a local stdio client. It is designed for personal or
small-team use behind TLS, not for multi-tenant hosting.

## Starting the server

```bash
# Loopback only — reachable from this machine
obsitrace-mcp ~/obsidian --http 127.0.0.1:8787

# All interfaces — requires a token (the server refuses to start without one)
obsitrace-mcp ~/obsidian --http 0.0.0.0:8787 --auth-token "$OBSITRACE_AUTH_TOKEN"

# Several addresses at once: repeat the flag or comma-separate
obsitrace-mcp ~/obsidian --http 127.0.0.1:8787,192.168.128.91:8787 --auth-token "$OBSITRACE_AUTH_TOKEN"
obsitrace-mcp ~/obsidian --http 127.0.0.1:8787 --http 192.168.128.91:8787 --auth-token "$OBSITRACE_AUTH_TOKEN"
```

| Flag | Env var | Description |
|---|---|---|
| `--http <addr>[,...]` | `OBSITRACE_HTTP_ADDR` | Serve Streamable HTTP on one or more addresses instead of stdio; repeat the flag or comma-separate them. Empty keeps stdio. |
| `--auth-token <token>` | `OBSITRACE_AUTH_TOKEN` | Bearer token every HTTP request must present. Mandatory for non-loopback addresses. |

Both flags are optional. When `--http` is absent, behavior is exactly the same as
before, including stdin/stdout MCP configuration.

Every address is validated and bound before any request is served: if one bind
fails, startup aborts and no listener is left half-running. The same token
guards all of them.

Generate a token with:

```bash
openssl rand -hex 32
```

### What the HTTP mode does

- Serves MCP at any path on that address (`/` is fine; the path is ignored).
- Runs **stateless**: every POST is a self-contained request. `GET` and `DELETE`
  return `405` — this server sends no server-initiated notifications, so no
  streaming GET is needed.
- Responds with `application/json` rather than `text/event-stream`.
- Caps request bodies at 4 MiB (SDK default). Larger `write` payloads get `413`.
- Keeps the SDK's DNS-rebinding guard when no token is configured. With a token,
  the guard is disabled because a reverse proxy legitimately forwards the public
  `Host` header.

## Client configuration

Point an HTTP-capable MCP client at the URL and send the bearer token.
Field names vary by client; the shape is:

```json
{
  "mcpServers": {
    "obsitrace": {
      "type": "http",
      "url": "http://127.0.0.1:8787/mcp",
      "headers": {
        "Authorization": "Bearer <token>"
      }
    }
  }
}
```

Some clients spell the transport `"transport": "http"` or expose it as a
`streamable-http` option; use whatever that client documents.

## nginx reverse proxy (TLS termination)

Run the server on loopback and let nginx terminate TLS. Plain HTTP must never
leave the host.

```nginx
server {
    listen 443 ssl;
    server_name vault.example.com;

    ssl_certificate     /etc/letsencrypt/live/vault.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/vault.example.com/privkey.pem;

    # SDK caps request bodies at 4 MiB; leave headroom for JSON overhead.
    client_max_body_size 8m;

    location /mcp {
        proxy_pass http://127.0.0.1:8787;
        proxy_http_version 1.1;
        proxy_set_header Connection "";
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # Streamable responses may be SSE; do not buffer or time them out early.
        proxy_buffering off;
        proxy_cache off;
        proxy_read_timeout 1h;
        proxy_send_timeout 1h;
    }
}
```

Obtain the certificate with Certbot:

```bash
sudo certbot --nginx -d vault.example.com
```

Then start the server as:

```bash
OBSITRACE_AUTH_TOKEN="$(cat /etc/obsitrace/token)" \
  obsitrace-mcp /srv/obsidian --http 127.0.0.1:8787
```

The client URL becomes `https://vault.example.com/mcp`.

Notes:

- MCP is served at `/mcp`; the bare `/` still answers for compatibility, so
  `location /mcp` works with or without a trailing slash on `proxy_pass`. Keep
  the proxy on 127.0.0.1 so the only public entry point is nginx.
- If a client insists on a streaming `GET` endpoint, run without `--http`'s
  stateless mode — that requires a code change, not an nginx one.
- Do not add `proxy_set_header Host 127.0.0.1:8787`. It is unnecessary with a
  token configured and would break virtual hosting.

## systemd unit (optional)

```ini
[Unit]
Description=obsitrace-mcp (Streamable HTTP)
After=network.target

[Service]
User=obsidian
EnvironmentFile=/etc/obsitrace/env
ExecStart=/usr/local/bin/obsitrace-mcp /srv/obsidian --http 127.0.0.1:8787
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

`/etc/obsitrace/env` must be mode `0600` and contain:

```bash
OBSITRACE_AUTH_TOKEN=<token>
```

## Security checklist

- The token is a **root credential for the vault**: read, write, rename, delete,
  and worklog mutation. Treat it like an SSH key.
- Always use TLS in front of any non-loopback bind.
- Never commit the token or pass it on the command line where shell history or
  `ps` can see it; use `OBSITRACE_AUTH_TOKEN` from an `EnvironmentFile`.
- The server refuses `--http` on a non-loopback address without a token. That
  guard is config validation, not authorization — keep the token set anyway.
- Rotate by restarting the process with a new token; sessions are stateless, so
  every client must present the new value immediately.
