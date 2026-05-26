# GoAgent

Minimal Go-based quote daemon for ChatGPT Actions experiments.

Built from a tractor in a field, which feels important architecturally.

## Features

- Unix `fortune` integration
- API key authentication
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
sudo apt install golang fortune-mod cloudflared
```

## Run

```bash
export GOAGENT_API_KEY="replace-me"

go run ./cmd/goagent
```

Expected output:

```text
GoAgent listening on :8080
```

## Endpoints

### Health check

```bash
curl http://127.0.0.1:8080/health
```

### Short quote

```bash
curl \
  -H "X-API-Key: replace-me" \
  'http://127.0.0.1:8080/quote?length=short'
```

### Long quote

```bash
curl \
  -H "X-API-Key: replace-me" \
  'http://127.0.0.1:8080/quote?length=long'
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

## Cloudflare Tunnel

Quick temporary tunnel:

```bash
cloudflared tunnel --url http://127.0.0.1:8080
```

Cloudflare will provide a public HTTPS endpoint.

Example:

```text
https://something-random.trycloudflare.com
```

This avoids:

- router port forwarding
- exposing your server IP
- firewall misery
- summoning networking demons from 2003

## ChatGPT Actions

Recommended configuration:

### Authentication

- Type: API Key
- Location: Header
- Header name: `X-API-Key`

### Example endpoint

```text
GET /quote?length=short
```

## Build binary

```bash
go build -o goagent ./cmd/goagent
```

Run:

```bash
./goagent
```

## Future ideas

- OpenAPI schema
- systemd service
- rate limiting
- structured logging
- multiple quote providers
- MCP integration
- accidentally inventing distributed AI middleware

## Notes

This project exists to explore:

- ChatGPT Actions
- external AI tools
- Go microservices
- Cloudflare tunnels
- lightweight AI-agent architecture

while sitting on agricultural equipment.
