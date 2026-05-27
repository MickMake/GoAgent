package defaults

import _ "embed"

// Need to execute `go generate -v -x defaults/const.go` OR `go generate -v -x ./...`
//go:generate cp ../README.md README.md
//go:generate cp ../EXAMPLES.md EXAMPLES.md

//go:embed README.md
var Readme string

//go:embed EXAMPLES.md
var Examples string

const (
	Description   = "GoAgent - Minimal GoLang daemon for ChatGPT Actions"
	BinaryName    = "GoAgent"
	BinaryVersion = "1.0"
	SourceRepo    = "github.com/MickMake/" + BinaryName
	BinaryRepo    = "github.com/MickMake/" + BinaryName

	EnvPrefix = "GOAGENT"

	HelpSummary = `
# GoAgent - Minimal GoLang daemon for ChatGPT Actions

This tool came about because I needed a cross-platform way of performing date and time manipulations within scripts.

- range - Produce a range of dates with variable duration span between.
- Support for more parse formats, (Java and C), using a simple JSON mapping file.
- Can run as an interactive shell.

Also, since it's based on my Unify package, it has support for self-updating.

`
)
