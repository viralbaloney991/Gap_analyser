package coralogix

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"coralogix-alert-analyzer/internal/models"
)

// Client calls the Coralogix AlertDefsService via grpcurl (server reflection).
type Client struct {
	endpoint string
	apiKey   string
}

// NewClient validates the region and returns a Client.
func NewClient(region, apiKey string) (*Client, error) {
	r, ok := models.Regions[strings.ToLower(region)]
	if !ok {
		return nil, fmt.Errorf("unknown region: %s (valid: eu1, eu2, us1, us2, ap1, ap2, ap3)", region)
	}
	return &Client{endpoint: r.Endpoint, apiKey: apiKey}, nil
}

// Close is a no-op (kept for interface compatibility).
func (c *Client) Close() error { return nil }

// FetchActiveAlerts returns only enabled alert definitions.
func (c *Client) FetchActiveAlerts(ctx context.Context) ([]*models.AlertDef, error) {
	all, err := c.FetchAllAlerts(ctx)
	if err != nil {
		return nil, err
	}
	var active []*models.AlertDef
	for _, a := range all {
		if a.Enabled {
			active = append(active, a)
		}
	}
	return active, nil
}

// FetchAllAlerts calls ListAlertDefs via grpcurl and parses the JSON response.
func (c *Client) FetchAllAlerts(ctx context.Context) ([]*models.AlertDef, error) {
	raw, err := c.grpcCall(ctx, "com.coralogixapis.alerts.v3.AlertDefsService/ListAlertDefs", "{}")
	if err != nil {
		return nil, err
	}

	var resp listAlertDefsResp
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	alerts := make([]*models.AlertDef, 0, len(resp.AlertDefs))
	for _, def := range resp.AlertDefs {
		alerts = append(alerts, def.toModel())
	}
	return alerts, nil
}

// grpcCall shells out to grpcurl and returns the raw JSON output.
func (c *Client) grpcCall(ctx context.Context, method, body string) ([]byte, error) {
	args := []string{
		"-H", "Authorization: Bearer " + c.apiKey,
		"-d", body,
		c.endpoint,
		method,
	}

	cmd := exec.CommandContext(ctx, "grpcurl", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = err.Error()
		}
		return nil, fmt.Errorf("%s: %s", method, errMsg)
	}

	return stdout.Bytes(), nil
}

// FetchAlertEventCounts returns the trigger count for each alert ID over the
// past [days] days. Uses EventsService/ListEventsCount (no pagination).
// Returns a map of alertID → count; IDs not in the response have count 0.
// If the call fails, returns nil, err — the caller falls back to structural-only.
func (c *Client) FetchAlertEventCounts(
	ctx context.Context,
	alertIDs []string,
	days int,
) (map[string]int, error) {
	if len(alertIDs) == 0 {
		return map[string]int{}, nil
	}
	if days <= 0 {
		return nil, fmt.Errorf("FetchAlertEventCounts: days must be positive, got %d", days)
	}

	now := time.Now().UTC()
	from := now.AddDate(0, 0, -days)

	// NOTE: field names use camelCase per protobuf JSON transcoding convention.
	// If the API rejects the request or returns unexpected results, verify the
	// exact field names against the ListEventsCount proto definition.
	type reqBody struct {
		AlertIDs       []string `json:"alertIds"`
		TimestampRange struct {
			From string `json:"from"`
			To   string `json:"to"`
		} `json:"timestampRange"`
	}
	var body reqBody
	body.AlertIDs = alertIDs
	body.TimestampRange.From = from.Format(time.RFC3339)
	body.TimestampRange.To = now.Format(time.RFC3339)

	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal event count request: %w", err)
	}

	raw, err := c.grpcCall(ctx, "com.coralogixapis.events.v3.EventsService/ListEventsCount", string(bodyJSON))
	if err != nil {
		return nil, err
	}
	return parseEventCountResponse(raw)
}

// parseEventCountResponse parses the ListEventsCount JSON response into a
// map of alertID → count. Extracted for testability (avoids grpcurl dependency).
// NOTE: if the real API uses different field names, update listEventsCountResp only.
func parseEventCountResponse(raw []byte) (map[string]int, error) {
	var resp listEventsCountResp
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("parse event count response: %w", err)
	}
	counts := make(map[string]int, len(resp.AlertsEventsCounts))
	for _, entry := range resp.AlertsEventsCounts {
		counts[entry.AlertID] = entry.Count
	}
	return counts, nil
}

// ── JSON response types (mirrors Coralogix gRPC JSON output) ────────

type listAlertDefsResp struct {
	AlertDefs []alertDefJSON `json:"alertDefs"`
}

