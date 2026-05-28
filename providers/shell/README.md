# GoAgent Shell Provider

The shell provider exposes selected local commands as authenticated GoAgent endpoints under:

```text
/shell/<endpoint-name>
```

Configuration lives at:

```text
~/.GoAgent/providers/shell/config.json
```

On first run, GoAgent creates this directory and writes a small example config if one does not already exist.

## Safety model

The shell provider is intentionally boring.

Boring is good here. Boring is the little bolt that keeps the cupboard from becoming a trebuchet.

The provider:

- requires every command to be an absolute path, or to start with `~/`
- runs commands with `exec.Command`, not through `sh -c`
- does not support pipes, redirects, glob expansion, command chaining, or shell interpolation
- treats `$name` arguments as query parameter placeholders
- passes query parameter values as command arguments, not as executable code
- protects shell endpoints with the configured `X-API-Key` header

Do not expose broad command runners, scripting interpreters with user-controlled programs, package managers, destructive commands, or commands that write arbitrary files.

## Config shape

```json
{
  "endpoints": {
    "endpoint-name": {
      "command": "/absolute/path/to/program",
      "args": ["fixed", "$query_parameter"],
      "description": "Short summary used in the generated Action schema.",
      "instruction": "Instruction added to GoAgent setup output.",
      "conversation_starters": [
        "Example starter added to GoAgent setup output."
      ]
    }
  }
}
```

Required fields:

- `command`: absolute command path, or a path beginning with `~/`
- `args`: command argument list

Optional fields:

- `chroot`: working root used by the command, if supported by the current platform/runtime
- `description`: used as the generated OpenAPI summary
- `instruction`: added to the GPT instructions printed by `GoAgent setup`
- `conversation_starters`: added to the conversation starters printed by `GoAgent setup`

After editing the config, rerun:

```bash
GoAgent setup https://your-goagent-url.example
```

Then update the GPT instructions, starters, and Action schema from the generated output.

## Example 1: OS version

This endpoint returns the local operating system version from `uname -v`.

```json
{
  "endpoints": {
    "os-version": {
      "command": "/usr/bin/uname",
      "args": ["-v"],
      "description": "Return the operating system version string from uname -v.",
      "instruction": "When the user asks for the local operating system version, call runShellOsVersion.",
      "conversation_starters": [
        "GoAgent, what OS version is this running on?"
      ]
    }
  }
}
```

Request:

```bash
curl \
  -H "X-API-Key: YOUR_KEY" \
  https://your-goagent-url.example/shell/os-version
```

The generated Action operation ID will be:

```text
runShellOsVersion
```

## Example 2: uppercase supplied text

This endpoint accepts user input as the `text` query parameter and uppercases it using a fixed awk program.

```json
{
  "endpoints": {
    "upper": {
      "command": "/usr/bin/awk",
      "args": [
        "BEGIN { print toupper(ARGV[1]); exit }",
        "$text"
      ],
      "description": "Uppercase supplied text using a fixed awk program.",
      "instruction": "When the user asks to uppercase text, call runShellUpper with the text parameter set to only the text to transform. Do not pass commands, awk programs, file paths, flags, or shell syntax.",
      "conversation_starters": [
        "GoAgent, uppercase this text: measure twice, cut once"
      ]
    }
  }
}
```

Request:

```bash
curl \
  -H "X-API-Key: YOUR_KEY" \
  "https://your-goagent-url.example/shell/upper?text=measure%20twice%2C%20cut%20once"
```

What happens internally is equivalent to:

```bash
/usr/bin/awk 'BEGIN { print toupper(ARGV[1]); exit }' 'measure twice, cut once'
```

The user controls only the second argument, which is data. The user does not control the awk program.

The generated Action operation ID will be:

```text
runShellUpper
```

## Combining the defaults

A complete default-style config with both examples:

```json
{
  "endpoints": {
    "os-version": {
      "command": "/usr/bin/uname",
      "args": ["-v"],
      "description": "Return the operating system version string from uname -v.",
      "instruction": "When the user asks for the local operating system version, call runShellOsVersion.",
      "conversation_starters": [
        "GoAgent, what OS version is this running on?"
      ]
    },
    "upper": {
      "command": "/usr/bin/awk",
      "args": [
        "BEGIN { print toupper(ARGV[1]); exit }",
        "$text"
      ],
      "description": "Uppercase supplied text using a fixed awk program.",
      "instruction": "When the user asks to uppercase text, call runShellUpper with the text parameter set to only the text to transform. Do not pass commands, awk programs, file paths, flags, or shell syntax.",
      "conversation_starters": [
        "GoAgent, uppercase this text: measure twice, cut once"
      ]
    }
  }
}
```

## Query parameter placeholders

Any argument beginning with `$` becomes a required query parameter.

Example:

```json
"args": ["fixed", "$text", "$format"]
```

creates required query parameters:

```text
text
format
```

A request must include both:

```text
/shell/example?text=hello&format=upper
```

The values are passed as separate command arguments.

## Chroot example

The optional `chroot` field is for endpoints that should run with a restricted root filesystem.

Example shape:

```json
{
  "endpoints": {
    "sandbox-uname": {
      "command": "/usr/bin/uname",
      "args": ["-v"],
      "chroot": "/srv/goagent/chroot",
      "description": "Return uname output from inside a configured chroot.",
      "instruction": "When the user asks for uname output from the shell sandbox, call runShellSandboxUname.",
      "conversation_starters": [
        "GoAgent, check uname inside the shell sandbox."
      ]
    }
  }
}
```

Chroot is not magic fairy dust. The chroot directory must contain whatever the command needs to run, including the binary and any required libraries or files. A minimal chroot is usually fiddly.

Before using chroot, confirm:

- the GoAgent process has permission to use it
- the target platform supports it as expected
- required binaries and libraries exist inside the chroot
- the endpoint still uses fixed commands and fixed programs
- user input remains data only

## Recommended endpoint rules

Prefer endpoints that:

- run one specific command
- have a narrow purpose
- use fixed arguments where possible
- accept only data parameters
- include `description`, `instruction`, and `conversation_starters`
- produce simple text output

Avoid endpoints that:

- execute user-supplied commands
- accept user-supplied programs or scripts
- use shells such as `/bin/sh -c` or `/bin/bash -c`
- run package managers
- write or delete arbitrary files
- expose secrets, tokens, environment variables, SSH keys, or private paths

## Updating the GPT after adding endpoints

1. Edit:

```text
~/.GoAgent/providers/shell/config.json
```

2. Restart GoAgent if it is already running.

3. Regenerate setup output:

```bash
GoAgent setup https://your-goagent-url.example
```

4. Update the GPT Configure fields and Action schema from the generated output.

The goal is: add endpoint metadata once, then let `GoAgent setup` do the dull copying work. Dull copying work is what computers are for, despite their ongoing attempts to become poets.
