// Package approval implements the async human-approval flow: parking
// destructive calls, notifying a human, and — after an approval — re-running
// the policy check before the call is finally executed.
package approval

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Pending is one parked tool call awaiting a human decision.
type Pending struct {
	TransactionID string         `json:"transaction_id"`
	AgentID       string         `json:"agent_id"`
	OnBehalfOf    string         `json:"on_behalf_of"`
	Roles         []string       `json:"roles"`
	Tool          string         `json:"tool"`
	Args          map[string]any `json:"args"`
	State         string         `json:"state"`
	DecidedBy     *string        `json:"decided_by,omitempty"`
	Result        *string        `json:"result,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
}

// Store persists parked calls in Postgres (same database as the audit log).
type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Park records a destructive call as pending. transactionID is the audit-log
// transaction id of the original attempt, so the two records stay linked.
func (s *Store) Park(ctx context.Context, p Pending) error {
	argsJSON, _ := json.Marshal(p.Args)
	_, err := s.pool.Exec(ctx, `
		INSERT INTO pending_approvals
			(transaction_id, agent_id, on_behalf_of, roles, tool, args)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		p.TransactionID, p.AgentID, p.OnBehalfOf, p.Roles, p.Tool, argsJSON)
	if err != nil {
		return fmt.Errorf("park transaction: %w", err)
	}
	return nil
}

// Get fetches one parked call by transaction id.
func (s *Store) Get(ctx context.Context, txID string) (*Pending, error) {
	return s.scanOne(s.pool.QueryRow(ctx, `
		SELECT transaction_id, agent_id, on_behalf_of, roles, tool, args,
		       state, decided_by, result, created_at
		FROM pending_approvals WHERE transaction_id = $1`, txID))
}

// ListPending returns calls still waiting for a decision, oldest first.
func (s *Store) ListPending(ctx context.Context) ([]Pending, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT transaction_id, agent_id, on_behalf_of, roles, tool, args,
		       state, decided_by, result, created_at
		FROM pending_approvals WHERE state = 'pending' ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Pending
	for rows.Next() {
		p, err := s.scanOne(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// Decide transitions a pending row to a new state, but ONLY from 'pending' —
// two humans racing to click both buttons cannot double-decide a call.
// Returns false if the row was already decided (or doesn't exist).
func (s *Store) Decide(ctx context.Context, txID, newState, decidedBy string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE pending_approvals
		SET state = $2, decided_by = $3, decided_at = now()
		WHERE transaction_id = $1 AND state = 'pending'`,
		txID, newState, decidedBy)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// SetResult stores the final outcome (tool result, or refusal reason) and
// terminal state ('executed' or 'failed').
func (s *Store) SetResult(ctx context.Context, txID, state, result string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE pending_approvals SET state = $2, result = $3
		WHERE transaction_id = $1`, txID, state, result)
	return err
}

type rowScanner interface{ Scan(dest ...any) error }

func (s *Store) scanOne(row rowScanner) (*Pending, error) {
	var p Pending
	var argsJSON []byte
	if err := row.Scan(&p.TransactionID, &p.AgentID, &p.OnBehalfOf, &p.Roles,
		&p.Tool, &argsJSON, &p.State, &p.DecidedBy, &p.Result, &p.CreatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("no pending approval with that transaction id")
		}
		return nil, err
	}
	_ = json.Unmarshal(argsJSON, &p.Args)
	return &p, nil
}
