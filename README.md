# GoAgent

Minimal Go-based local agent for ChatGPT integration experiments.

Built from a tractor in a field, which feels important architecturally.

GoAgent runs a small HTTP service on your machine, protects provider endpoints with an `X-API-Key` header, and can expose itself through Cloudflare Tunnel so ChatGPT can call it safely without opening inbound firewall ports. It can also run a local stdio MCP server for MCP-capable clients.

## Features

- Local HTTP listener for Custom GPT Actions
- Local stdio MCP server mode
- API key authentication for HTTP provider endpoints
- Persistent config under `~/.GoAgent/`
- Generated artifacts under `~/.GoAgent/artifacts/`
- Stored API keys and Cloudflare tunnel tokens
- Auto-download and cache of `cloudflared`
- Explicit `cloudflared` cache update command
- Fortune provider: `/fortune`
- Configurable shell provider: `/shell/<name>`
- Dynamic MCP tools for configured shell endpoints
- Optional shell response prefix field for clearer ChatGPT replies
- GPT setup/action artifact generation: `GoAgent gpt create`
- GPT configuration verification: `GoAgent gpt verify`
- Skill package generation and verification for reusable GoAgent workflows: `GoAgent skill create` and `GoAgent skill verify`
- Public OpenAPI schema endpoint for ChatGPT Actions: `/config/schema`
- Optional protected knowledge files under `~/.GoAgent/knowledge/`
- Designed for ChatGPT Actions, MCP clients, and Skill-guided workflows
- Tiny and gloriously boring, which is often where reliability hides

## Architecture

Custom GPT Action path:

```text
ChatGPT
  -> Custom GPT Action
  -> HTTPS Cloudflare Tunnel
  -> GoAgent GPT HTTP server on localhost
  -> provider endpoint
```

MCP path:

```text
MCP client
  -> stdio subprocess
  -> GoAgent MCP server
  -> shared GoAgent provider functions
```

Skills are not a transport layer. They help ChatGPT follow GoAgent conventions and reference generated setup/schema material, but they do not call GoAgent directly and cannot proxy through a Custom GPT Action. MCP exists for live local tool access outside the Custom GPT Action path.

Default local HTTP listener:

```text
127.0.0.1:8080
```

Cloudflare Tunnel exposes the local HTTP listener over HTTPS without requiring public inbound ports. MCP stdio does not use Cloudflare or HTTP.

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

Start the default server mode:

```bash
GoAgent serve
```

By default, `GoAgent serve` runs the GPT HTTP Action server only. That preserves the original behaviour, because surprises belong in Christmas crackers, not daemons.

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

- [Shell provider](providers/shell/README.md): configure `/shell/<name>` endpoints, optional response prefix, metadata for `GoAgent setup`, query parameters, and chroot examples.

The shell provider is always loaded. Its config file is optional: if `~/.GoAgent/providers/shell/config.json` does not exist, GoAgent writes a small default config. Edit or remove those default endpoints to suit the local machine. As with all cupboards containing local command execution, label the handles clearly before inviting a GPT to open them.

Shell config can include a top-level `prefix` such as `"GoAgent: "`. When present, shell endpoint responses include that value as a `prefix` field, and `GoAgent setup` adds shell-provider instructions telling the GPT to use it in final answers. Remove the field or set it to an empty string to disable the behaviour.

The same shell provider config is used by HTTP Actions and MCP. HTTP exposes shell endpoints as `/shell/<name>`. MCP exposes each configured shell endpoint as a safely generated tool name such as `goagent_shell_os_version` or `goagent_shell_upper`.

## CLI commands

