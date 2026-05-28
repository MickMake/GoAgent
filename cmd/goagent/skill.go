package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const defaultSkillName = "local-goagent"

var nonSkillNameChars = regexp.MustCompile(`[^a-z0-9]+`)

func runSkillCommand(cfg AppConfig, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: GoAgent skill create [name] [output]")
	}

	switch args[0] {
	case "create":
		return runSkillCreateCommand(cfg, args[1:])
	default:
		return fmt.Errorf("unknown skill command %q", args[0])
	}
}

func runSkillCreateCommand(cfg AppConfig, args []string) error {
	if len(args) > 2 {
		return errors.New("usage: GoAgent skill create [name] [output]")
	}

	name := defaultSkillName
	if len(args) >= 1 && strings.TrimSpace(args[0]) != "" {
		name = args[0]
	}
	name = normalizeSkillName(name)
	if name == "" {
		return errors.New("skill name must contain at least one letter or digit")
	}

	output := "skill.zip"
	if len(args) == 2 && strings.TrimSpace(args[1]) != "" {
		output = args[1]
	}
	if strings.HasSuffix(output, string(os.PathSeparator)) || strings.HasSuffix(output, "/") {
		output = filepath.Join(output, "skill.zip")
	}
	if filepath.Base(output) != "skill.zip" {
		output = filepath.Join(output, "skill.zip")
	}

	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil && filepath.Dir(output) != "." {
		return err
	}

	bundle, err := buildSkillBundle(cfg, name)
	if err != nil {
		return err
	}
	if err := writeSkillZip(output, name, bundle); err != nil {
		return err
	}
	fmt.Printf("created skill package: %s\n", output)
	return nil
}

func normalizeSkillName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = nonSkillNameChars.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")
	return name
}

type skillBundle map[string]string

func buildSkillBundle(cfg AppConfig, skillName string) (skillBundle, error) {
	serverURL := defaultSetupServerURL(cfg)
	privacyURL := defaultSetupPrivacyURL(cfg, serverURL)
	apiKey := "<paste the GoAgent X-API-Key value configured for this Action>"
	if existing, err := readNamedSecret(goagentAPIKeyPath(cfg, cfg.Listener.DefaultAPIKey)); err == nil && existing != "" {
		apiKey = existing
	}

	shellCfg, err := loadShellSchemaConfig(cfg.Global.ProviderBaseDir)
	if err != nil {
		return nil, err
	}
	knowledgeFiles, err := listKnowledgeFiles()
	if err != nil {
		return nil, err
	}

	setup := &bytes.Buffer{}
	writeGPTSetup(setup, serverURL, privacyURL, apiKey, shellCfg, knowledgeFiles)

	schema := &bytes.Buffer{}
	writeGPTActionSchema(schema, serverURL, shellCfg)

	bundle := skillBundle{}
	bundle["SKILL.md"] = renderSkillMD(skillName, serverURL, shellCfg, len(knowledgeFiles) > 0)
	bundle["agents/openai.yaml"] = renderOpenAIMetadata(skillName)
	bundle["references/goagent-setup.md"] = setup.String()
	bundle["references/action-schema.yaml"] = schema.String()
	bundle["references/action-schema-url.md"] = renderSchemaURL(serverURL, privacyURL, apiKey)
	bundle["references/shell-endpoints.md"] = renderShellEndpointReference(shellCfg)
	if len(knowledgeFiles) > 0 {
		bundle["references/knowledge-files.md"] = renderKnowledgeReference(serverURL, knowledgeFiles)
	}
	return bundle, nil
}

func renderSkillMD(skillName, serverURL string, shellCfg shellSchemaConfig, hasKnowledge bool) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("name: ")
	b.WriteString(skillName)
	b.WriteString("\n")
	b.WriteString("description: use this skill when the user asks to work with a locally running goagent service, goagent custom gpt action, goagent shell endpoints, goagent knowledge files, or local helper capabilities exposed through goagent. trigger for requests to check goagent health or version, call configured goagent action endpoints, troubleshoot stale cloudflare tunnel or api key setup, regenerate action configuration guidance, or follow goagent-specific response conventions such as shell response prefixes.\n")
	b.WriteString("---\n\n")
	b.WriteString("# GoAgent Skill\n\n")
	b.WriteString("Use this skill to guide work with a locally running GoAgent service and its configured ChatGPT Action.\n\n")
	b.WriteString("## Core rules\n\n")
	b.WriteString("- Use the GoAgent Action only for capabilities explicitly described by the configured Action schema.\n")
	b.WriteString("- Do not invent GoAgent endpoints, shell commands, file paths, or operation IDs.\n")
	b.WriteString("- When a shell endpoint response includes a `prefix` field, begin the final answer with that exact prefix.\n")
	b.WriteString("- When a response includes a `marker` field, include it verbatim if it helps the user confirm the endpoint was reached.\n")
	b.WriteString("- Treat shell endpoint parameters as data only. Never pass commands, shell syntax, flags, file paths, or programs unless the endpoint documentation explicitly says to.\n")
	b.WriteString("- If a GoAgent call fails, briefly suggest checking whether GoAgent is running, whether the Cloudflare tunnel URL is current, and whether the `X-API-Key` matches the running service.\n\n")
	b.WriteString("## Generated references\n\n")
	b.WriteString("- Read `references/goagent-setup.md` when configuring or updating the Custom GPT.\n")
	b.WriteString("- Read `references/action-schema-url.md` when the user needs the schema URL, privacy URL, or API key placement.\n")
	b.WriteString("- Read `references/action-schema.yaml` when checking exact operation IDs, parameters, and endpoints.\n")
	b.WriteString("- Read `references/shell-endpoints.md` before using or explaining shell helper endpoints.\n")
	if hasKnowledge {
		b.WriteString("- Read `references/knowledge-files.md` when the user asks about GoAgent knowledge files.\n")
	}
	b.WriteString("\n")
	b.WriteString("## Current generated context\n\n")
	b.WriteString("- Default server URL at generation time: `")
	b.WriteString(serverURL)
	b.WriteString("`\n")
	if prefix := strings.TrimSpace(shellCfg.Prefix); prefix != "" {
		b.WriteString("- Shell response prefix is configured as `")
		b.WriteString(prefixWithTrailingSpace(prefix))
		b.WriteString("`.\n")
	}
	if len(shellCfg.Instructions) > 0 {
		b.WriteString("- Shell provider includes global setup instructions in `references/shell-endpoints.md`.\n")
	}
	return b.String()
}

