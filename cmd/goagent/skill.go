package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	defaultSkillDirectoryName = "skill-GoAgent"
	defaultSkillMetadataName  = "skill-goagent"
	defaultSkillZipFilename   = "skill-GoAgent.zip"
	maxSkillZipSizeBytes      = 25 * 1024 * 1024
)

var skillReferencePattern = regexp.MustCompile(`references/[A-Za-z0-9._/-]+`)

func runSkillCommand(cfg AppConfig, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: GoAgent skill create|verify")
	}

	switch args[0] {
	case "create":
		return runSkillCreateCommand(cfg, args[1:])
	case "verify":
		return runSkillVerifyCommand(args[1:])
	default:
		return fmt.Errorf("unknown skill command %q", args[0])
	}
}

func runSkillCreateCommand(cfg AppConfig, args []string) error {
	if len(args) != 0 {
		return errors.New("usage: GoAgent skill create")
	}

	bundle, err := buildSkillBundle(cfg, defaultSkillMetadataName)
	if err != nil {
		return err
	}
	if err := writeSkillZip(defaultSkillZipFilename, defaultSkillDirectoryName, bundle); err != nil {
		return err
	}

	checks := validateSkillZip(defaultSkillZipFilename)
	printSkillVerifyReport(checks)
	if countSkillFailures(checks) > 0 {
		return fmt.Errorf("created %s but verification failed", defaultSkillZipFilename)
	}

	fmt.Printf("created verified skill package: %s\n", defaultSkillZipFilename)
	return nil
}

