package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"coralogix-alert-analyzer/internal/models"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store wraps a NeonDB (Postgres) connection pool for alert persistence.
type Store struct {
	pool *pgxpool.Pool
}

// New connects to NeonDB, pings, and runs CREATE TABLE IF NOT EXISTS migrations.
func New(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.New: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("neondb ping: %w", err)
	}

	s := &Store{pool: pool}
	if err := s.migrate(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("neondb migrate: %w", err)
	}

	log.Printf("INFO [store] connected to NeonDB")
	return s, nil
}

// Close shuts down the connection pool.
func (s *Store) Close() {
	s.pool.Close()
}

// migrate creates tables if they don't exist. Safe to call on every startup.
func (s *Store) migrate(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS client_alerts (
			client     TEXT        NOT NULL,
			alert_id   TEXT        NOT NULL,
			data       JSONB       NOT NULL,
			fetched_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (client, alert_id)
		);
		CREATE TABLE IF NOT EXISTS sync_state (
			client      TEXT        PRIMARY KEY,
			last_synced TIMESTAMPTZ NOT NULL
		);
		CREATE TABLE IF NOT EXISTS suggestion_cache (
			id           BIGSERIAL   PRIMARY KEY,
			cache_key    TEXT        NOT NULL,
			technique_id TEXT        NOT NULL,
			log_sources  TEXT[]      NOT NULL,
			suggestions  JSONB       NOT NULL,
			provider     TEXT        NOT NULL,
			generated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS suggestion_cache_key_idx ON suggestion_cache(cache_key);
		CREATE TABLE IF NOT EXISTS correlation_cache (
			id           BIGSERIAL   PRIMARY KEY,
			cache_key    TEXT        NOT NULL,
			client       TEXT        NOT NULL,
			suggestions  JSONB       NOT NULL,
			provider     TEXT        NOT NULL,
			generated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS correlation_cache_key_idx ON correlation_cache(cache_key);
		CREATE TABLE IF NOT EXISTS saved_detections (
			id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			client         TEXT        NOT NULL,
			source         TEXT        NOT NULL CHECK (source IN ('builder', 'suggestions')),
			title          TEXT        NOT NULL,
			technique_id   TEXT        NOT NULL,
			tactic         TEXT        NOT NULL,
			lucene_query   TEXT        NOT NULL,
			sigma_rule     TEXT        NOT NULL,
			severity       TEXT        NOT NULL CHECK (severity IN ('critical', 'high', 'medium', 'low')),
			log_source     TEXT        NOT NULL,
			falsepositives TEXT[]      NOT NULL DEFAULT '{}',
			created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS saved_detections_client_idx    ON saved_detections (client);
		CREATE INDEX IF NOT EXISTS saved_detections_technique_idx ON saved_detections (technique_id);
		CREATE INDEX IF NOT EXISTS saved_detections_created_idx   ON saved_detections (created_at DESC);
	`)
	return err
}

// LoadAlerts returns all stored alerts for a client.
// Returns an empty (non-nil) slice if the client has no stored alerts.
func (s *Store) LoadAlerts(ctx context.Context, client string) ([]*models.AlertDef, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT data FROM client_alerts WHERE client = $1`, client)
	if err != nil {
		return nil, fmt.Errorf("query client_alerts: %w", err)
	}
	defer rows.Close()

	var alerts []*models.AlertDef
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		var alert models.AlertDef
		if err := json.Unmarshal(data, &alert); err != nil {
			return nil, fmt.Errorf("unmarshal alert: %w", err)
		}
		alerts = append(alerts, &alert)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	if alerts == nil {
		alerts = []*models.AlertDef{}
	}
	return alerts, nil
}

// UpsertAlerts replaces all stored alerts for a client with the given set.
// Runs in a transaction: deletes existing rows first, then inserts the new set.
// This ensures alerts deleted in Coralogix are removed from the store.
func (s *Store) UpsertAlerts(ctx context.Context, client string, alerts []*models.AlertDef) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) // no-op if already committed

	if _, err := tx.Exec(ctx, `DELETE FROM client_alerts WHERE client = $1`, client); err != nil {
		return fmt.Errorf("delete existing alerts: %w", err)
	}

	if len(alerts) == 0 {
		return tx.Commit(ctx)
	}

	batch := &pgx.Batch{}
	for _, alert := range alerts {
		data, err := json.Marshal(alert)
		if err != nil {
			return fmt.Errorf("marshal alert %s: %w", alert.ID, err)
		}
		batch.Queue(`
			INSERT INTO client_alerts (client, alert_id, data, fetched_at)
			VALUES ($1, $2, $3, NOW())
		`, client, alert.ID, string(data))
	}

	results := tx.SendBatch(ctx, batch)
	for range alerts {
		if _, err := results.Exec(); err != nil {
			results.Close()
			return fmt.Errorf("insert batch exec: %w", err)
		}
	}
	results.Close()

	return tx.Commit(ctx)
}

