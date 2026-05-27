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
```

`GoAgent` with no arguments prints help.

`GoAgent serve` starts the daemon. Runtime options such as listen address and Cloudflare tunnel behaviour are read from config only.

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
GoAgent config set listener.listen_addr 127.0.0.1:8080
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
    "listen_addr": "127.0.0.1:8080",
    "default_api_key": "default",
    "default_quote_length": "short"
  },
  "cloudflare": {
    "default_token": "default",
    "enabled": false,
    "mode": "auto",
    "log_level": "info"
  }
}
```

Cloudflare config keys accepted by `GoAgent config set`:

```text
cloudflare.default_token
cloudflare.tunnel_enabled
cloudflare.tunnel_mode
cloudflare.cloudflared_log_level
```

Valid Cloudflare tunnel modes:

```text
auto
temporary
authenticated
disabled
```

## API keys

Create the default API key:

```bash
GoAgent key create
```

Create a named API key:

```bash
GoAgent key create workbench
```

List API key names:

```bash
GoAgent key ls
```

Remove an API key:

```bash
GoAgent key rm workbench
```

Use a named key by setting the default key name:

```bash
GoAgent config set listener.default_api_key workbench
```

For quick tests, use the key printed by `GoAgent key create` as `GOAGENT_API_KEY`:

```bash
export GOAGENT_API_KEY='paste-the-generated-value-here'
GoAgent serve
```

`GOAGENT_API_KEY` overrides the stored key file. If curl or ChatGPT gets `forbidden`, check that the header value matches the key used by the running daemon.

## Cloudflare Tunnel

GoAgent can run `cloudflared` for you. It auto-detects your OS and architecture, downloads the matching official `cloudflared` binary, and caches it under:

```text
~/.GoAgent/cache/
```

Supported platforms:

| OS | Architectures |
|---|---|
| Linux | amd64, arm64, arm, 386 |
| macOS | amd64, arm64 |
| Windows | amd64, 386 |

### Temporary tunnel mode

Temporary mode does not require a Cloudflare account token. It creates a random `trycloudflare.com` URL each time.

```bash
GoAgent config set cloudflare.tunnel_enabled true
GoAgent config set cloudflare.tunnel_mode temporary
GoAgent serve
```

Expected logs include something like:

```text
GoAgent listening on 127.0.0.1:8080
starting temporary Cloudflare tunnel
cloudflared started with pid 12345
Cloudflare tunnel URL: https://example.trycloudflare.com
```

Use the logged `Cloudflare tunnel URL` as the server URL in your ChatGPT Action.

### Authenticated tunnel mode

Authenticated mode uses a Cloudflare tunnel token.

High-level Cloudflare setup:

1. In Cloudflare Zero Trust, create a Tunnel.
2. Choose the `cloudflared` connector option.
3. Copy the generated tunnel token.
4. Store that token in GoAgent.
5. Enable Cloudflare tunnel mode in GoAgent config.

Store the default token:

```bash
GoAgent token add default 'paste-cloudflare-tunnel-token-here'
```

Or store a named token:

```bash
GoAgent token add workshop 'paste-cloudflare-tunnel-token-here'
GoAgent config set cloudflare.default_token workshop
```

Enable authenticated tunnel mode:

```bash
GoAgent config set cloudflare.tunnel_enabled true
GoAgent config set cloudflare.tunnel_mode authenticated
GoAgent config set cloudflare.cloudflared_log_level info
GoAgent serve
```

List stored Cloudflare token names:

```bash
GoAgent token ls
```

Remove a stored token:

```bash
GoAgent token rm workshop
```

### Auto mode

Auto mode uses a stored token if one exists. If no token is available, it falls back to a temporary tunnel.

```bash
GoAgent config set cloudflare.tunnel_enabled true
GoAgent config set cloudflare.tunnel_mode auto
GoAgent serve
```

Disable tunnel startup:

```bash
GoAgent config set cloudflare.tunnel_enabled false
```

or:

```bash
GoAgent config set cloudflare.tunnel_mode disabled
```

## Endpoints

### Health check

```bash
curl http://127.0.0.1:8080/health
```

Example response:

```json
{
  "quote": "ok"
}
```

### Fortune quote

Use configured default length:

```bash
curl \
  -H "X-API-Key: ${GOAGENT_API_KEY}" \
  'http://127.0.0.1:8080/fortune'
```

Short quote:

```bash
curl \
  -H "X-API-Key: ${GOAGENT_API_KEY}" \
  'http://127.0.0.1:8080/fortune?length=short'
```

