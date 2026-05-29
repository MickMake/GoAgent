# GoAgent

Minimal Go-based local agent for ChatGPT integration experiments.

Built from a tractor in a field, which feels important architecturally.

GoAgent runs a small local Go binary that can expose selected local capabilities through three live integration lanes:

- **GPT Custom GPT Actions** over HTTP, optionally exposed through Cloudflare Tunnel
- **MCP stdio** for local MCP-capable clients
- **Hosted HTTP connector/tool bridge artifacts** for runtimes that register an HTTPS OpenAPI tool with API key auth

Skills are not a live transport layer. They help ChatGPT follow GoAgent conventions and reference generated setup/schema material, but they do not call GoAgent directly and cannot proxy through a Custom GPT Action. MCP exists for live local tool access outside the Custom GPT Action path.

For worked commands and copy/paste examples, see [EXAMPLES.md](EXAMPLES.md).

## Features

- Local HTTP listener for Custom GPT Actions
- Local stdio MCP server mode
- Hosted HTTP connector/tool bridge artifact generation: `GoAgent connector create`
- API key authentication for HTTP provider endpoints
- Persistent config under `~/.GoAgent/`
- Generated artifacts under `~/.GoAgent/artifacts/`
- Stored API keys and Cloudflare tunnel tokens
- Auto-download and cache of `cloudflared`
- Explicit scoped `cloudflared` cache update command
- Fortune provider: `/fortune`
- Configurable shell provider: `/shell/<name>`
- Dynamic MCP tools for configured shell endpoints
- Optional shell response prefix field for clearer ChatGPT replies
- GPT setup/action artifact generation: `GoAgent gpt create`
- GPT configuration verification: `GoAgent gpt verify`
- MCP client artifact generation: `GoAgent mcp create`
- Connector artifact generation and verification: `GoAgent connector create` and `GoAgent connector verify`
- Skill package generation and verification: `GoAgent skill create` and `GoAgent skill verify`
- Public OpenAPI schema endpoint for ChatGPT Actions and connector registration: `/config/schema`
- Optional protected knowledge files under `~/.GoAgent/knowledge/`
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

Hosted HTTP connector/tool bridge path:

```text
ChatGPT-style tool runtime
  -> HTTPS OpenAPI connector/tool registration
  -> HTTPS Cloudflare Tunnel or reverse proxy
  -> GoAgent GPT HTTP server on localhost
  -> provider endpoint
```

Cloudflare Tunnel exposes the local HTTP listener over HTTPS without requiring public inbound ports. MCP stdio does not use Cloudflare or HTTP.

## Requirements

- Go 1.22 or newer
- `fortune-mod` on Debian/Ubuntu, or `fortune` on macOS via Homebrew, if using the fortune provider

## Build

```bash
go build -o GoAgent ./cmd/goagent
```

The command examples assume `GoAgent` is on your `PATH`. If not, use `./GoAgent`.

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
├── artifacts/
│   ├── gpt/
│   ├── mcp/
│   ├── connector/
│   └── skill/
└── providers/
    └── shell/
        └── config.json
```

## Providers

Provider-specific documentation:

- [Shell provider](providers/shell/README.md): configure `/shell/<name>` endpoints, optional response prefix, metadata for `GoAgent gpt create`, query parameters, and chroot examples.

The shell provider is always loaded. Its config file is optional: if `~/.GoAgent/providers/shell/config.json` does not exist, GoAgent writes a small default config. Edit or remove those default endpoints to suit the local machine. As with all cupboards containing local command execution, label the handles clearly before inviting a GPT to open them.

The same shell provider config is used by HTTP Actions, hosted connector artifacts, and MCP. HTTP exposes shell endpoints as `/shell/<name>`. MCP exposes each configured shell endpoint as a safely generated tool name such as `goagent_shell_os_version` or `goagent_shell_upper`.

## CLI command tree

```text
GoAgent help

GoAgent serve
GoAgent serve gpt
GoAgent serve mcp

GoAgent gpt create [server-url] [privacy-url]
GoAgent gpt verify
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

GoAgent connector create
GoAgent connector verify
GoAgent connector config
GoAgent connector config set <key> <value>
GoAgent connector config reset <key>

GoAgent skill create
GoAgent skill verify
GoAgent skill config
GoAgent skill config set <key> <value>
GoAgent skill config reset <key>

GoAgent config
GoAgent config set <section.key> <value>
GoAgent config reset
GoAgent config reset <section.key>
```

`GoAgent` with no arguments prints help.

## Serve modes

GoAgent has two live server modes and one hosted HTTP artifact lane that targets the existing HTTP server:

| Command | Transport | Purpose |
| --- | --- | --- |
| `GoAgent serve gpt` | HTTP + optional Cloudflare Tunnel | Custom GPT Actions and hosted HTTP connector/tool bridge calls |
| `GoAgent serve mcp` | stdio JSON-RPC | Local MCP clients |
| `GoAgent serve` | Config-driven | Starts enabled modes |

Default config:

```json
"serve": {
  "gpt_enabled": true,
  "mcp_enabled": false
}
```

When both are enabled, MCP owns stdout because stdio MCP requires stdout to contain protocol messages only. GoAgent logs go to stderr. The GPT HTTP server continues to listen on the configured local address in the same process.

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

Any shell provider argument beginning with `$` becomes a required string argument in the MCP tool input schema. Shell MCP tools use the same execution path as HTTP shell endpoints: no shell interpolation, no `sh -c`, configured command path validation, argv-only user input, and existing chroot behaviour.

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
├── connector/
│   ├── connector.json
│   ├── connector.md
│   └── openapi.yaml
└── skill/
    ├── skill-GoAgent.zip
    └── skill-GoAgent/
```