// GetLastSynced returns when client was last synced.
// ok=false if this client has never been synced.
func (s *Store) GetLastSynced(ctx context.Context, client string) (time.Time, bool, error) {
	var t time.Time
	err := s.pool.QueryRow(ctx,
		`SELECT last_synced FROM sync_state WHERE client = $1`, client).Scan(&t)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("query sync_state: %w", err)
	}
	return t, true, nil
}

// SetLastSynced records a successful sync time for client.
func (s *Store) SetLastSynced(ctx context.Context, client string, t time.Time) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO sync_state (client, last_synced)
		VALUES ($1, $2)
		ON CONFLICT (client)
		DO UPDATE SET last_synced = EXCLUDED.last_synced
	`, client, t)
	if err != nil {
		return fmt.Errorf("upsert sync_state: %w", err)
	}
	return nil
}

// SuggestionRow is one generation of LLM suggestions for a (technique, log_sources) pair.
// Suggestions is a raw JSON array — kept as json.RawMessage to avoid import cycles with the llm package.
type SuggestionRow struct {
	CacheKey    string
	TechniqueID string
	LogSources  []string
	Suggestions json.RawMessage // serialised []llm.Suggestion
	Provider    string
	GeneratedAt time.Time
}

// GetCachedSuggestions returns all suggestion rows for a cache key ordered ASC by generated_at.
// Returns an empty (non-nil) slice when no rows exist.
func (s *Store) GetCachedSuggestions(ctx context.Context, cacheKey string) ([]SuggestionRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT cache_key, technique_id, log_sources, suggestions, provider, generated_at
		FROM suggestion_cache
		WHERE cache_key = $1
		ORDER BY generated_at ASC
	`, cacheKey)
	if err != nil {
		return nil, fmt.Errorf("query suggestion_cache: %w", err)
	}
	defer rows.Close()

	var result []SuggestionRow
	for rows.Next() {
		var row SuggestionRow
		var suggestions []byte
		if err := rows.Scan(
			&row.CacheKey, &row.TechniqueID, &row.LogSources,
			&suggestions, &row.Provider, &row.GeneratedAt,
		); err != nil {
			return nil, fmt.Errorf("scan suggestion_cache row: %w", err)
		}
		row.Suggestions = json.RawMessage(suggestions)
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("suggestion_cache rows error: %w", err)
	}
	if result == nil {
		result = []SuggestionRow{}
	}
	return result, nil
}

// AppendCachedSuggestions inserts one new suggestion generation row.
// Existing rows are never modified — the table is append-only.
func (s *Store) AppendCachedSuggestions(ctx context.Context, row SuggestionRow) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO suggestion_cache (cache_key, technique_id, log_sources, suggestions, provider, generated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, row.CacheKey, row.TechniqueID, row.LogSources, string(row.Suggestions), row.Provider, row.GeneratedAt)
	if err != nil {
		return fmt.Errorf("insert suggestion_cache: %w", err)
	}
	return nil
}

// CorrelationRow is one generation of LLM correlation suggestions for a (client, gap_prose) pair.
type CorrelationRow struct {
	CacheKey    string
	Client      string
	Suggestions json.RawMessage // serialised []models.CorrelationSuggestion
	Provider    string
	GeneratedAt time.Time
}

