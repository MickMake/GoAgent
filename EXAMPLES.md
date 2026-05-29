# GoAgent examples

Worked commands and copy/paste examples for GoAgent.

The examples assume `GoAgent` is on your `PATH`. If not, use `./GoAgent`.

## Build

```bash
go build -o GoAgent ./cmd/goagent
```

## Install optional fortune dependency

Debian/Ubuntu:

```bash
sudo apt install golang fortune-mod
```

macOS with Homebrew:

```bash
brew install fortune
```

## Quick start

Generate an API key and capture it for this shell session:

```bash
export GOAGENT_API_KEY="$(GoAgent gpt key create | awk '/X-API-Key:/ {print $2}')"
```

Start the default server mode:

```bash
GoAgent serve
```

By default, `GoAgent serve` runs the GPT HTTP Action server only.

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

## Serve modes

Start only the Custom GPT Actions HTTP server:

```bash
GoAgent serve gpt
```

Start only the MCP stdio server:

```bash
GoAgent serve mcp
```

Make bare `GoAgent serve` run both the GPT HTTP server and MCP server:

```bash
GoAgent config set serve.gpt_enabled true
GoAgent config set serve.mcp_enabled true
GoAgent serve
```

For separate terminals or supervisors, run them explicitly:

```bash
# Terminal 1
GoAgent serve gpt

# Terminal 2, or launched by an MCP client
GoAgent serve mcp
```

## GPT setup artifacts

Generate GPT setup/action artifacts using configured URLs or prompts:

```bash
GoAgent gpt create
```

Provide both URLs directly:

```bash
GoAgent gpt create https://example.trycloudflare.com https://example.com/privacy
```

Generated files:

```text
~/.GoAgent/artifacts/gpt/setup.md
~/.GoAgent/artifacts/gpt/action-schema.yaml
```

Verify the local GPT setup:

```bash
GoAgent gpt verify
```

## GPT keys and Cloudflare tokens

List API keys:

```bash
GoAgent gpt key
```

Create the default API key:

```bash
GoAgent gpt key create
```

Create a named API key:

```bash
GoAgent gpt key create workshop
```

Remove a named API key:

```bash
GoAgent gpt key rm workshop
```

List Cloudflare tunnel tokens:

```bash
GoAgent gpt token
```

Create a Cloudflare tunnel token:

```bash
GoAgent gpt token create
```

Remove a Cloudflare tunnel token:

```bash
GoAgent gpt token rm default
```

Refresh the cached `cloudflared` binary:

```bash
GoAgent gpt cloudflared update
```

## MCP client artifacts

Generate a local MCP client config snippet using the absolute path to the current GoAgent binary:

```bash
GoAgent mcp create
```

Generated files:

```text
~/.GoAgent/artifacts/mcp/client-config.json
~/.GoAgent/artifacts/mcp/client-config.md
```

Example generated config shape:

```json
{
  "mcpServers": {
    "goagent": {
      "command": "/absolute/path/to/GoAgent",
      "args": ["serve", "mcp"]
    }
  }
}
```

Verify generated MCP artifacts:

```bash
GoAgent mcp verify
```

## Skill artifacts

Generate a reusable ChatGPT Skill package from the current GoAgent setup:

```bash
GoAgent skill create
```

Generated files:

```text
~/.GoAgent/artifacts/skill/skill-GoAgent.zip
~/.GoAgent/artifacts/skill/skill-GoAgent/
```

Verify the existing package:

```bash
GoAgent skill verify
```

The internal Skill frontmatter name is lowercase:

```yaml
name: skill-goagent
```

## GPT Action schema

The daemon exposes the current OpenAPI YAML schema at:

```text
/config/schema
```

Fetch it from a local server:

```bash
curl http://127.0.0.1:8080/config/schema
```

## Config

Show raw complete config:

```bash
GoAgent config
```

Show scoped effective config:

```bash
GoAgent gpt config
GoAgent mcp config
GoAgent skill config
```

Reset all config to defaults:

```bash
GoAgent config reset
```

Reset one key to its default:

```bash
GoAgent config reset serve.mcp_enabled
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

Set GPT setup URLs:

```bash
GoAgent config set gpt.server_url https://example.trycloudflare.com
GoAgent config set gpt.privacy_url https://example.com/privacy
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

## Artifact layout

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