Long quote:

```bash
curl \
  -H "X-API-Key: ${GOAGENT_API_KEY}" \
  'http://127.0.0.1:8080/fortune?length=long'
```

Example response:

```json
{
  "endpoint": "/fortune",
  "marker": "GOAGENT_FORTUNE_ENDPOINT_REACHED",
  "quote": "A witty Unix fortune appears here.",
  "default_length": "short"
}
```

### Fortune runtime config

The fortune provider exposes an in-memory runtime config endpoint for the default quote length.

Get current fortune config:

```bash
curl \
  -H "X-API-Key: ${GOAGENT_API_KEY}" \
  'http://127.0.0.1:8080/fortune/config'
```

Temporarily change default length to long:

```bash
curl \
  -X POST \
  -H 'Content-Type: application/json' \
  -H "X-API-Key: ${GOAGENT_API_KEY}" \
  -d '{"default_length":"long"}' \
  'http://127.0.0.1:8080/fortune/config'
```

This runtime endpoint affects the running process only. Persistent defaults belong in GoAgent config:

```bash
GoAgent config set listener.default_quote_length long
```

## Shell provider

The shell provider exposes configured shell commands as HTTP endpoints under:

```text
/shell/<name>
```

Config file:

```text
~/.GoAgent/providers/shell/config.json
```

Example shell provider config:

```json
{
  "endpoints": {
    "disk-free": {
      "command": "/bin/df",
      "args": ["-h"]
    },
    "list-job": {
      "command": "/bin/ls",
      "args": ["-la", "$path"],
      "chroot": "/tmp"
    }
  }
}
```

Call an endpoint:

```bash
curl \
  -H "X-API-Key: ${GOAGENT_API_KEY}" \
  'http://127.0.0.1:8080/shell/disk-free'
```

Query parameters can fill arguments that start with `$`:

```bash
curl \
  -H "X-API-Key: ${GOAGENT_API_KEY}" \
  'http://127.0.0.1:8080/shell/list-job?path=.'
```

Security notes:

- Commands must be absolute paths or start with `~/`.
- Query-filled arguments are passed as process arguments, not shell-expanded strings.
- Keep exposed commands boring and narrow.
- Do not expose broad shells like `/bin/sh` unless you are deliberately summoning demons and have read the small print.

## Integrating with ChatGPT

There are two useful layers:

1. A Custom GPT Action gives ChatGPT an HTTPS API it can call.
2. A ChatGPT Skill gives ChatGPT reusable instructions for when and how to use that API.

The Action is the network plumbing. The Skill is the operating manual.

### Custom GPT Action setup

1. Start GoAgent locally:

   ```bash
   GoAgent serve
   ```

2. Enable Cloudflare tunnel mode and copy the public HTTPS URL from the `Cloudflare tunnel URL:` log line.

3. In your Custom GPT, add an Action.

4. Configure authentication:

   ```text
   Type: API Key
   Location: Header
   Header name: X-API-Key
   Value: the actual generated GoAgent API key
   ```

5. Use an OpenAPI schema like this, replacing the server URL with your Cloudflare URL:

```yaml
openapi: 3.1.0
info:
  title: GoAgent
  version: 0.1.0
servers:
  - url: https://your-cloudflare-hostname.example.com
components:
  securitySchemes:
    GoAgentApiKey:
      type: apiKey
      in: header
      name: X-API-Key
security:
  - GoAgentApiKey: []
paths:
  /health:
    get:
      operationId: getGoAgentHealth
      summary: Check GoAgent health
      responses:
        '200':
          description: Health response
          content:
            application/json:
              schema:
                type: object
  /fortune:
    get:
      operationId: getFortune
      summary: Get a Unix fortune quote
      parameters:
        - name: length
          in: query
          required: false
          schema:
            type: string
            enum: [short, long]
      responses:
        '200':
          description: Fortune response
          content:
            application/json:
              schema:
                type: object
                properties:
                  endpoint:
                    type: string
                  marker:
                    type: string
                  quote:
                    type: string
                  default_length:
                    type: string
  /fortune/config:
    get:
      operationId: getFortuneConfig
      summary: Get runtime fortune config
      responses:
        '200':
          description: Fortune config response
          content:
            application/json:
              schema:
                type: object
    post:
      operationId: setFortuneConfig
      summary: Set runtime fortune config
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              properties:
                default_length:
                  type: string
                  enum: [short, long]
              required: [default_length]
      responses:
        '200':
          description: Updated fortune config
          content:
            application/json:
              schema:
                type: object
```

