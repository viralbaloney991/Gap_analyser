package coralogix

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
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

// eventCountTimestampRange is the time window for event count requests.
type eventCountTimestampRange struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// eventCountReqPagination is the pagination block for ListAlertEvents requests.
type eventCountReqPagination struct {
	PageSize int    `json:"pageSize"`
	Page     string `json:"page,omitempty"`
}

// eventCountReqBody is the request body for ListAlertEvents.
// Field names use proto3 camelCase JSON transcoding (alertIds, timestampRange, pageSize).
type eventCountReqBody struct {
	AlertIDs       []string                 `json:"alertIds"`
	TimestampRange eventCountTimestampRange `json:"timestampRange"`
	Pagination     eventCountReqPagination  `json:"pagination"`
}

// FetchAlertEventCounts returns the trigger count for each alert ID over the
// past [days] days. Uses EventsService/ListAlertEvents in batches of batchSize
// so that high-frequency alerts in one batch don't crowd out others.
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
	counts := make(map[string]int, len(alertIDs))

	const batchSize = 50
	const pageSize = 1000

	batchErrors := 0
	for start := 0; start < len(alertIDs); start += batchSize {
		end := start + batchSize
		if end > len(alertIDs) {
			end = len(alertIDs)
		}
		batch := alertIDs[start:end]

		var nextPage string
		for {
			body := eventCountReqBody{
				AlertIDs: batch,
				TimestampRange: eventCountTimestampRange{
					From: from.Format(time.RFC3339),
					To:   now.Format(time.RFC3339),
				},
				Pagination: eventCountReqPagination{
					PageSize: pageSize,
					Page:     nextPage,
				},
			}

			bodyJSON, err := json.Marshal(body)
			if err != nil {
				return nil, fmt.Errorf("marshal event count request: %w", err)
			}

			raw, err := c.grpcCall(ctx, "com.coralogixapis.events.v3.EventsService/ListAlertEvents", string(bodyJSON))
			if err != nil {
				log.Printf("WARN [noise] batch %d-%d failed: %v", start, end, err)
				batchErrors++
				break
			}

			next, err := parseAlertEventsResponse(raw, counts)
			if err != nil {
				log.Printf("WARN [noise] batch %d-%d parse error: %v", start, end, err)
				batchErrors++
				break
			}
			if next == "" {
				break
			}
			nextPage = next
		}
	}
	// If every batch failed, return nil so callers fall back to structural-only.
	if batchErrors == (len(alertIDs)+batchSize-1)/batchSize {
		return nil, fmt.Errorf("all %d batches failed", batchErrors)
	}

	matched := 0
	for _, c := range counts {
		if c > 0 {
			matched++
		}
	}
	log.Printf("INFO [noise] event counts: requested=%d matched=%d", len(alertIDs), matched)
	return counts, nil
}

// parseAlertEventsResponse counts events per alertId from a ListAlertEvents page.
// Returns the next page token (empty string when done).
func parseAlertEventsResponse(raw []byte, counts map[string]int) (string, error) {
	if len(raw) == 0 || string(raw) == "{}" {
		return "", nil
	}
	var resp listAlertEventsResp
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", fmt.Errorf("parse alert events response: %w", err)
	}
	for _, ev := range resp.Events {
		if id := ev.CxEventPayload.AlertID; id != "" {
			counts[id]++
		}
	}
	return resp.Pagination.NextPage, nil
}

// parseEventCountResponse is kept for existing tests.
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
type listEventsCountResp struct {
	AlertsEventsCounts []struct {
		AlertID string `json:"alertId"`
		Count   int    `json:"count"`
	} `json:"alertsEventsCounts"`
}

// listAlertEventsResp mirrors the EventsService/ListAlertEvents JSON response.
// alertId is nested inside cxEventPayload, not at the top level of each event.
type listAlertEventsResp struct {
	Events []struct {
		CxEventPayload struct {
			AlertID string `json:"alertId"`
		} `json:"cxEventPayload"`
	} `json:"events"`
	Pagination struct {
		NextPage string `json:"nextPage"`
	} `json:"pagination"`
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
