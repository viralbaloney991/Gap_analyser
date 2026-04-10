package monday

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

const apiURL = "https://api.monday.com/v2"

// Client queries the Monday.com GraphQL API.
type Client struct {
	apiToken string
	boardID  int64
}

// Integration represents a single onboarded log source from Monday.
type Integration struct {
	Name        string `json:"name"`
	Application string `json:"application"`
	Subsystem   string `json:"subsystem"`
	Status      string `json:"status"`
	AlertCount  int    `json:"alert_count"`
}

// NewClient creates a Monday.com API client.
func NewClient(apiToken string, boardID int64) *Client {
	return &Client{apiToken: apiToken, boardID: boardID}
}

// FetchIntegrations returns Done integrations for a group's "Integrations" item.
func (c *Client) FetchIntegrations(ctx context.Context, groupID string) ([]Integration, error) {
	query := fmt.Sprintf(`{
		boards(ids: [%d]) {
			groups(ids: ["%s"]) {
				items_page(limit: 500) {
					items {
						name
						subitems {
							name
							column_values(ids: ["status", "text3", "text39"]) {
								id
								text
							}
						}
					}
				}
			}
		}
	}`, c.boardID, groupID)

	resp, err := c.graphql(ctx, query)
	if err != nil {
		return nil, err
	}

	var result struct {
		Data struct {
			Boards []struct {
				Groups []struct {
					ItemsPage struct {
						Items []struct {
							Name     string `json:"name"`
							Subitems []struct {
								Name         string `json:"name"`
								ColumnValues []struct {
									ID   string `json:"id"`
									Text string `json:"text"`
								} `json:"column_values"`
							} `json:"subitems"`
						} `json:"items"`
					} `json:"items_page"`
				} `json:"groups"`
			} `json:"boards"`
		} `json:"data"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("parse monday response: %w", err)
	}

	if len(result.Data.Boards) == 0 || len(result.Data.Boards[0].Groups) == 0 {
		return nil, fmt.Errorf("group %q not found on board %d", groupID, c.boardID)
	}

	var integrations []Integration
	items := result.Data.Boards[0].Groups[0].ItemsPage.Items

	for _, item := range items {
		if !strings.EqualFold(item.Name, "Integrations") {
			continue
		}
		for _, sub := range item.Subitems {
			cols := make(map[string]string)
			for _, cv := range sub.ColumnValues {
				if cv.Text != "" {
					cols[cv.ID] = cv.Text
				}
			}

			status := cols["status"]
			if !strings.EqualFold(status, "Done") {
				continue
			}

			integrations = append(integrations, Integration{
				Name:        sub.Name,
				Application: cols["text3"],
				Subsystem:   cols["text39"],
				Status:      status,
			})
		}
		break
	}

	return integrations, nil
}

func (c *Client) graphql(ctx context.Context, query string) ([]byte, error) {
	body, _ := json.Marshal(map[string]string{"query": query})

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", c.apiToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("monday API: %w", err)
	}
	defer resp.Body.Close()

	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("monday API returned %d: %s", resp.StatusCode, buf.String())
	}

	return buf.Bytes(), nil
}
