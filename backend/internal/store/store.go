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
