package localdeploy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// RecordSchemaVersion versions the run record format.
const RecordSchemaVersion = 1

// Outcome is the terminal state of a run.
type Outcome string

// Outcomes, in the order a run can reach them.
const (
	OutcomeRefused        Outcome = "refused"         // stopped before any stack change
	OutcomePlanned        Outcome = "planned"         // plan mode finished
	OutcomeSucceeded      Outcome = "succeeded"       // applied and verified
	OutcomeRolledBack     Outcome = "rolled_back"     // apply or verify failed; previous pins re-applied
	OutcomeRollbackFailed Outcome = "rollback_failed" // restoring the previous pins failed
)

// Record is the machine-readable account of one run. Times come from the
// clock at the moment each step ends; nothing here is estimated.
type Record struct {
	SchemaVersion int       `json:"schema_version"`
	RunID         string    `json:"run_id"`
	Mode          string    `json:"mode"`
	StartedAt     time.Time `json:"started_at"`
	FinishedAt    time.Time `json:"finished_at"`
	Outcome       Outcome   `json:"outcome"`
	Error         string    `json:"error,omitempty"`
	Log           string    `json:"log,omitempty"`

	Source struct {
		SHA   string `json:"sha"`
		Dirty bool   `json:"dirty"`
	} `json:"source"`
	AWS struct {
		Region    string `json:"region"`
		AccountID string `json:"account_id"`
	} `json:"aws"`
	Stack string `json:"stack"`

	Gate      *GateResult     `json:"gate,omitempty"`
	Images    []ImageResult   `json:"images,omitempty"`
	Preview   map[string]int  `json:"preview,omitempty"`
	Confirmed string          `json:"confirmed,omitempty"` // "flag" or "prompt"
	Apply     *StepResult     `json:"apply,omitempty"`
	Verify    []CheckResult   `json:"verify,omitempty"`
	Rollback  *RollbackResult `json:"rollback,omitempty"`
	PinPR     *PinPRResult    `json:"pin_pr,omitempty"`
}

// GateResult is the gate command outcome.
type GateResult struct {
	Command    []string  `json:"command"`
	Passed     bool      `json:"passed"`
	Error      string    `json:"error,omitempty"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
}

// ImageResult is one built, pushed and verified image.
type ImageResult struct {
	Name        string `json:"name"`
	Tag         string `json:"tag"`
	Digest      string `json:"digest"`
	Ref         string `json:"ref"`
	ConfigKey   string `json:"config_key"`
	PreviousRef string `json:"previous_ref,omitempty"`
}

// StepResult is a pass/fail step with its finish time.
type StepResult struct {
	Succeeded  bool      `json:"succeeded"`
	Error      string    `json:"error,omitempty"`
	FinishedAt time.Time `json:"finished_at"`
}

// CheckResult is one verification check.
type CheckResult struct {
	Kind   string    `json:"kind"` // "ecs" or "http"
	Target string    `json:"target"`
	Passed bool      `json:"passed"`
	Detail string    `json:"detail,omitempty"`
	At     time.Time `json:"at"`
}

// PinPRResult is the pin pull request outcome after a verified deploy.
type PinPRResult struct {
	Branch     string    `json:"branch,omitempty"`
	URL        string    `json:"url,omitempty"`
	Error      string    `json:"error,omitempty"`
	FinishedAt time.Time `json:"finished_at"`

	err error
}

// RollbackResult is the restore-and-reapply outcome.
type RollbackResult struct {
	Reason     string        `json:"reason"`
	Succeeded  bool          `json:"succeeded"`
	Error      string        `json:"error,omitempty"`
	Checks     []CheckResult `json:"checks,omitempty"`
	FinishedAt time.Time     `json:"finished_at"`
}

// write stores the record as <dir>/<run-id>.json.
func (r *Record) write(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, r.RunID+".json")
	if err := os.WriteFile(p, append(b, '\n'), 0o640); err != nil {
		return "", fmt.Errorf("write run record: %w", err)
	}
	return p, nil
}
