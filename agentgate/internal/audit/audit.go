// Package audit writes every authorization decision to Postgres — the
// permanent record from which "what did agents do, and why was it allowed?"
// can always be reconstructed.
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Entry is one authorization decision.
type Entry struct {
	AgentID       string
	OnBehalfOf    string
	Roles         []string
	Method        string
	Tool          string
	Args          map[string]any
	Decision      string
	Reason        string
	PolicyVersion string
}

// Log is a handle to the audit database.
type Log struct {
	pool *pgxpool.Pool
}

// Connect opens the pool, retrying while Postgres finishes booting (docker
// compose starts everything at once, so the first attempts may fail).
func Connect(ctx context.Context, dsn string) (*Log, error) {
	var pool *pgxpool.Pool
	var err error
	for attempt := 1; attempt <= 15; attempt++ {
		pool, err = pgxpool.New(ctx, dsn)
		if err == nil {
			if err = pool.Ping(ctx); err == nil {
				return &Log{pool: pool}, nil
			}
			pool.Close()
		}
		log.Printf("audit: postgres not ready (attempt %d): %v", attempt, err)
		time.Sleep(2 * time.Second)
	}
	return nil, fmt.Errorf("connect to postgres: %w", err)
}

// Record inserts one decision row and returns its transaction id. Called
// before the decision is returned to the gateway, so nothing the gateway
// acted on is ever missing from the log.
func (l *Log) Record(ctx context.Context, e Entry) (string, error) {
	argsJSON, _ := json.Marshal(e.Args)
	var txID string
	err := l.pool.QueryRow(ctx, `
		INSERT INTO audit_log
			(agent_id, on_behalf_of, roles, method, tool, args,
			 decision, reason, policy_version)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING transaction_id`,
		e.AgentID, e.OnBehalfOf, e.Roles, e.Method, e.Tool, argsJSON,
		e.Decision, e.Reason, e.PolicyVersion,
	).Scan(&txID)
	if err != nil {
		return "", fmt.Errorf("insert audit row: %w", err)
	}
	return txID, nil
}

// Pool exposes the underlying connection pool for other packages that share
// the database (the pending-approvals store in Phase 5).
func (l *Log) Pool() *pgxpool.Pool { return l.pool }
