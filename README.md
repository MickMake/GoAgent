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
- Explicit `cloudflared` cache update command
- Fortune provider: `/fortune`
- Configurable shell provider: `/shell/<name>`
- GPT setup output for ChatGPT configuration: `GoAgent setup`
- Public OpenAPI schema endpoint for ChatGPT Actions: `/config/schema`
- Optional protected knowledge files under `~/.GoAgent/knowledge/`
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
├── knowledge/
│   └── notes.md
└── providers/
    └── shell/
        └── config.json
```

## Providers

Provider-specific documentation:

- [Shell provider](providers/shell/README.md): configure `/shell/<name>` endpoints, metadata for `GoAgent setup`, query parameters, and chroot examples.

The shell provider is always loaded. Its config file is optional: if `~/.GoAgent/providers/shell/config.json` does not exist, GoAgent writes a small default config. Edit or remove those default endpoints to suit the local machine. As with all cupboards containing local command execution, label the handles clearly before inviting a GPT to open them.

## CLI commands

```text
GoAgent help
GoAgent serve
GoAgent setup [server-url] [privacy-url]
GoAgent key create [name]
GoAgent key ls
GoAgent key rm <name>
GoAgent token add [name] <token>
GoAgent token ls
GoAgent token rm <name>
GoAgent cloudflared update
GoAgent config show
GoAgent config set <section.key> <value>
GoAgent config reset
```

`GoAgent` with no arguments prints help.

`GoAgent serve` starts the daemon. Runtime options such as listen address and Cloudflare tunnel behaviour are read from config only.

`GoAgent cloudflared update` forces a fresh `cloudflared` download into the cache and validates it before use. This is the deliberate-update lever; otherwise GoAgent reuses a valid cached binary.

## GPT setup

Generate the full text needed to configure a Custom GPT and its Action:

```bash
GoAgent setup
```

If server and privacy URLs are not already stored in config, `setup` prompts for them. Prompt messages and save confirmations are written to stderr so stdout remains copyable setup text.

You can provide both URLs directly:

```bash
GoAgent setup https://example.trycloudflare.com https://example.com/privacy
```

When URLs are supplied, they are saved into config under:

```text
gpt.server_url
gpt.privacy_url
```

Bare hostnames are normalised to `https://` URLs.

The generated setup text includes:

- GPT name, description, and instructions
- conversation starters based on available providers
- knowledge file URLs for files in `~/.GoAgent/knowledge/`
- the configured API key value, or a placeholder if no key exists yet
- Action schema URL: `<server-url>/config/schema`
- privacy policy URL

## GPT Action schema

The daemon exposes the current OpenAPI YAML schema at:

```text
/config/schema
```

For example:

```bash
curl http://127.0.0.1:8080/config/schema
```

This endpoint does not require an API key so ChatGPT can load the schema while configuring the Action.

The schema includes:

- `/health`
- `/version`
- `/fortune`
- `/fortune/config`
- any configured `/shell/<name>` endpoints from `~/.GoAgent/providers/shell/config.json`
- `X-API-Key` header authentication for protected Action calls

For shell endpoints, any configured argument beginning with `$` becomes a required query parameter in the generated schema. This keeps the schema lined up with provider config, instead of maintaining the same thing twice, which is how small dragons hatch.

## Knowledge files

Files placed in:

```text
~/.GoAgent/knowledge/
```

are listed by `GoAgent setup` as:

```text
<server-url>/config/knowledge/<filename>
```

Knowledge file URLs require the configured `X-API-Key` header.

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

Set Cloudflare Tunnel behaviour:

```bash
GoAgent config set cloudflare.enabled true
GoAgent config set cloudflare.mode auto
GoAgent config set cloudflare.default_token default
GoAgent config set cloudflare.log_level info
GoAgent config set cloudflare.version latest
```

`cloudflare.version` can be `latest` or a Cloudflare release tag such as `2025.6.0`. When set to a specific release, GoAgent downloads that release and checks that `cloudflared --version` matches the configured value. On macOS Catalina, the default `latest` behaviour is pinned internally to `2025.6.0` for compatibility.

Cloudflare modes:

```text
auto           Use a saved token if available; otherwise create a temporary tunnel.
temporary      Always create a temporary trycloudflare tunnel.
authenticated  Require a saved Cloudflare tunnel token and run that named tunnel.
disabled       Do not start Cloudflare Tunnel.
```

Refresh the cached `cloudflared` binary:

```bash
GoAgent cloudflared update
```

Set GPT setup URLs:

```bash
GoAgent config set gpt.server_url https://example.trycloudflare.com
GoAgent config set gpt.privacy_url https://example.com/privacy
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
  },
  "cloudflare": {
    "default_token": "default",
    "enabled": false,
    "mode": "auto",
    "log_level": "info",
    "version": "latest"
  },
  "gpt": {
    "server_url": "https://example.trycloudflare.com",
    "privacy_url": "https://example.com/privacy"
  }
}
```
