# GoAgent

Minimal Go-based quote daemon for ChatGPT Actions experiments.

Built from a tractor in a field, which feels important architecturally.

## Features

- Unix `fortune` integration
- API key authentication
- Runtime config endpoint
- Configurable default quote mode
- Endpoint marker strings for ChatGPT testing
- Auto-download Cloudflare Tunnel support
- Automatic OS/architecture detection
- Local-only listener
- Designed for Cloudflare Tunnel exposure
- Tiny and gloriously boring

## Architecture

```text
ChatGPT Action
    ↓ HTTPS
Cloudflare Tunnel
    ↓
GoAgent on localhost:8080
    ↓
fortune
```

The service intentionally only listens locally:

```text
127.0.0.1:8080
```

Cloudflare Tunnel exposes it safely without opening inbound ports.

## Requirements

Debian/Ubuntu:

```bash
sudo apt install golang fortune-mod
```

No manual cloudflared installation is required when using `--tunnel`.

## Run

### Standard mode

```bash
export GOAGENT_API_KEY="replace-me"

go run ./cmd/goagent
```

### Tunnel mode

Automatically:

- detects OS/architecture
- downloads official `cloudflared`
- caches it locally
- launches the tunnel

```bash
export GOAGENT_API_KEY="replace-me"

go run ./cmd/goagent --tunnel
```

Expected output:

```text
GoAgent listening on 127.0.0.1:8080
cloudflared started with pid 12345
```

Cloudflare will then emit a public HTTPS endpoint.

Example:

```text
https://something-random.trycloudflare.com
```

## cloudflared cache location

GoAgent caches downloaded binaries in:

### Linux/macOS

```text
~/.cache/goagent/
```

### Windows

```text
%LocalAppData%\goagent\
```

This avoids repeatedly downloading binaries like a caffeinated package manager.

## Supported platforms

| OS | Architectures |
|---|---|
| Linux | amd64, arm64, arm, 386 |
| macOS | amd64, arm64 |
| Windows | amd64, 386 |

## Endpoint markers

These markers exist purely to verify that ChatGPT actually called a specific endpoint.

| Endpoint | Marker |
|---|---|
| `GET /quote` | `GOAGENT_QUOTE_ENDPOINT_REACHED` |
| `GET /config` | `GOAGENT_CONFIG_GET_ENDPOINT_REACHED` |
| `POST /config` | `GOAGENT_CONFIG_POST_ENDPOINT_REACHED` |

Recommended Custom GPT instruction:

```text
When GoAgent returns a marker field, include it verbatim in your response.
```

## Endpoints

### Health check

```bash
curl http://127.0.0.1:8080/health
```

### Quote endpoint

If `length` is omitted, the daemon uses the configured default.

#### Short quote

```bash
curl \
  -H "X-API-Key: replace-me" \
  'http://127.0.0.1:8080/quote?length=short'
```

#### Long quote

```bash
curl \
  -H "X-API-Key: replace-me" \
  'http://127.0.0.1:8080/quote?length=long'
```

#### Use configured default

```bash
curl \
  -H "X-API-Key: replace-me" \
  'http://127.0.0.1:8080/quote'
```

Example response:

```json
{
  "endpoint": "quote",
  "marker": "GOAGENT_QUOTE_ENDPOINT_REACHED",
  "quote": "A witty Unix fortune appears here.",
  "default_length": "short"
}
```

## Config endpoint

The config endpoint changes the daemon's runtime default quote mode.

Current default is stored in memory only.

This means:

- changing config is immediate
- restarting GoAgent resets it to `short`
- no config file exists yet
- no database exists yet
- peace still reigns in the kingdom

### Get current config

```bash
curl \
  -H "X-API-Key: replace-me" \
  'http://127.0.0.1:8080/config'
```

Example response:

```json
{
  "endpoint": "config",
  "marker": "GOAGENT_CONFIG_GET_ENDPOINT_REACHED",
  "default_length": "short"
}
```

### Change default to long

```bash
curl \
  -X POST \
  -H "Content-Type: application/json" \
  -H "X-API-Key: replace-me" \
  -d '{"default_length":"long"}' \
  'http://127.0.0.1:8080/config'
```

Example response:

```json
{
  "endpoint": "config",
  "marker": "GOAGENT_CONFIG_POST_ENDPOINT_REACHED",
  "default_length": "long"
}
```

### Change default to short

```bash
curl \
  -X POST \
  -H "Content-Type: application/json" \
  -H "X-API-Key: replace-me" \
  -d '{"default_length":"short"}' \
  'http://127.0.0.1:8080/config'
```

## Authentication

Authentication uses a static API key header:

```text
X-API-Key
```

The daemon validates this header before serving quotes.

Environment variable:

```bash
export GOAGENT_API_KEY="replace-me"
```

## ChatGPT Actions

Recommended configuration:

### Authentication

- Type: API Key
- Location: Header
- Header name: `X-API-Key`

### Example endpoints

```text
GET  /quote
GET  /quote?length=short
GET  /quote?length=long
GET  /config
POST /config
```

ChatGPT can:

- request quotes
- read current config
- change default quote behaviour
- verify which endpoint was called

without directly accessing your server.

## Build binary

```bash
go build -o goagent ./cmd/goagent
```

Run:

```bash
./goagent --tunnel
```

## Future ideas

- OpenAPI schema
- systemd service
- persistent config file
- rate limiting
- structured logging
- multiple quote providers
- MCP integration
- embedded cloudflared binary
- accidentally inventing distributed AI middleware

## Notes

This project exists to explore:

- ChatGPT Actions
- external AI tools
- Go microservices
- Cloudflare tunnels
- lightweight AI-agent architecture

while sitting on agricultural equipment.