// GetCachedCorrelations returns all correlation rows for a cache key ordered ASC by generated_at.
// Returns an empty (non-nil) slice when no rows exist.
func (s *Store) GetCachedCorrelations(ctx context.Context, cacheKey string) ([]CorrelationRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT cache_key, client, suggestions, provider, generated_at
		FROM correlation_cache
		WHERE cache_key = $1
		ORDER BY generated_at ASC
	`, cacheKey)
	if err != nil {
		return nil, fmt.Errorf("query correlation_cache: %w", err)
	}
	defer rows.Close()

	var result []CorrelationRow
	for rows.Next() {
		var row CorrelationRow
		var suggestions []byte
		if err := rows.Scan(&row.CacheKey, &row.Client, &suggestions, &row.Provider, &row.GeneratedAt); err != nil {
			return nil, fmt.Errorf("scan correlation_cache row: %w", err)
		}
		row.Suggestions = json.RawMessage(suggestions)
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("correlation_cache rows error: %w", err)
	}
	if result == nil {
		result = []CorrelationRow{}
	}
	return result, nil
}

// SavedDetection is a persisted detection from the Builder or Suggestions panel.
type SavedDetection struct {
	ID             string    `json:"id"`
	Client         string    `json:"client"`
	Source         string    `json:"source"` // "builder" | "suggestions"
	Title          string    `json:"title"`
	TechniqueID    string    `json:"technique_id"`
	Tactic         string    `json:"tactic"`
	LuceneQuery    string    `json:"lucene_query"`
	SigmaRule      string    `json:"sigma_rule"`
	Severity       string    `json:"severity"`
	LogSource      string    `json:"log_source"`
	Falsepositives []string  `json:"falsepositives,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// DetectionFilter controls which saved detections ListDetections returns.
type DetectionFilter struct {
	Client      string // empty = all clients
	TechniqueID string // empty = all techniques
	Severity    string // empty = all severities
	Limit       int    // 0 = use default (100)
}

// AppendCachedCorrelations inserts one new correlation generation row.
// Existing rows are never modified — the table is append-only.
func (s *Store) AppendCachedCorrelations(ctx context.Context, row CorrelationRow) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO correlation_cache (cache_key, client, suggestions, provider, generated_at)
		VALUES ($1, $2, $3, $4, $5)
	`, row.CacheKey, row.Client, string(row.Suggestions), row.Provider, row.GeneratedAt)
	if err != nil {
		return fmt.Errorf("insert correlation_cache: %w", err)
	}
	return nil
}

// SaveDetection inserts a new detection into saved_detections and returns its UUID string.
func (s *Store) SaveDetection(ctx context.Context, d SavedDetection) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx, `
		INSERT INTO saved_detections
			(client, source, title, technique_id, tactic, lucene_query, sigma_rule, severity, log_source, falsepositives)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id::text
	`, d.Client, d.Source, d.Title, d.TechniqueID, d.Tactic,
		d.LuceneQuery, d.SigmaRule, d.Severity, d.LogSource, d.Falsepositives,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("insert saved_detections: %w", err)
	}
	return id, nil
}

// ListDetections returns saved detections matching the filter, newest first.
func (s *Store) ListDetections(ctx context.Context, f DetectionFilter) ([]SavedDetection, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, client, source, title, technique_id, tactic,
		       lucene_query, sigma_rule, severity, log_source, falsepositives, created_at
		FROM saved_detections
		WHERE ($1 = '' OR client = $1)
		  AND ($2 = '' OR technique_id = $2)
		  AND ($3 = '' OR severity = $3)
		ORDER BY created_at DESC
		LIMIT $4
	`, f.Client, f.TechniqueID, f.Severity, limit)
	if err != nil {
		return nil, fmt.Errorf("query saved_detections: %w", err)
	}
	defer rows.Close()

	var result []SavedDetection
	for rows.Next() {
		var d SavedDetection
		if err := rows.Scan(
			&d.ID, &d.Client, &d.Source, &d.Title, &d.TechniqueID, &d.Tactic,
			&d.LuceneQuery, &d.SigmaRule, &d.Severity, &d.LogSource,
			&d.Falsepositives, &d.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan saved_detection: %w", err)
		}
		result = append(result, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("saved_detections rows error: %w", err)
	}
	if result == nil {
		result = []SavedDetection{}
	}
	return result, nil
}

// DeleteDetection removes a saved detection by UUID string. Idempotent.
func (s *Store) DeleteDetection(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM saved_detections WHERE id = $1::uuid`, id)
	if err != nil {
		return fmt.Errorf("delete saved_detection %s: %w", id, err)
	}
	return nil
}

// GetDetection fetches a single detection by UUID string. Returns nil, nil when not found.
func (s *Store) GetDetection(ctx context.Context, id string) (*SavedDetection, error) {
	var d SavedDetection
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, client, source, title, technique_id, tactic,
		       lucene_query, sigma_rule, severity, log_source, falsepositives, created_at
		FROM saved_detections WHERE id = $1::uuid
	`, id).Scan(
		&d.ID, &d.Client, &d.Source, &d.Title, &d.TechniqueID, &d.Tactic,
		&d.LuceneQuery, &d.SigmaRule, &d.Severity, &d.LogSource,
		&d.Falsepositives, &d.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get saved_detection %s: %w", id, err)
	}
	return &d, nil
}
