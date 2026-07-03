package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

import "gopkg.in/yaml.v3"

const profileVersion = 1

var expectedProfiles = []string{"base", "dev", "staging", "production"}

type Profile struct {
	Version     int                 `yaml:"version"`
	Profile     string              `yaml:"profile"`
	Extends     string              `yaml:"extends"`
	Description string              `yaml:"description"`
	Env         map[string]EnvEntry `yaml:"env"`
}

type EnvEntry struct {
	Value       *string `yaml:"value"`
	FromEnv     string  `yaml:"fromEnv"`
	Required    bool    `yaml:"required"`
	Sensitive   bool    `yaml:"sensitive"`
	Description string  `yaml:"description"`
}

type renderOptions struct {
	root        string
	profile     string
	secretFiles []string
	format      string
	out         string
}

type stringListFlag []string

func (f *stringListFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *stringListFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usageError("missing command: verify or render")
	}
	switch args[0] {
	case "verify":
		fs := flag.NewFlagSet("verify", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		root := fs.String("root", "", "repository root")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		resolvedRoot, err := resolveRoot(*root)
		if err != nil {
			return err
		}
		issues := verifyProfiles(resolvedRoot)
		if len(issues) > 0 {
			for _, issue := range issues {
				fmt.Fprintf(os.Stderr, "- %s\n", issue)
			}
			return fmt.Errorf("config profile verification failed with %d issue(s)", len(issues))
		}
		fmt.Println("Config profile checks passed.")
		return nil
	case "render":
		opts, err := parseRenderOptions(args[1:])
		if err != nil {
			return err
		}
		resolvedRoot, err := resolveRoot(opts.root)
		if err != nil {
			return err
		}
		opts.root = resolvedRoot
		return render(opts)
	default:
		return usageError("unknown command %q", args[0])
	}
}

func usageError(format string, args ...any) error {
	return fmt.Errorf(format+"\n\nusage:\n  configctl verify [--root <repo>]\n  configctl render --profile dev --secret-file .env.local --format dotenv|shell --out <path> [--root <repo>]", args...)
}

func parseRenderOptions(args []string) (renderOptions, error) {
	fs := flag.NewFlagSet("render", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	opts := renderOptions{}
	fs.StringVar(&opts.root, "root", "", "repository root")
	fs.StringVar(&opts.profile, "profile", "dev", "profile name")
	var secretFiles stringListFlag
	fs.Var(&secretFiles, "secret-file", "dotenv secret file; may be repeated")
	fs.StringVar(&opts.format, "format", "dotenv", "dotenv or shell")
	fs.StringVar(&opts.out, "out", "-", "output path or - for stdout")
	if err := fs.Parse(args); err != nil {
		return opts, err
	}
	opts.secretFiles = secretFiles
	switch opts.format {
	case "dotenv", "shell":
	default:
		return opts, fmt.Errorf("--format must be dotenv or shell, got %q", opts.format)
	}
	if strings.TrimSpace(opts.profile) == "" {
		return opts, errors.New("--profile must not be empty")
	}
	return opts, nil
}

func resolveRoot(explicit string) (string, error) {
	if explicit != "" {
		root, err := filepath.Abs(explicit)
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(filepath.Join(root, "config", "base.yaml")); err != nil {
			return "", fmt.Errorf("%s is not a repository root with config/base.yaml", root)
		}
		return root, nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(wd, "config", "base.yaml")); err == nil {
			return wd, nil
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			break
		}
		wd = parent
	}
	return "", errors.New("could not find repository root containing config/base.yaml")
}

func verifyProfiles(root string) []string {
	var issues []string
	if _, err := os.Stat(filepath.Join(root, "config", "schema.yaml")); err != nil {
		issues = append(issues, "config/schema.yaml is required")
	}
	for _, name := range expectedProfiles {
		path := filepath.Join(root, "config", name+".yaml")
		profile, err := readProfile(path)
		if err != nil {
			issues = append(issues, fmt.Sprintf("config/%s.yaml: %v", name, err))
			continue
		}
		issues = append(issues, validateProfile(name, profile)...)
	}
	return issues
}

