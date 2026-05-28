# GoAgent

Minimal Go-based local agent for ChatGPT integration experiments.

Built from a tractor in a field, which feels important architecturally.

GoAgent runs a small HTTP service on your machine, protects provider endpoints with an `X-API-Key` header, and can expose itself through Cloudflare Tunnel so ChatGPT can call it safely without opening inbound firewall ports.

## Features

- Local HTTP listener
- API key authentication
- Persistent config under `~/.GoAgent/`
- Stored API keys and Cloudflare tunnel tokens
- Auto-download and cache of `cloudflared`
- Fortune provider: `/fortune`
- Optional shell provider: `/shell/<name>`
- OpenAPI schema generation for ChatGPT Actions: `GoAgent show schema`
- Designed for ChatGPT Actions and Skill-guided workflows
- Tiny and gloriously boring, which is often where reliability hides

## Architecture

```text
ChatGPT
  -> Custom GPT Action
  -> HTTPS Cloudflare Tunnel
  -> GoAgent on localhost
  -> provider endpoint
```

Default local listener:

```text
127.0.0.1:8080
```

Cloudflare Tunnel exposes the local listener over HTTPS without requiring public inbound ports.

## Requirements

Debian/Ubuntu:

```bash
sudo apt install golang fortune-mod
```

macOS with Homebrew:

```bash
brew install fortune
```

Build from source:

```bash
go build -o GoAgent ./cmd/goagent
```

The examples below assume `GoAgent` is on your `PATH`. If not, use `./GoAgent`.

## Quick start

Generate an API key and capture it for this shell session:

```bash
export GOAGENT_API_KEY="$(GoAgent key create | awk '/X-API-Key:/ {print $2}')"
```

Start the daemon:

```bash
GoAgent serve
```

In another terminal, reuse the same key value or export it there too. Then check health:

```bash
curl http://127.0.0.1:8080/health
```

Call the fortune provider:

```bash
curl \
  -H "X-API-Key: ${GOAGENT_API_KEY}" \
  'http://127.0.0.1:8080/fortune?length=short'
```

Do not use literal placeholder text such as `<generated-key>` in the header. GoAgent will correctly return `forbidden`, because placeholders are not credentials, however convincing their little angle brackets may look.

## State directory

GoAgent stores state under:

```text
~/.GoAgent/
```

Typical layout:

```text
~/.GoAgent/
├── config.json
├── cache/
│   └── cloudflared
├── keys/
│   ├── GoAgent-default.key
│   └── token-default.token
└── providers/
    └── shell/
        └── config.json
```

## CLI commands

```text
GoAgent help
GoAgent serve
GoAgent key create [name]
GoAgent key ls
GoAgent key rm <name>
GoAgent token add [name] <token>
GoAgent token ls
GoAgent token rm <name>
GoAgent config show
GoAgent config set <section.key> <value>
GoAgent config reset
GoAgent show schema [server-url]
```

`GoAgent` with no arguments prints help.

`GoAgent serve` starts the daemon. Runtime options such as listen address and Cloudflare tunnel behaviour are read from config only.

## GPT Action schema

Generate an OpenAPI YAML schema suitable for a Custom GPT Action:

```bash
GoAgent show schema
```

By default, the generated schema uses the configured local listener as the server URL, for example `http://127.0.0.1:8080`.

When using Cloudflare Tunnel, pass the public tunnel URL:

```bash
GoAgent show schema https://example.trycloudflare.com
```

If the URL has no scheme, GoAgent assumes `https://`:

```bash
GoAgent show schema example.trycloudflare.com
```

The schema includes:

- `/health`
- `/fortune`
- `/fortune/config`
- any configured `/shell/<name>` endpoints from `~/.GoAgent/providers/shell/config.json`
- `X-API-Key` header authentication

For shell endpoints, any configured argument beginning with `$` becomes a required query parameter in the generated schema. This keeps the schema lined up with provider config, instead of maintaining the same thing twice, which is how small dragons hatch.

## GoAgent config

Show current config:

```bash
GoAgent config show
```

Reset config to defaults:

```bash
GoAgent config reset
```

Set the local listen address:

```bash
GoAgent config set listener.address 127.0.0.1:8080
```

Set the default API key name:

```bash
GoAgent config set listener.default_api_key default
```

Set the default fortune quote length:

```bash
GoAgent config set listener.default_quote_length short
```

Valid quote lengths:

```text
short
long
```

Set directories:

```bash
GoAgent config set global.cache_dir ~/.GoAgent/cache
GoAgent config set global.key_dir ~/.GoAgent/keys
GoAgent config set global.provider_base_dir ~/.GoAgent/providers
```

Set shutdown timeout:

```bash
GoAgent config set global.shutdown_timeout_seconds 5
```

### Example config

```json
{
  "global": {
    "cache_dir": "/home/mick/.GoAgent/cache",
    "key_dir": "/home/mick/.GoAgent/keys",
    "provider_base_dir": "/home/mick/.GoAgent/providers",
    "shutdown_timeout_seconds": 5
  },
  "listener": {
    "address": "127.0.0.1:8080",
    "default_api_key": "default",
    "default_quote_length": "short"
  }
}
```