func renderOpenAIMetadata(skillName string) string {
	displayName := strings.ReplaceAll(skillName, "-", " ")
	words := strings.Fields(displayName)
	for i, word := range words {
		if word == "" {
			continue
		}
		words[i] = strings.ToUpper(word[:1]) + word[1:]
	}
	if len(words) == 0 {
		displayName = "Local GoAgent"
	} else {
		displayName = strings.Join(words, " ")
	}
	return fmt.Sprintf(`interface:
  display_name: %q
  short_description: "Use a locally running GoAgent service and its configured Action endpoints."
  icon: "terminal"
  color: "#2563eb"
`, displayName)
}

func renderSchemaURL(serverURL, privacyURL, apiKey string) string {
	var b strings.Builder
	b.WriteString("# GoAgent Action configuration\n\n")
	b.WriteString("## Schema URL\n\n")
	b.WriteString("```text\n")
	b.WriteString(strings.TrimRight(serverURL, "/"))
	b.WriteString("/config/schema\n")
	b.WriteString("```\n\n")
	b.WriteString("This schema URL does not require an API key.\n\n")
	b.WriteString("## Authentication\n\n")
	b.WriteString("Use API key authentication with this header name:\n\n")
	b.WriteString("```text\nX-API-Key\n```\n\n")
	b.WriteString("Configured key value at generation time:\n\n")
	b.WriteString("```text\n")
	b.WriteString(apiKey)
	b.WriteString("\n```\n\n")
	b.WriteString("## Privacy policy URL\n\n")
	b.WriteString("```text\n")
	b.WriteString(privacyURL)
	b.WriteString("\n```\n")
	return b.String()
}

func renderShellEndpointReference(shellCfg shellSchemaConfig) string {
	var b strings.Builder
	b.WriteString("# GoAgent shell endpoints\n\n")
	if prefix := strings.TrimSpace(shellCfg.Prefix); prefix != "" {
		b.WriteString("Shell response prefix: `")
		b.WriteString(prefixWithTrailingSpace(prefix))
		b.WriteString("`\n\n")
	}
	if len(shellCfg.Instructions) > 0 {
		b.WriteString("## Global shell instructions\n\n")
		for _, instruction := range shellCfg.Instructions {
			instruction = strings.TrimSpace(instruction)
			if instruction == "" {
				continue
			}
			b.WriteString("- ")
			b.WriteString(instruction)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	names := sortedShellEndpointNames(shellCfg)
	if len(names) == 0 {
		b.WriteString("No shell endpoints were configured when this skill was generated.\n")
		return b.String()
	}

	b.WriteString("## Endpoints\n\n")
	for _, name := range names {
		endpoint := shellCfg.Endpoints[name]
		operationID := "runShell" + operationName(strings.Trim(name, "/"))
		b.WriteString("### ")
		b.WriteString(strings.Trim(name, "/"))
		b.WriteString("\n\n")
		b.WriteString("- Operation ID: `")
		b.WriteString(operationID)
		b.WriteString("`\n")
		if endpoint.Description != "" {
			b.WriteString("- Description: ")
			b.WriteString(endpoint.Description)
			b.WriteString("\n")
		}
		if endpoint.Instruction != "" {
			b.WriteString("- Instruction: ")
			b.WriteString(endpoint.Instruction)
			b.WriteString("\n")
		}
		params := shellQueryParams(endpoint.Args)
		if len(params) > 0 {
			b.WriteString("- Required query parameters: `")
			b.WriteString(strings.Join(params, "`, `"))
			b.WriteString("`\n")
		} else {
			b.WriteString("- Required query parameters: none\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

func renderKnowledgeReference(serverURL string, files []string) string {
	var b strings.Builder
	b.WriteString("# GoAgent knowledge files\n\n")
	b.WriteString("Knowledge file URLs require the configured `X-API-Key` header.\n\n")
	for _, file := range files {
		b.WriteString("- `")
		b.WriteString(file)
		b.WriteString("`: ")
		b.WriteString(strings.TrimRight(serverURL, "/"))
		b.WriteString("/config/knowledge/")
		b.WriteString(pathEscape(file))
		b.WriteString("\n")
	}
	return b.String()
}

func writeSkillZip(output, skillName string, bundle skillBundle) error {
	file, err := os.Create(output)
	if err != nil {
		return err
	}
	defer file.Close()

	zipWriter := zip.NewWriter(file)
	defer zipWriter.Close()

	paths := make([]string, 0, len(bundle))
	for path := range bundle {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	for _, path := range paths {
		if err := addSkillZipFile(zipWriter, filepath.ToSlash(filepath.Join(skillName, path)), bundle[path]); err != nil {
			return err
		}
	}
	return nil
}

func addSkillZipFile(zipWriter *zip.Writer, name, content string) error {
	header := &zip.FileHeader{
		Name:     name,
		Method:   zip.Deflate,
		Modified: time.Now(),
	}
	header.SetMode(0o644)
	writer, err := zipWriter.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = io.WriteString(writer, content)
	return err
}