func validateProfile(expectedName string, profile Profile) []string {
	var issues []string
	if profile.Version != profileVersion {
		issues = append(issues, fmt.Sprintf("config/%s.yaml: version must be %d", expectedName, profileVersion))
	}
	if profile.Profile != expectedName {
		issues = append(issues, fmt.Sprintf("config/%s.yaml: profile must be %q, got %q", expectedName, expectedName, profile.Profile))
	}
	if expectedName == "base" && strings.TrimSpace(profile.Extends) != "" {
		issues = append(issues, "config/base.yaml: base profile must not extend another profile")
	}
	if expectedName != "base" && profile.Extends != "base" {
		issues = append(issues, fmt.Sprintf("config/%s.yaml: extends must be \"base\"", expectedName))
	}
	if len(profile.Env) == 0 {
		issues = append(issues, fmt.Sprintf("config/%s.yaml: env map must not be empty", expectedName))
	}
	for key, entry := range profile.Env {
		if !validEnvName(key) {
			issues = append(issues, fmt.Sprintf("config/%s.yaml: env key %q is not a valid environment variable name", expectedName, key))
		}
		if entry.Value != nil && strings.TrimSpace(entry.FromEnv) != "" {
			issues = append(issues, fmt.Sprintf("config/%s.yaml: %s must use either value or fromEnv, not both", expectedName, key))
		}
		if entry.FromEnv != "" && !validEnvName(entry.FromEnv) {
			issues = append(issues, fmt.Sprintf("config/%s.yaml: %s fromEnv %q is not a valid environment variable name", expectedName, key, entry.FromEnv))
		}
		if entry.Value == nil {
			continue
		}
		value := *entry.Value
		if expectedName == "staging" || expectedName == "production" {
			if entry.Sensitive || secretLikeKey(key) {
				issues = append(issues, fmt.Sprintf("config/%s.yaml: %s must use fromEnv in %s profiles", expectedName, key, expectedName))
			}
		}
		if looksLikeSecretValue(key, value) {
			issues = append(issues, fmt.Sprintf("config/%s.yaml: %s contains an obvious inline secret", expectedName, key))
		}
	}
	return issues
}

func readProfile(path string) (Profile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Profile{}, err
	}
	var profile Profile
	if err := yaml.Unmarshal(data, &profile); err != nil {
		return Profile{}, err
	}
	return profile, nil
}

func render(opts renderOptions) error {
	merged, err := loadMergedProfile(opts.root, opts.profile)
	if err != nil {
		return err
	}
	secretValues := map[string]string{}
	for _, secretFile := range opts.secretFiles {
		path := secretFile
		if !filepath.IsAbs(path) {
			path = filepath.Join(opts.root, path)
		}
		values, err := parseDotenvFile(path)
		if err != nil {
			return err
		}
		for key, value := range values {
			secretValues[key] = value
		}
	}
	envValues := processEnvMap()
	resolved, missing := resolveEnv(merged.Env, secretValues, envValues)
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("missing required configuration value(s): %s", strings.Join(missing, ", "))
	}
	content, err := formatResolved(resolved, opts.format)
	if err != nil {
		return err
	}
	if opts.out == "-" || opts.out == "" {
		fmt.Print(content)
		return nil
	}
	outPath := opts.out
	if !filepath.IsAbs(outPath) {
		outPath = filepath.Join(opts.root, outPath)
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(outPath, []byte(content), 0o600)
}

func loadMergedProfile(root, profileName string) (Profile, error) {
	base, err := readProfile(filepath.Join(root, "config", "base.yaml"))
	if err != nil {
		return Profile{}, fmt.Errorf("read base profile: %w", err)
	}
	if profileName == "base" {
		return base, nil
	}
	profile, err := readProfile(filepath.Join(root, "config", profileName+".yaml"))
	if err != nil {
		return Profile{}, fmt.Errorf("read %s profile: %w", profileName, err)
	}
	if profile.Extends != "base" {
		return Profile{}, fmt.Errorf("config/%s.yaml must extend base", profileName)
	}
	merged := base
	merged.Profile = profile.Profile
	merged.Extends = profile.Extends
	merged.Description = profile.Description
	if merged.Env == nil {
		merged.Env = map[string]EnvEntry{}
	}
	for key, entry := range profile.Env {
		merged.Env[key] = entry
	}
	return merged, nil
}

func resolveEnv(entries map[string]EnvEntry, secretValues, envValues map[string]string) (map[string]string, []string) {
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	resolved := make(map[string]string, len(entries))
	var missing []string
	for _, key := range keys {
		entry := entries[key]
		value, ok := lookupOverride(key, entry.FromEnv, secretValues)
		if envValue, envOK := lookupOverride(key, entry.FromEnv, envValues); envOK {
			value, ok = envValue, true
		}
		if !ok && entry.Value != nil {
			value, ok = *entry.Value, true
		}
		if entry.Required && (!ok || strings.TrimSpace(value) == "") {
			if entry.FromEnv != "" {
				missing = append(missing, entry.FromEnv)
			} else {
				missing = append(missing, key)
			}
			continue
		}
		if ok {
			resolved[key] = value
		}
	}
	return resolved, missing
}

func lookupOverride(key, fromEnv string, values map[string]string) (string, bool) {
	if value, ok := values[key]; ok {
		return value, true
	}
	if fromEnv != "" && fromEnv != key {
		if value, ok := values[fromEnv]; ok {
			return value, true
		}
	}
	return "", false
}

func processEnvMap() map[string]string {
	values := map[string]string{}
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			values[key] = value
		}
	}
	return values
}

