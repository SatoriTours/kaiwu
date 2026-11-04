package workbench

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"

	"github.com/SatoriTours/kaiwu/server/internal/executionenv"
	"modernc.org/sqlite"
)

const maxActiveRuntimes = 1024

func runtimeDBError(err error) error {
	var e *sqlite.Error
	if errors.As(err, &e) {
		switch e.Code() & 255 {
		case 5, 6, 19:
			return executionenv.ErrConflict
		}
	}
	return executionenv.ErrUnavailable
}
func runtimeOwnerValid(st State, r executionenv.Record) bool {
	p, ok := st.Projects[r.Owner.ProjectID]
	if !ok || p.ID != r.Owner.ProjectID || p.StorageMode != "managed" {
		return false
	}
	c, ok := st.Conversations[r.Owner.ConversationID]
	if !ok || c.ID != r.Owner.ConversationID || c.ProjectID != p.ID || c.TaskID != r.Owner.TaskID {
		return false
	}
	if r.Owner.TaskID != "" {
		t, ok := st.Tasks[r.Owner.TaskID]
		if !ok || t.ID != r.Owner.TaskID || t.ProjectID != p.ID || t.ConversationID != c.ID || t.ExecutionID != r.Owner.ExecutionID {
			return false
		}
		e, ok := st.Executions[r.Owner.ExecutionID]
		if !ok || e.ID != r.Owner.ExecutionID || e.TaskID != t.ID || e.ConversationID != c.ID {
			return false
		}
	}
	return true
}
func validateRuntimeOwnersTx(tx *sql.Tx, next State) error {
	rows, err := tx.Query("SELECT body FROM runtime_runs WHERE phase!='stopped'")
	if err != nil {
		return runtimeDBError(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		if count > maxActiveRuntimes {
			return executionenv.ErrUnavailable
		}
		var body []byte
		if rows.Scan(&body) != nil {
			return executionenv.ErrUnavailable
		}
		var r executionenv.Record
		if json.Unmarshal(body, &r) != nil || executionenv.ValidateRecord(r) != nil {
			return executionenv.ErrUnavailable
		}
		if !runtimeOwnerValid(next, r) {
			return executionenv.ErrConflict
		}
	}
	if rows.Err() != nil {
		return executionenv.ErrUnavailable
	}
	return nil
}
func scanRuntime(row interface{ Scan(...any) error }) (executionenv.Record, error) {
	var r executionenv.Record
	var key, phase string
	var version uint64
	var body []byte
	if err := row.Scan(&key, &phase, &version, &body); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return r, err
		}
		return r, runtimeDBError(err)
	}
	if json.Unmarshal(body, &r) != nil || r.Identity.RunID != key || string(r.Phase) != phase || r.Version != version || version == 0 || executionenv.ValidateRecord(r) != nil {
		return executionenv.Record{}, executionenv.ErrUnavailable
	}
	return r, nil
}
func sameRuntimeReservation(a, b executionenv.Record) bool {
	return a.Owner == b.Owner && a.SpecHash == b.SpecHash && a.Kind == b.Kind && a.Writer == b.Writer && a.Budget == b.Budget && a.Identity.RunID == b.Identity.RunID && a.Identity.BootID == b.Identity.BootID && a.Identity.Unit == b.Identity.Unit
}