func runSkillVerifyCommand(args []string) error {
	if len(args) != 0 {
		return errors.New("usage: GoAgent skill verify")
	}

	checks := validateSkillZip(defaultSkillZipFilename)
	printSkillVerifyReport(checks)
	if countSkillFailures(checks) > 0 {
		return fmt.Errorf("skill verification failed")
	}
	return nil
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
	bundle["agents/openai.yaml"] = renderOpenAIMetadata()
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

func renderOpenAIMetadata() string {
	return `interface:
  display_name: "Skill GoAgent"
  short_description: "Use a locally running GoAgent service and its configured Action endpoints."
  icon: "terminal"
  color: "#2563eb"
`
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

func writeSkillZip(output, skillDirectoryName string, bundle skillBundle) error {
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
		if err := addSkillZipFile(zipWriter, filepath.ToSlash(filepath.Join(skillDirectoryName, path)), bundle[path]); err != nil {
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

func validateSkillZip(filename string) []verifyCheck {
	checks := []verifyCheck{}

	info, err := os.Stat(filename)
	if err != nil {
		return []verifyCheck{{Status: verifyFail, Name: "skill zip exists", Detail: err.Error()}}
	}
	if info.Size() == 0 {
		checks = append(checks, verifyCheck{Status: verifyFail, Name: "skill zip size", Detail: "file is empty"})
	} else if info.Size() > maxSkillZipSizeBytes {
		checks = append(checks, verifyCheck{Status: verifyFail, Name: "skill zip size", Detail: fmt.Sprintf("%d bytes exceeds 25 MB upload target", info.Size())})
	} else {
		checks = append(checks, verifyCheck{Status: verifyPass, Name: "skill zip size", Detail: fmt.Sprintf("%d bytes", info.Size())})
	}

	reader, err := zip.OpenReader(filename)
	if err != nil {
		checks = append(checks, verifyCheck{Status: verifyFail, Name: "skill zip opens", Detail: err.Error()})
		return checks
	}
	defer reader.Close()
	checks = append(checks, verifyCheck{Status: verifyPass, Name: "skill zip opens", Detail: filename})

	fileSet := map[string]bool{}
	topDirs := map[string]bool{}
	unsafeEntries := []string{}
	for _, file := range reader.File {
		clean := path.Clean(file.Name)
		if clean == "." || clean == "" || strings.HasPrefix(clean, "../") || clean == ".." || path.IsAbs(clean) || strings.Contains(clean, "\\") {
			unsafeEntries = append(unsafeEntries, file.Name)
			continue
		}
		fileSet[clean] = true
		parts := strings.Split(clean, "/")
		if len(parts) < 2 || parts[0] == "" {
			unsafeEntries = append(unsafeEntries, file.Name)
			continue
		}
		topDirs[parts[0]] = true
	}
	if len(unsafeEntries) > 0 {
		checks = append(checks, verifyCheck{Status: verifyFail, Name: "zip paths", Detail: strings.Join(unsafeEntries, ", ")})
	} else {
		checks = append(checks, verifyCheck{Status: verifyPass, Name: "zip paths", Detail: "no absolute, traversal, or top-level loose entries"})
	}

	if len(topDirs) != 1 || !topDirs[defaultSkillDirectoryName] {
		dirs := make([]string, 0, len(topDirs))
		for dir := range topDirs {
			dirs = append(dirs, dir)
		}
		sort.Strings(dirs)
		checks = append(checks, verifyCheck{Status: verifyFail, Name: "top-level skill directory", Detail: fmt.Sprintf("expected %s, found %s", defaultSkillDirectoryName, strings.Join(dirs, ", "))})
	} else {
		checks = append(checks, verifyCheck{Status: verifyPass, Name: "top-level skill directory", Detail: defaultSkillDirectoryName})
	}

	requiredFiles := []string{
		defaultSkillDirectoryName + "/SKILL.md",
		defaultSkillDirectoryName + "/agents/openai.yaml",
		defaultSkillDirectoryName + "/references/goagent-setup.md",
		defaultSkillDirectoryName + "/references/action-schema.yaml",
		defaultSkillDirectoryName + "/references/action-schema-url.md",
		defaultSkillDirectoryName + "/references/shell-endpoints.md",
	}
	missing := []string{}
	for _, required := range requiredFiles {
		if !fileSet[required] {
			missing = append(missing, required)
		}
	}
	if len(missing) > 0 {
		checks = append(checks, verifyCheck{Status: verifyFail, Name: "required files", Detail: strings.Join(missing, ", ")})
	} else {
		checks = append(checks, verifyCheck{Status: verifyPass, Name: "required files", Detail: fmt.Sprintf("%d present", len(requiredFiles))})
	}

	skillMD, ok, err := readZipTextFile(reader, defaultSkillDirectoryName+"/SKILL.md")
	if err != nil {
		checks = append(checks, verifyCheck{Status: verifyFail, Name: "SKILL.md read", Detail: err.Error()})
	} else if !ok {
		checks = append(checks, verifyCheck{Status: verifyFail, Name: "SKILL.md read", Detail: "missing"})
	} else {
		checks = append(checks, validateSkillMD(skillMD, fileSet)...)
	}

	metadata, ok, err := readZipTextFile(reader, defaultSkillDirectoryName+"/agents/openai.yaml")
	if err != nil {
		checks = append(checks, verifyCheck{Status: verifyFail, Name: "agents/openai.yaml read", Detail: err.Error()})
	} else if !ok {
		checks = append(checks, verifyCheck{Status: verifyFail, Name: "agents/openai.yaml read", Detail: "missing"})
	} else if strings.Contains(metadata, "display_name:") && strings.Contains(metadata, "Skill GoAgent") {
		checks = append(checks, verifyCheck{Status: verifyPass, Name: "agents/openai.yaml metadata", Detail: "display name present"})
	} else {
		checks = append(checks, verifyCheck{Status: verifyFail, Name: "agents/openai.yaml metadata", Detail: "missing expected display_name"})
	}

	schema, ok, err := readZipTextFile(reader, defaultSkillDirectoryName+"/references/action-schema.yaml")
	if err != nil {
		checks = append(checks, verifyCheck{Status: verifyFail, Name: "action schema read", Detail: err.Error()})
	} else if !ok {
		checks = append(checks, verifyCheck{Status: verifyFail, Name: "action schema read", Detail: "missing"})
	} else {
		checks = append(checks, validateSkillActionSchema(schema)...)
	}

	return checks
}

func validateSkillMD(skillMD string, fileSet map[string]bool) []verifyCheck {
	checks := []verifyCheck{}
	if !strings.HasPrefix(skillMD, "---\n") || !strings.Contains(skillMD[4:], "\n---\n") {
		checks = append(checks, verifyCheck{Status: verifyFail, Name: "SKILL.md frontmatter", Detail: "missing YAML frontmatter"})
	} else if strings.Contains(skillMD, "\nname: "+defaultSkillMetadataName+"\n") && strings.Contains(skillMD, "\ndescription: ") {
		checks = append(checks, verifyCheck{Status: verifyPass, Name: "SKILL.md frontmatter", Detail: "name and description present"})
	} else {
		checks = append(checks, verifyCheck{Status: verifyFail, Name: "SKILL.md frontmatter", Detail: "expected name: " + defaultSkillMetadataName})
	}

	if strings.Count(skillMD, "\n") > 500 {
		checks = append(checks, verifyCheck{Status: verifyWarn, Name: "SKILL.md length", Detail: "over 500 lines"})
	} else {
		checks = append(checks, verifyCheck{Status: verifyPass, Name: "SKILL.md length", Detail: fmt.Sprintf("%d lines", strings.Count(skillMD, "\n")+1)})
	}

	missingReferences := []string{}
	seen := map[string]bool{}
	for _, reference := range skillReferencePattern.FindAllString(skillMD, -1) {
		fullPath := defaultSkillDirectoryName + "/" + reference
		if seen[fullPath] {
			continue
		}
		seen[fullPath] = true
		if !fileSet[fullPath] {
			missingReferences = append(missingReferences, reference)
		}
	}
	if len(missingReferences) > 0 {
		checks = append(checks, verifyCheck{Status: verifyFail, Name: "SKILL.md references", Detail: strings.Join(missingReferences, ", ")})
	} else {
		checks = append(checks, verifyCheck{Status: verifyPass, Name: "SKILL.md references", Detail: fmt.Sprintf("%d linked reference(s) exist", len(seen))})
	}
	return checks
}

func validateSkillActionSchema(schema string) []verifyCheck {
	missing := []string{}
	for _, operationID := range []string{"getGoAgentHealth", "getGoAgentVersion", "getFortune"} {
		if !strings.Contains(schema, "operationId: "+operationID) {
			missing = append(missing, operationID)
		}
	}
	if !strings.Contains(schema, "ApiKeyAuth") {
		missing = append(missing, "ApiKeyAuth")
	}
	if len(missing) > 0 {
		return []verifyCheck{{Status: verifyFail, Name: "action schema sanity", Detail: strings.Join(missing, ", ")}}
	}
	return []verifyCheck{{Status: verifyPass, Name: "action schema sanity", Detail: "core operations and API key auth present"}}
}

func readZipTextFile(reader *zip.ReadCloser, name string) (string, bool, error) {
	for _, file := range reader.File {
		if path.Clean(file.Name) != name {
			continue
		}
		readCloser, err := file.Open()
		if err != nil {
			return "", true, err
		}
		defer readCloser.Close()
		contents, err := io.ReadAll(readCloser)
		if err != nil {
			return "", true, err
		}
		return string(contents), true, nil
	}
	return "", false, nil
}

func printSkillVerifyReport(checks []verifyCheck) {
	failures := countSkillFailures(checks)
	warnings := 0
	passed := 0

	fmt.Println("GoAgent skill verify")
	fmt.Println()
	for _, check := range checks {
		fmt.Printf("[%s] %s", check.Status, check.Name)
		if check.Detail != "" {
			fmt.Printf(" - %s", check.Detail)
		}
		fmt.Println()
		switch check.Status {
		case verifyWarn:
			warnings++
		case verifyPass:
			passed++
		}
	}
	fmt.Println()
	fmt.Printf("Summary: %d failed, %d warning(s), %d passed\n", failures, warnings, passed)
}

func countSkillFailures(checks []verifyCheck) int {
	failures := 0
	for _, check := range checks {
		if check.Status == verifyFail {
			failures++
		}
	}
	return failures
}