For shell provider endpoints, add explicit OpenAPI paths only for the commands you actually want ChatGPT to call. Avoid exposing a generic arbitrary shell endpoint. The universe has enough trapdoors.

### Suggested Custom GPT instructions

```text
You can use the GoAgent action for local helper tasks exposed by the user's GoAgent daemon.

When the user asks for a fortune quote, call getFortune. Use length=short unless the user asks for a long quote.

When GoAgent returns a marker field, include it verbatim in your response so the user can confirm the endpoint was reached.

Do not invent shell endpoints. Only call shell endpoints that are explicitly present in the OpenAPI schema.
```

## Integrating with ChatGPT Skills

Skills are small reusable instruction bundles for ChatGPT. They do not, by themselves, grant network access to your GoAgent daemon. Use a Skill to teach ChatGPT how to use the GoAgent Action once the Action exists.

A minimal Skill layout:

```text
goagent-helper/
├── SKILL.md
└── agents/
    └── openai.yaml
```

Example `SKILL.md`:

```markdown
---
name: goagent-helper
description: Use this skill when the user asks ChatGPT to use Mick's local GoAgent service for fortune quotes, configured local helper commands, or endpoint verification. This skill assumes a Custom GPT Action named GoAgent is already configured with X-API-Key authentication.
---

# GoAgent Helper

Use the GoAgent Action when the user asks for local GoAgent capabilities.

## Fortune quotes

- For a short fortune, call `getFortune` with `length=short`.
- For a long fortune, call `getFortune` with `length=long`.
- If the user does not specify a length, call `getFortune` without `length` or use `short`.
- Include any returned `marker` value in the final answer.

## Health checks

Call `getGoAgentHealth` when the user asks whether GoAgent is alive.

## Runtime fortune config

- Call `getFortuneConfig` to inspect the current runtime default.
- Call `setFortuneConfig` only when the user explicitly asks to change the running default quote length.
- Remind the user that persistent defaults should be changed with `GoAgent config set listener.default_quote_length <short|long>`.

## Shell endpoints

Only call shell endpoints that are explicitly documented in the Custom GPT Action schema. Never invent endpoint names or command arguments.
```

Example `agents/openai.yaml`:

```yaml
interface:
  display_name: GoAgent Helper
  short_description: Use Mick's local GoAgent service through configured ChatGPT Actions.
  icon: terminal
  brand_color: '#111827'
```

Zip the skill directory and upload it to ChatGPT Skills. In ChatGPT, Skills are available from the Skill library at `/skills`.

### Example Skill-driven prompts

```text
Use GoAgent to get me a short fortune quote.
```

```text
Check whether my local GoAgent is alive.
```

```text
Use GoAgent to switch fortune quotes to long mode for this running session.
```

```text
Use the GoAgent disk-free endpoint and summarize the result.
```

## Troubleshooting

### `forbidden` from curl or ChatGPT

Check that you are sending the real generated key value, not placeholder text:

```bash
curl \
  -H "X-API-Key: ${GOAGENT_API_KEY}" \
  'http://127.0.0.1:8080/fortune?length=short'
```

If you are using a named key, make sure the running daemon and your request use the same key source:

```bash
GoAgent key ls
GoAgent config set listener.default_api_key workbench
```

Also check whether `GOAGENT_API_KEY` is overriding your stored key:

```bash
unset GOAGENT_API_KEY
```

### `GOAGENT_API_KEY not set and ... not found`

Create an API key:

```bash
GoAgent key create
```

or set the matching key name:

```bash
GoAgent key ls
GoAgent config set listener.default_api_key default
```

### Cloudflare authenticated tunnel cannot find token

Check stored tokens:

```bash
GoAgent token ls
```

Set the configured default token name:

```bash
GoAgent config set cloudflare.default_token default
```

### Cloudflare URL changed

Temporary tunnel URLs change between runs. Update the Custom GPT Action server URL after restarting GoAgent in temporary tunnel mode.

For stable URLs, use an authenticated Cloudflare tunnel.

## Future ideas

- Stable release packaging
- systemd service
- richer provider registry
- OpenAPI schema generation
- stricter shell endpoint allow-lists
- structured logging
- MCP integration
- accidentally inventing distributed AI middleware while looking for a quote

## Notes

This project exists to explore:

- ChatGPT Actions
- ChatGPT Skills
- external AI tools
- local Go microservices
- Cloudflare tunnels
- lightweight AI-agent architecture

while sitting on agricultural equipment.