```text
GoAgent help
GoAgent serve
GoAgent serve gpt
GoAgent serve mcp
GoAgent gpt create [server-url] [privacy-url]
GoAgent gpt verify
GoAgent skill create
GoAgent skill verify
GoAgent gpt config
GoAgent gpt config set <key> <value>
GoAgent gpt config reset <key>
GoAgent gpt key
GoAgent gpt key create [name]
GoAgent gpt key rm <name>
GoAgent gpt token
GoAgent gpt token create [name]
GoAgent gpt token rm <name>
GoAgent gpt cloudflared update
GoAgent mcp create
GoAgent mcp verify
GoAgent mcp config
GoAgent mcp config set <key> <value>
GoAgent mcp config reset <key>
GoAgent skill config
GoAgent skill config set <key> <value>
GoAgent skill config reset <key>
GoAgent config
GoAgent config set <section.key> <value>
GoAgent config reset
GoAgent config reset <section.key>
```

`GoAgent` with no arguments prints help.

`GoAgent serve` starts whatever server modes are enabled in config.

`GoAgent serve gpt` starts only the Custom GPT Actions HTTP server.

`GoAgent serve mcp` starts only the stdio MCP server.

`GoAgent cloudflared update` forces a fresh `cloudflared` download into the cache and validates it before use. This is the deliberate-update lever; otherwise GoAgent reuses a valid cached binary.

## Serve modes

GoAgent has two live transport interfaces:

| Command | Transport | Purpose |
| --- | --- | --- |
| `GoAgent serve gpt` | HTTP + optional Cloudflare Tunnel | Custom GPT Actions |
| `GoAgent serve mcp` | stdio JSON-RPC | Local MCP clients |
| `GoAgent serve` | Config-driven | Starts enabled modes |

Default config:

```json
"serve": {
  "gpt_enabled": true,
  "mcp_enabled": false
}
```

To make bare `GoAgent serve` run both the GPT HTTP server and MCP server:

```bash
GoAgent config set serve.gpt_enabled true
GoAgent config set serve.mcp_enabled true
GoAgent serve
```

When both are enabled, MCP owns stdout because stdio MCP requires stdout to contain protocol messages only. GoAgent logs go to stderr. The GPT HTTP server continues to listen on the configured local address in the same process.

For separate terminals or supervisors, run them explicitly:

```bash
# Terminal 1
GoAgent serve gpt

# Terminal 2, or launched by an MCP client
GoAgent serve mcp
```

## MCP mode

MCP mode is for live local access from MCP-capable clients without going through a Custom GPT Action or Cloudflare Tunnel.

Built-in MCP tools:

- `goagent_health`
- `goagent_version`
- `goagent_fortune`

Configured shell endpoints are exposed dynamically as MCP tools. Tool names are generated safely from endpoint names:

```text
/shell/os-version -> goagent_shell_os_version
/shell/upper      -> goagent_shell_upper
```

Any shell provider argument beginning with `$` becomes a required string argument in the MCP tool input schema. For example:

```json
{
  "upper": {
    "command": "/usr/bin/awk",
    "args": ["BEGIN { print toupper(ARGV[1]); exit }", "$text"],
    "description": "Uppercase supplied text using a fixed awk program."
  }
}
```

becomes an MCP tool named `goagent_shell_upper` with required argument `text`.

Shell MCP tools use the same execution path as HTTP shell endpoints: no shell interpolation, no `sh -c`, configured command path validation, argv-only user input, and existing chroot behaviour.

Start only the MCP server:

```bash
GoAgent serve mcp
```

The MCP server uses stdio transport. It reads JSON-RPC messages from stdin, writes valid MCP JSON-RPC messages to stdout, and uses stderr for logs/errors/debug output. Do not pipe casual text into it unless you enjoy watching protocol parsers raise one eyebrow.

Example local stdio MCP client config shape:

```json
{
  "mcpServers": {
    "goagent": {
      "command": "GoAgent",
      "args": ["serve", "mcp"]
    }
  }
}
```

Use the full path to the binary if your MCP client does not inherit your shell `PATH`.


## Generated artifacts

Generated integration files are written under:

```text
~/.GoAgent/artifacts/
├── gpt/
│   ├── setup.md
│   └── action-schema.yaml
├── mcp/
│   ├── client-config.json
│   └── client-config.md
└── skill/
    ├── skill-GoAgent.zip
    └── skill-GoAgent/
```