## GPT Action setup

`GoAgent gpt create` generates the text and schema needed to configure a Custom GPT and its Action.

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

## Hosted HTTP connector bridge

`GoAgent connector create` generates artifacts for runtimes that can register an HTTPS OpenAPI tool or connector and call it with API key authentication.

This is not a new transport and it is not a replacement for MCP. It describes the existing hosted HTTPS GoAgent HTTP surface. The HTTP server is still started with `GoAgent serve gpt` or config-driven `GoAgent serve`.

Generated connector files:

```text
~/.GoAgent/artifacts/connector/
├── connector.json
├── connector.md
└── openapi.yaml
```

The connector uses:

- server URL from `gpt.server_url`, falling back to the local listener URL
- schema URL `<server-url>/config/schema`
- API key authentication using the `X-API-Key` header
- the same OpenAPI schema writer used by GPT Actions

For remote ChatGPT-style runtimes, GoAgent must be reachable at the configured server URL. Use Cloudflare Tunnel or another HTTPS route. Create the API key with:

```bash
GoAgent gpt key create
```

`GoAgent connector config` prints the effective connector view, including name, display name, transport, server URL, schema URL, auth header, artifact paths, and provider base directory.

## Verification

`GoAgent gpt verify` checks the local GoAgent pieces needed for ChatGPT Action setup:

- configured server and privacy URLs
- default API key presence
- shell provider config loading
- generated Action schema sanity
- Cloudflare mode and token state
- knowledge file availability
- local `/health` reachability
- configured schema URL reachability

`GoAgent mcp verify` checks generated MCP client artifacts.

`GoAgent connector verify` checks generated connector artifacts for:

- manifest readability and expected HTTPS/API-key shape
- guide file presence
- OpenAPI schema presence and API key auth sanity

`GoAgent skill verify` checks the generated Skill package for:

- zip readability and size below the 25 MB target
- one top-level `skill-GoAgent/` directory
- required files such as `SKILL.md` and `agents/openai.yaml`
- expected lowercase frontmatter name
- linked reference files
- core Action schema operations and API key auth
- unsafe zip paths such as absolute paths or `..` traversal

Warnings are advisory. Failures return a non-zero exit code.

## Skill generation

`GoAgent skill create` generates a reusable ChatGPT Skill package from the current GoAgent setup.

The generated Skill includes:

- `SKILL.md` with GoAgent-specific instructions and trigger guidance
- `agents/openai.yaml` metadata
- `references/goagent-setup.md` copied from the current `GoAgent gpt create` output
- `references/action-schema.yaml` generated from current provider config
- `references/action-schema-url.md` with schema, privacy, and authentication notes
- `references/shell-endpoints.md` generated from the current shell provider config
- `references/knowledge-files.md` if knowledge files exist

The Skill helps ChatGPT follow GoAgent conventions, but it does not install the Custom GPT Action, API key, MCP client, hosted connector registration, or transport wiring by itself. It is a very useful clipboard, not a licensed electrician.

## GPT Action schema

The daemon exposes the current OpenAPI YAML schema at:

```text
/config/schema
```

This endpoint does not require an API key so ChatGPT can load the schema while configuring the Action or connector.

The schema includes:

- `/health`
- `/version`
- `/fortune`
- `/fortune/config`
- any configured `/shell/<name>` endpoints from `~/.GoAgent/providers/shell/config.json`
- `X-API-Key` header authentication for protected Action and connector calls

For shell endpoints, any configured argument beginning with `$` becomes a required query parameter in the generated schema. This keeps the schema lined up with provider config, instead of maintaining the same thing twice, which is how small dragons hatch.

## Knowledge files

Files placed in:

```text
~/.GoAgent/knowledge/
```

are listed by `GoAgent gpt create` as:

```text
<server-url>/config/knowledge/<filename>
```

Knowledge file URLs require the configured `X-API-Key` header.

## Config

`GoAgent config` prints the raw complete config.

Scoped config commands print the effective subsystem view, assembled from the shared config keys that subsystem uses:

```text
GoAgent gpt config
GoAgent mcp config
GoAgent connector config
GoAgent skill config
```

Set and reset still use the stored config keys, plus friendly scoped keys where supported.

Example config shape:

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

`cloudflare.version` can be `latest` or a Cloudflare release tag such as `2025.6.0`. When set to a specific release, GoAgent downloads that release and checks that `cloudflared --version` matches the configured value. On macOS Catalina, the default `latest` behaviour is pinned internally to `2025.6.0` for compatibility.

Cloudflare modes:

```text
auto           Use a saved token if available; otherwise create a temporary tunnel.
temporary      Always create a temporary trycloudflare tunnel.
authenticated  Require a saved Cloudflare tunnel token and run that named tunnel.
disabled       Do not start Cloudflare Tunnel.
```

Shell provider response prefixes are configured in `~/.GoAgent/providers/shell/config.json`, not in the main GoAgent config file.

## More examples

See [EXAMPLES.md](EXAMPLES.md).
