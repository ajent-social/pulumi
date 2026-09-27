package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ajent-social/pulumi/internal/localdeploy"
)

func TestExitCode(t *testing.T) {
	tests := []struct {
		outcome localdeploy.Outcome
		err     error
		want    int
	}{
		{localdeploy.OutcomeSucceeded, nil, exitOK},
		{localdeploy.OutcomePlanned, nil, exitOK},
		{localdeploy.OutcomeSucceeded, errors.New("write run record"), exitRefused},
		{localdeploy.OutcomeRefused, errors.New("gate failed"), exitRefused},
		{localdeploy.OutcomeRolledBack, errors.New("verify"), exitRolledBack},
		{localdeploy.OutcomeRollbackFailed, errors.New("ROLLBACK FAILED"), exitRollbackFailed},
	}
	unpublished := &localdeploy.Record{Outcome: localdeploy.OutcomeSucceeded, PinPR: &localdeploy.PinPRResult{Error: "gh failed"}}
	if got := exitCode(unpublished, errors.New("publishing the pins failed")); got != exitPinsUnpublished {
		t.Errorf("exitCode(pins unpublished) = %d, want %d", got, exitPinsUnpublished)
	}
	for _, tt := range tests {
		if got := exitCode(&localdeploy.Record{Outcome: tt.outcome}, tt.err); got != tt.want {
			t.Errorf("exitCode(%s, %v) = %d, want %d", tt.outcome, tt.err, got, tt.want)
		}
	}
}

func TestValidateAndUsage(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.json")
	bad := filepath.Join(dir, "bad.json")
	cfg := `{"version":1,"aws":{"region":"us-east-1","account_id":"123456789012"},
"gate":{"command":["make","test"],"timeout":"10m"},
"images":[{"name":"app","context":".","platforms":["linux/amd64"],
"repository":"123456789012.dkr.ecr.us-east-1.amazonaws.com/app","config_key":"app:image"}],
"pulumi":{"work_dir":"infra","stack":"dev"},
"verify":{"timeout":"5m","http":[{"url":"https://example.com/healthz","expect_status":200}]}}`
	if err := os.WriteFile(good, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte(strings.Replace(cfg, `"version":1`, `"version":1,"x":1`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		args []string
		want int
	}{
		{[]string{"-config", good, "validate"}, exitOK},
		{[]string{"-config", bad, "validate"}, exitUsage},
		{[]string{"-config", good}, exitUsage},
		{[]string{"-config", good, "destroy"}, exitUsage},
		{[]string{"-nope"}, exitUsage},
	}
	for _, tt := range tests {
		var out, errOut bytes.Buffer
		if got := run(tt.args, nil, &out, &errOut); got != tt.want {
			t.Errorf("run(%v) = %d, want %d; stderr: %s", tt.args, got, tt.want, errOut.String())
		}
	}
}