## GPT setup

Generate the full text needed to configure a Custom GPT and its Action:

```bash
GoAgent gpt create
```

If server and privacy URLs are not already stored in config, `setup` prompts for them. Prompt messages and save confirmations are written to stderr so stdout remains copyable setup text.

You can provide both URLs directly:

```bash
GoAgent gpt create https://example.trycloudflare.com https://example.com/privacy
```

When URLs are supplied, they are saved into config under:

```text
gpt.server_url
gpt.privacy_url
```

Bare hostnames are normalised to `https://` URLs.

The generated setup text includes:

- GPT name, description, and instructions
- shell-provider global instructions when present in `~/.GoAgent/providers/shell/config.json`
- conversation starters based on available providers
- knowledge file URLs for files in `~/.GoAgent/knowledge/`
- the configured API key value, or a placeholder if no key exists yet
- Action schema URL: `<server-url>/config/schema`
- privacy policy URL

## GPT verification

Check the local GoAgent pieces needed for ChatGPT Action setup:

```bash
GoAgent gpt verify
```

The verifier prints `PASS`, `WARN`, and `FAIL` checks for:

- configured server and privacy URLs
- default API key presence
- shell provider config loading
- generated Action schema sanity
- Cloudflare mode and token state
- knowledge file availability
- local `/health` reachability
- configured schema URL reachability

Warnings are advisory. Failures return a non-zero exit code. This command checks what the binary can see locally; it cannot inspect whether the ChatGPT UI has actually attached the Action, because even GoAgent has not yet learned to reach through the screen and press buttons like a tiny sysadmin raccoon.

## Skill generation

Generate a reusable ChatGPT Skill package from the current GoAgent setup:

```bash
GoAgent skill create
```

This writes and verifies:

```text
skill-GoAgent.zip
```

The generated zip contains one top-level directory:

```text
skill-GoAgent/
```

The internal Skill frontmatter name is lowercase:

```yaml
name: skill-goagent
```

The generated Skill includes:

- `SKILL.md` with GoAgent-specific instructions and trigger guidance
- `agents/openai.yaml` metadata
- `references/goagent-setup.md` copied from the current `GoAgent setup` output
- `references/action-schema.yaml` generated from current provider config
- `references/action-schema-url.md` with schema, privacy, and authentication notes
- `references/shell-endpoints.md` generated from the current shell provider config
- `references/knowledge-files.md` if knowledge files exist

Verify the existing package:

```bash
GoAgent skill verify
```

The verifier checks `skill-GoAgent.zip` for:

- zip readability and size below the 25 MB target
- one top-level `skill-GoAgent/` directory
- required files such as `SKILL.md` and `agents/openai.yaml`
- expected lowercase frontmatter name
- linked reference files
- core Action schema operations and API key auth
- unsafe zip paths such as absolute paths or `..` traversal

The Skill helps ChatGPT follow GoAgent conventions, but it does not install the Custom GPT Action, API key, MCP client, or transport wiring by itself. It is a very useful clipboard, not a licensed electrician.

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

Scoped config commands print the effective subsystem view, assembled from the shared config keys that subsystem uses:

```bash
GoAgent gpt config
GoAgent mcp config
GoAgent skill config
```

`GoAgent config` still prints the raw complete config.


Show current config:

```bash
GoAgent config
```

Reset config to defaults:

```bash
GoAgent config reset
```

Set serve modes:

```bash
GoAgent config set serve.gpt_enabled true
GoAgent config set serve.mcp_enabled false
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
GoAgent config set global.artifact_dir ~/.GoAgent/artifacts
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
    "artifact_dir": "/home/mick/.GoAgent/artifacts",
    "shutdown_timeout_seconds": 5
  },
  "serve": {
    "gpt_enabled": true,
    "mcp_enabled": false
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

Shell provider response prefixes are configured in `~/.GoAgent/providers/shell/config.json`, not in the main GoAgent config file.
