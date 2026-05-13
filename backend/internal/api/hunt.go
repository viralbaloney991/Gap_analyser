package api

import (
	"errors"
	"regexp"
)

// ── Data models ──────────────────────────────────────────────────────────────

type huntLogEvent struct {
	Timestamp string `json:"timestamp"`
	Host      string `json:"host"`
	User      string `json:"user"`
	Command   string `json:"command"`
}

type queryDoneData struct {
	Hits         int            `json:"hits"`
	Hosts        int            `json:"hosts"`
	LastSeen     string         `json:"last_seen"`
	UniqueUsers  int            `json:"unique_users"`
	SampleEvents []huntLogEvent `json:"sample_events"`
	CxCommand    string         `json:"cx_command"`
}

type ollyDoneData struct {
	Sections map[string]string `json:"sections"`
}

type huntFinding struct {
	Text     string `json:"text"`
	Severity string `json:"severity"`
}

type huntAction struct {
	Priority    int    `json:"priority"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Level       string `json:"level"`
}

type huntAlertDef struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Condition string `json:"condition"`
	Severity  string `json:"severity"`
	GroupBy   string `json:"group_by"`
}

type huntStats struct {
	Hits         string `json:"hits"`
	Hosts        string `json:"hosts"`
	AttackWindow string `json:"attack_window"`
	C2Flagged    bool   `json:"c2_flagged"`
}

type huntReport struct {
	Verdict       string        `json:"verdict"`
	Confidence    string        `json:"confidence"`
	Title         string        `json:"title"`
	Subtitle      string        `json:"subtitle"`
	Stats         huntStats     `json:"stats"`
	Findings      []huntFinding `json:"findings"`
	Actions       []huntAction  `json:"actions"`
	AlertDef      huntAlertDef  `json:"alert_def"`
	RunDurationMs int64         `json:"run_duration_ms"`
	Timestamp     string        `json:"timestamp"`
}

type huntErrorData struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ── Input sanitization ───────────────────────────────────────────────────────

var queryAllowlist = regexp.MustCompile(`^[\x20-\x7E]+$`)
var queryForbidden = regexp.MustCompile(`[$` + "`" + `;\|\n\r]`)

func sanitizeQuery(q string) error {
	if len(q) > 1000 {
		return errors.New("query exceeds 1000 character limit")
	}
	if !queryAllowlist.MatchString(q) {
		return errors.New("query contains non-printable characters")
	}
	if queryForbidden.MatchString(q) {
		return errors.New("query contains forbidden characters")
	}
	return nil
}