// runtimeTx obtains the SQLite write reservation before reading authoritative
// owners, avoiding a stale State cache or deferred-transaction lock upgrade.
// This is metadata only: no prompt, program, credentials or external launch.
func (s *Store) runtimeTx(ctx context.Context) (*sql.Tx, error) {
	if s.closed {
		return nil, executionenv.ErrUnavailable
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, runtimeDBError(err)
	}
	if _, err = tx.ExecContext(ctx, "UPDATE state SET revision=revision WHERE id=1"); err != nil {
		tx.Rollback()
		return nil, runtimeDBError(err)
	}
	return tx, nil
}
func (s *Store) ReserveRuntime(ctx context.Context, r executionenv.Record) (executionenv.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if executionenv.ValidateRecord(r) != nil || r.Phase != executionenv.Starting || r.Version != 0 || r.LaunchSequence != 0 || r.Identity.InvocationID != "" || r.RendezvousInode != 0 || r.RendezvousDevice != 0 {
		return executionenv.Record{}, executionenv.ErrInvalid
	}
	tx, err := s.runtimeTx(ctx)
	if err != nil {
		return executionenv.Record{}, err
	}
	defer tx.Rollback()
	saved, err := scanRuntime(tx.QueryRowContext(ctx, "SELECT run_id,phase,version,body FROM runtime_runs WHERE run_id=?", r.Identity.RunID))
	if err == nil {
		if !sameRuntimeReservation(saved, r) {
			return executionenv.Record{}, executionenv.ErrConflict
		}
		return saved, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return executionenv.Record{}, err
	}
	var stateBody []byte
	var st State
	if tx.QueryRowContext(ctx, "SELECT body FROM state WHERE id=1").Scan(&stateBody) != nil || json.Unmarshal(stateBody, &st) != nil {
		return executionenv.Record{}, executionenv.ErrUnavailable
	}
	if !runtimeOwnerValid(st, r) {
		return executionenv.Record{}, executionenv.ErrInvalid
	}
	var count int
	if tx.QueryRowContext(ctx, "SELECT count(*) FROM runtime_runs WHERE phase!='stopped'").Scan(&count) != nil {
		return executionenv.Record{}, executionenv.ErrUnavailable
	}
	if count >= maxActiveRuntimes {
		return executionenv.Record{}, executionenv.ErrUnavailable
	}
	r.Version = 1
	body, err := json.Marshal(r)
	if err != nil {
		return executionenv.Record{}, executionenv.ErrInvalid
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO runtime_runs(run_id,project_id,conversation_id,task_id,kind,writer,phase,version,body) VALUES(?,?,?,?,?,?,?,?,?)", r.Identity.RunID, r.Owner.ProjectID, r.Owner.ConversationID, r.Owner.TaskID, r.Kind, r.Writer, r.Phase, r.Version, body)
	if err != nil {
		return executionenv.Record{}, runtimeDBError(err)
	}
	if err = tx.Commit(); err != nil {
		return executionenv.Record{}, runtimeDBError(err)
	}
	return r, nil
}
func (s *Store) AdvanceRuntime(ctx context.Context, next executionenv.Record, proof *executionenv.Observation) (executionenv.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if executionenv.ValidateRecord(next) != nil || next.Version == 0 || next.Version >= math.MaxInt64 {
		return executionenv.Record{}, executionenv.ErrInvalid
	}
	tx, err := s.runtimeTx(ctx)
	if err != nil {
		return executionenv.Record{}, err
	}
	defer tx.Rollback()
	old, err := scanRuntime(tx.QueryRowContext(ctx, "SELECT run_id,phase,version,body FROM runtime_runs WHERE run_id=?", next.Identity.RunID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return executionenv.Record{}, executionenv.ErrConflict
		}
		return executionenv.Record{}, err
	}
	if err = executionenv.ValidateTransition(old, next, proof); err != nil {
		return executionenv.Record{}, err
	}
	next.Version++
	body, err := json.Marshal(next)
	if err != nil {
		return executionenv.Record{}, executionenv.ErrInvalid
	}
	result, err := tx.ExecContext(ctx, "UPDATE runtime_runs SET phase=?,version=?,body=? WHERE run_id=? AND version=?", next.Phase, next.Version, body, next.Identity.RunID, old.Version)
	if err != nil {
		return executionenv.Record{}, runtimeDBError(err)
	}
	n, err := result.RowsAffected()
	if err != nil || n != 1 {
		return executionenv.Record{}, executionenv.ErrConflict
	}
	if err = tx.Commit(); err != nil {
		return executionenv.Record{}, runtimeDBError(err)
	}
	return next, nil
}
func (s *Store) Runtime(ctx context.Context, key string) (executionenv.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return executionenv.Record{}, executionenv.ErrUnavailable
	}
	return scanRuntime(s.db.QueryRowContext(ctx, "SELECT run_id,phase,version,body FROM runtime_runs WHERE run_id=?", key))
}
func (s *Store) ActiveRuntimes(ctx context.Context) ([]executionenv.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, executionenv.ErrUnavailable
	}
	rows, err := s.db.QueryContext(ctx, "SELECT run_id,phase,version,body FROM runtime_runs WHERE phase!='stopped' ORDER BY run_id LIMIT ?", maxActiveRuntimes+1)
	if err != nil {
		return nil, runtimeDBError(err)
	}
	defer rows.Close()
	out := []executionenv.Record{}
	for rows.Next() {
		if len(out) >= maxActiveRuntimes {
			return nil, executionenv.ErrUnavailable
		}
		r, err := scanRuntime(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err = rows.Err(); err != nil {
		return nil, runtimeDBError(err)
	}
	return out, nil
}
