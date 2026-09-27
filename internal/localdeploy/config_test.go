package localdeploy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testAccount = "123456789012"
	testRepo    = "123456789012.dkr.ecr.us-east-1.amazonaws.com/app"
)

// validConfig returns a config map that passes validation; tests mutate it.
func validConfig() map[string]any {
	return map[string]any{
		"version": 1,
		"aws":     map[string]any{"region": "us-east-1", "account_id": testAccount},
		"gate":    map[string]any{"command": []any{"./scripts/test.sh"}, "timeout": "10m"},
		"images": []any{map[string]any{
			"name": "app", "context": ".", "platforms": []any{"linux/arm64"},
			"repository": testRepo, "config_key": "app:image",
		}},
		"pulumi": map[string]any{"work_dir": "infra", "stack": "staging"},
		"verify": map[string]any{
			"timeout":      "5m",
			"ecs_services": []any{map[string]any{"cluster": "c", "service": "s", "images": []any{"app"}}},
			"http": []any{map[string]any{
				"url": "https://example.com/version", "expect_status": 200,
				"json_field": "build.sha", "json_equals_source_sha": true,
			}},
		},
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseValid(t *testing.T) {
	cfg, err := Parse(mustJSON(t, validConfig()))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Images[0].ConfigKey != "app:image" || cfg.Verify.HTTP[0].JSONField != "build.sha" {
		t.Fatalf("decoded config = %+v", cfg)
	}
}

func TestParseRejects(t *testing.T) {
	image := func(c map[string]any) map[string]any { return c["images"].([]any)[0].(map[string]any) }
	httpCheck := func(c map[string]any) map[string]any {
		return c["verify"].(map[string]any)["http"].([]any)[0].(map[string]any)
	}
	tests := []struct {
		name   string
		mutate func(map[string]any)
		want   string
	}{
		{"unknown top-level field", func(c map[string]any) { c["extra"] = true }, "unknown field"},
		{"unknown nested field", func(c map[string]any) { image(c)["tag"] = "latest" }, "unknown field"},
		{"wrong version", func(c map[string]any) { c["version"] = 2 }, "version must be 1"},
		{"bad region", func(c map[string]any) { c["aws"].(map[string]any)["region"] = "mars" }, "aws.region"},
		{"bad account", func(c map[string]any) { c["aws"].(map[string]any)["account_id"] = "12" }, "aws.account_id"},
		{"bad role", func(c map[string]any) { c["aws"].(map[string]any)["assume_role_arn"] = "role" }, "assume_role_arn"},
		{"no gate", func(c map[string]any) { c["gate"].(map[string]any)["command"] = []any{} }, "gate.command"},
		{"bad gate timeout", func(c map[string]any) { c["gate"].(map[string]any)["timeout"] = "soon" }, "invalid duration"},
		{"zero gate timeout", func(c map[string]any) { c["gate"].(map[string]any)["timeout"] = "0s" }, "gate.timeout"},
		{"no images", func(c map[string]any) { c["images"] = []any{} }, "at least one image"},
		{"tagged repository", func(c map[string]any) { image(c)["repository"] = testRepo + ":latest" }, "without tag or digest"},
		{"digest repository", func(c map[string]any) { image(c)["repository"] = testRepo + "@sha256:" + strings.Repeat("a", 64) }, "without tag or digest"},
		{"repo in other account", func(c map[string]any) {
			image(c)["repository"] = "210987654321.dkr.ecr.us-east-1.amazonaws.com/app"
		}, "aws.account_id and aws.region"},
		{"repo in other region", func(c map[string]any) {
			image(c)["repository"] = "123456789012.dkr.ecr.eu-west-1.amazonaws.com/app"
		}, "aws.account_id and aws.region"},
		{"no platforms", func(c map[string]any) { image(c)["platforms"] = []any{} }, "platforms"},
		{"bad platform", func(c map[string]any) { image(c)["platforms"] = []any{"arm64"} }, "os/arch"},
		{"absolute context", func(c map[string]any) { image(c)["context"] = "/src" }, "relative path"},
		{"escaping context", func(c map[string]any) { image(c)["context"] = "../other" }, "relative path"},
		{"unqualified config key", func(c map[string]any) { image(c)["config_key"] = "image" }, "namespace:key"},
		{"bad build arg", func(c map[string]any) { image(c)["build_args"] = map[string]any{"1X": "v"} }, "build_args"},
		{"duplicate image", func(c map[string]any) {
			c["images"] = append(c["images"].([]any), image(c))
		}, "duplicated"},
		{"absolute pulumi dir", func(c map[string]any) { c["pulumi"].(map[string]any)["work_dir"] = "/infra" }, "work_dir"},
		{"no stack", func(c map[string]any) { c["pulumi"].(map[string]any)["stack"] = "" }, "pulumi.stack"},
		{"no checks", func(c map[string]any) { c["verify"] = map[string]any{"timeout": "1m"} }, "at least one"},
		{"unknown verify image", func(c map[string]any) {
			c["verify"].(map[string]any)["ecs_services"] = []any{map[string]any{"cluster": "c", "service": "s", "images": []any{"web"}}}
		}, "unknown image"},
		{"bad url", func(c map[string]any) { httpCheck(c)["url"] = "example.com" }, "absolute http(s) URL"},
		{"bad status", func(c map[string]any) { httpCheck(c)["expect_status"] = 42 }, "expect_status"},
		{"field without expectation", func(c map[string]any) { delete(httpCheck(c), "json_equals_source_sha") }, "json_field requires"},
		{"expectation without field", func(c map[string]any) { delete(httpCheck(c), "json_field") }, "json_field requires"},
		{"both expectations", func(c map[string]any) { httpCheck(c)["json_equals"] = "v1" }, "exclusive"},
		{"escaping record dir", func(c map[string]any) { c["record_dir"] = "../runs" }, "record_dir"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := validConfig()
			tt.mutate(c)
			_, err := Parse(mustJSON(t, c))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Parse error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestParseRejectsTrailingData(t *testing.T) {
	b := append(mustJSON(t, validConfig()), []byte(`{}`)...)
	if _, err := Parse(b); err == nil || !strings.Contains(err.Error(), "trailing") {
		t.Fatalf("Parse error = %v, want trailing data", err)
	}
}

func TestLoadResolvesPathsFromConfigDir(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "amsl-deploy.json")
	if err := os.WriteFile(p, mustJSON(t, validConfig()), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := cfg.PulumiDir(), filepath.Join(dir, "infra"); got != want {
		t.Fatalf("PulumiDir = %q, want %q", got, want)
	}
	if got, want := cfg.recordDir(), filepath.Join(dir, ".amsl-deploy", "runs"); got != want {
		t.Fatalf("recordDir = %q, want %q", got, want)
	}
}