func formatResolved(values map[string]string, format string) (string, error) {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, key := range keys {
		value := values[key]
		switch format {
		case "dotenv":
			fmt.Fprintf(&b, "%s=%s\n", key, quoteDotenv(value))
		case "shell":
			fmt.Fprintf(&b, "export %s=%s\n", key, quoteShell(value))
		default:
			return "", fmt.Errorf("unsupported format %q", format)
		}
	}
	return b.String(), nil
}

var dotenvSafe = regexp.MustCompile(`^[A-Za-z0-9_./:@%+,\-=?]+$`)

func quoteDotenv(value string) string {
	if value == "" {
		return ""
	}
	if dotenvSafe.MatchString(value) {
		return value
	}
	return strconv.Quote(value)
}

func quoteShell(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func parseDotenvFile(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read dotenv file %s: %w", path, err)
	}
	defer file.Close()
	return parseDotenv(file)
}

func parseDotenv(reader io.Reader) (map[string]string, error) {
	values := map[string]string{}
	scanner := bufio.NewScanner(reader)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		key, value, ok, err := parseDotenvLine(scanner.Text())
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
		if ok {
			values[key] = value
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

func parseDotenvLine(line string) (string, string, bool, error) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", "", false, nil
	}
	if strings.HasPrefix(trimmed, "export ") {
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "export "))
	}
	key, rawValue, ok := strings.Cut(trimmed, "=")
	if !ok {
		return "", "", false, nil
	}
	key = strings.TrimSpace(key)
	if !validEnvName(key) {
		return "", "", false, fmt.Errorf("invalid environment variable name %q", key)
	}
	value, err := parseDotenvValue(strings.TrimSpace(rawValue))
	if err != nil {
		return "", "", false, err
	}
	return key, value, true, nil
}

func parseDotenvValue(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	switch raw[0] {
	case '\'':
		end := strings.LastIndex(raw[1:], "'")
		if end < 0 {
			return "", errors.New("unterminated single-quoted value")
		}
		return raw[1 : end+1], nil
	case '"':
		end := closingDoubleQuote(raw)
		if end < 0 {
			return "", errors.New("unterminated double-quoted value")
		}
		return strconv.Unquote(raw[:end+1])
	default:
		return strings.TrimSpace(stripInlineComment(raw)), nil
	}
}

func closingDoubleQuote(raw string) int {
	escaped := false
	for i := 1; i < len(raw); i++ {
		ch := raw[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if ch == '"' {
			return i
		}
	}
	return -1
}

func stripInlineComment(raw string) string {
	for i, r := range raw {
		if r == '#' && (i == 0 || unicode.IsSpace(rune(raw[i-1]))) {
			return raw[:i]
		}
	}
	return raw
}

func validEnvName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if i == 0 {
			if r != '_' && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') {
				return false
			}
			continue
		}
		if r != '_' && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func secretLikeKey(key string) bool {
	upper := strings.ToUpper(key)
	if strings.HasSuffix(upper, "_TOKEN_HEADER") || strings.HasSuffix(upper, "_KEY_REF") {
		return false
	}
	for _, needle := range []string{
		"PASSWORD",
		"TOKEN",
		"SECRET",
		"API_KEY",
		"PRIVATE_KEY",
		"CREDENTIAL",
		"DSN",
		"DATABASE_URL",
	} {
		if strings.Contains(upper, needle) {
			return true
		}
	}
	return false
}

var (
	urlWithCredentials = regexp.MustCompile(`(?i)^[a-z][a-z0-9+.-]*://[^/\s:@]+:[^@\s]+@`)
	providerKey        = regexp.MustCompile(`(?i)^(sk-[A-Za-z0-9_-]{16,}|gh[pousr]_[A-Za-z0-9_]{20,}|AKIA[0-9A-Z]{16}|AIza[0-9A-Za-z_-]{20,})`)
)

func looksLikeSecretValue(key, value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	if strings.Contains(trimmed, "-----BEGIN ") || strings.Contains(trimmed, "PRIVATE KEY-----") {
		return true
	}
	if urlWithCredentials.MatchString(trimmed) {
		return true
	}
	if providerKey.MatchString(trimmed) {
		return true
	}
	if secretLikeKey(key) && highEntropy(trimmed) && !strings.Contains(strings.ToLower(trimmed), "local") && !strings.Contains(strings.ToLower(trimmed), "placeholder") && !strings.Contains(strings.ToLower(trimmed), "change-me") {
		return true
	}
	return false
}

func highEntropy(value string) bool {
	if len(value) < 32 {
		return false
	}
	var lower, upper, digit, symbol bool
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			lower = true
		case r >= 'A' && r <= 'Z':
			upper = true
		case r >= '0' && r <= '9':
			digit = true
		default:
			symbol = true
		}
	}
	categories := 0
	for _, ok := range []bool{lower, upper, digit, symbol} {
		if ok {
			categories++
		}
	}
	return categories >= 3
}