type alertDefJSON struct {
	ID                 string              `json:"id"`
	AlertVersionID     string              `json:"alertVersionId"`
	AlertDefProperties *alertDefPropsJSON  `json:"alertDefProperties"`
}

type alertDefPropsJSON struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Enabled     bool              `json:"enabled"`
	Priority    string            `json:"priority"`
	Type        string            `json:"type"`
	GroupByKeys []string          `json:"groupByKeys"`
	EntityLabels map[string]string `json:"entityLabels"`
	PhantomMode bool              `json:"phantomMode"`
	Deleted     bool              `json:"deleted"`

	// Type definitions — grpcurl outputs the oneof as a flat field
	LogsImmediate             json.RawMessage `json:"logsImmediate"`
	LogsThreshold             json.RawMessage `json:"logsThreshold"`
	LogsRatioThreshold        json.RawMessage `json:"logsRatioThreshold"`
	LogsTimeRelativeThreshold json.RawMessage `json:"logsTimeRelativeThreshold"`
	LogsAnomaly               json.RawMessage `json:"logsAnomaly"`
	LogsNewValue              json.RawMessage `json:"logsNewValue"`
	LogsUniqueCount           json.RawMessage `json:"logsUniqueCount"`
	TracingImmediate          json.RawMessage `json:"tracingImmediate"`
	TracingThreshold          json.RawMessage `json:"tracingThreshold"`
	MetricThreshold           json.RawMessage `json:"metricThreshold"`
	MetricAnomaly             json.RawMessage `json:"metricAnomaly"`
	Flow                      json.RawMessage `json:"flow"`
}

func (d *alertDefJSON) toModel() *models.AlertDef {
	props := d.AlertDefProperties
	if props == nil {
		return &models.AlertDef{ID: d.ID}
	}

	alert := &models.AlertDef{
		ID:          d.ID,
		Name:        props.Name,
		Description: props.Description,
		Enabled:     props.Enabled,
		Priority:    props.Priority,
		AlertType:   detectAlertType(props),
		GroupByKeys: props.GroupByKeys,
		Labels:      props.EntityLabels,
	}

	alert.TypeDef = extractTypeDef(props)

	return alert
}

func detectAlertType(props *alertDefPropsJSON) string {
	switch {
	case props.LogsImmediate != nil:
		return "logs_immediate"
	case props.LogsThreshold != nil:
		return "logs_threshold"
	case props.LogsRatioThreshold != nil:
		return "logs_ratio_threshold"
	case props.LogsTimeRelativeThreshold != nil:
		return "logs_time_relative_threshold"
	case props.LogsAnomaly != nil:
		return "logs_anomaly"
	case props.LogsNewValue != nil:
		return "logs_new_value"
	case props.LogsUniqueCount != nil:
		return "logs_unique_count"
	case props.TracingImmediate != nil:
		return "tracing_immediate"
	case props.TracingThreshold != nil:
		return "tracing_threshold"
	case props.MetricThreshold != nil:
		return "metric_threshold"
	case props.MetricAnomaly != nil:
		return "metric_anomaly"
	case props.Flow != nil:
		return "flow"
	default:
		return "unknown"
	}
}

// listEventsCountResp mirrors the EventsService/ListEventsCount JSON response.
// Field names are camelCase per protobuf-to-JSON transcoding convention.
// Verify against real API output if counts are always 0.
type listEventsCountResp struct {
	AlertsEventsCounts []struct {
		AlertID string `json:"alertId"`
		Count   int    `json:"count"`
	} `json:"alertsEventsCounts"`
}

func extractTypeDef(props *alertDefPropsJSON) map[string]any {
	var raw json.RawMessage
	switch {
	case props.LogsImmediate != nil:
		raw = props.LogsImmediate
	case props.LogsThreshold != nil:
		raw = props.LogsThreshold
	case props.LogsRatioThreshold != nil:
		raw = props.LogsRatioThreshold
	case props.LogsTimeRelativeThreshold != nil:
		raw = props.LogsTimeRelativeThreshold
	case props.LogsAnomaly != nil:
		raw = props.LogsAnomaly
	case props.LogsNewValue != nil:
		raw = props.LogsNewValue
	case props.LogsUniqueCount != nil:
		raw = props.LogsUniqueCount
	case props.TracingImmediate != nil:
		raw = props.TracingImmediate
	case props.TracingThreshold != nil:
		raw = props.TracingThreshold
	case props.MetricThreshold != nil:
		raw = props.MetricThreshold
	case props.MetricAnomaly != nil:
		raw = props.MetricAnomaly
	case props.Flow != nil:
		raw = props.Flow
	default:
		return nil
	}

	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil
	}
	return result
}
