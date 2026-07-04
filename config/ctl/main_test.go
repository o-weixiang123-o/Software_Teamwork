package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyProfilesPassesForMinimalProfiles(t *testing.T) {
	root := writeMinimalConfig(t)

	issues := verifyProfiles(root)

	if len(issues) != 0 {
		t.Fatalf("expected no issues, got %v", issues)
	}
}

func TestVerifyProfilesReportsProfileNameMismatch(t *testing.T) {
	root := writeMinimalConfig(t)
	writeFile(t, root, "config/dev.yaml", "version: 1\nprofile: wrong\nextends: base\nenv:\n  APP_ENV:\n    value: dev\n")

	issues := verifyProfiles(root)

	assertIssue(t, issues, `config/dev.yaml: profile must be "dev"`)
}

func TestVerifyProfilesReportsInvalidYAML(t *testing.T) {
	root := writeMinimalConfig(t)
	writeFile(t, root, "config/staging.yaml", "version: [\n")

	issues := verifyProfiles(root)

	assertIssue(t, issues, "config/staging.yaml")
	assertIssue(t, issues, "did not find expected node content")
}

func TestVerifyProfilesRejectsProductionInlineSecret(t *testing.T) {
	root := writeMinimalConfig(t)
	writeFile(t, root, "config/production.yaml", "version: 1\nprofile: production\nextends: base\nenv:\n  API_TOKEN:\n    value: sk-live-secret-token-value-1234567890\n")

	issues := verifyProfiles(root)

	assertIssue(t, issues, "API_TOKEN must use fromEnv")
	assertIssue(t, issues, "API_TOKEN contains an obvious inline secret")
}

func TestRenderUsesSecretFileAndProcessEnvPrecedence(t *testing.T) {
	root := writeMinimalConfig(t)
	secret := filepath.Join(root, ".env.local")
	if err := os.WriteFile(secret, []byte("DATABASE_URL=postgres://file\nOPTIONAL_SECRET=file-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DATABASE_URL", "postgres://process")

	merged, err := loadMergedProfile(root, "dev")
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := parseDotenvFile(secret)
	if err != nil {
		t.Fatal(err)
	}
	resolved, missing := resolveEnv(merged.Env, secrets, processEnvMap())
	if len(missing) != 0 {
		t.Fatalf("expected no missing values, got %v", missing)
	}
	if resolved["DATABASE_URL"] != "postgres://process" {
		t.Fatalf("process env should win, got %q", resolved["DATABASE_URL"])
	}
	if resolved["OPTIONAL_SECRET"] != "file-secret" {
		t.Fatalf("secret file value missing, got %q", resolved["OPTIONAL_SECRET"])
	}
}

func TestRenderReportsMissingRequiredValues(t *testing.T) {
	root := writeMinimalConfig(t)
	merged, err := loadMergedProfile(root, "dev")
	if err != nil {
		t.Fatal(err)
	}

	_, missing := resolveEnv(merged.Env, nil, nil)

	assertString(t, missing, "DATABASE_URL")
}

func TestFormatShellQuotesValues(t *testing.T) {
	content, err := formatResolved(map[string]string{"PASSWORD": "a'b # c"}, "shell")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(content) != "export PASSWORD='a'\\''b # c'" {
		t.Fatalf("unexpected shell output: %q", content)
	}
}

func TestParseDotenvHandlesQuotesAndComments(t *testing.T) {
	values, err := parseDotenv(strings.NewReader("A=plain # comment\nB=\"two words\"\nexport C='hash#kept'\n"))
	if err != nil {
		t.Fatal(err)
	}
	if values["A"] != "plain" || values["B"] != "two words" || values["C"] != "hash#kept" {
		t.Fatalf("unexpected values: %#v", values)
	}
}

func writeMinimalConfig(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "config/schema.yaml", "version: 1\n")
	writeFile(t, root, "config/base.yaml", "version: 1\nprofile: base\nenv:\n  APP_ENV:\n    value: base\n  DATABASE_URL:\n    fromEnv: DATABASE_URL\n    required: true\n    sensitive: true\n  OPTIONAL_SECRET:\n    fromEnv: OPTIONAL_SECRET\n    sensitive: true\n")
	writeFile(t, root, "config/dev.yaml", "version: 1\nprofile: dev\nextends: base\nenv:\n  APP_ENV:\n    value: dev\n")
	writeFile(t, root, "config/staging.yaml", "version: 1\nprofile: staging\nextends: base\nenv:\n  APP_ENV:\n    value: staging\n")
	writeFile(t, root, "config/production.yaml", "version: 1\nprofile: production\nextends: base\nenv:\n  APP_ENV:\n    value: production\n")
	return root
}

func writeFile(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertIssue(t *testing.T, issues []string, expected string) {
	t.Helper()
	for _, issue := range issues {
		if strings.Contains(issue, expected) {
			return
		}
	}
	t.Fatalf("expected issue containing %q, got %v", expected, issues)
}

func assertString(t *testing.T, values []string, expected string) {
	t.Helper()
	for _, value := range values {
		if value == expected {
			return
		}
	}
	t.Fatalf("expected %q in %v", expected, values)
}
