package workbench

import (
	"database/sql"
	"errors"
)

func migrate(db *sql.DB) error {
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > 5 {
		return errors.New("database schema is newer than this server")
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`
 CREATE TABLE IF NOT EXISTS state (id INTEGER PRIMARY KEY CHECK(id=1), revision INTEGER NOT NULL, body BLOB NOT NULL);
 CREATE TABLE IF NOT EXISTS operations (id TEXT PRIMARY KEY, fingerprint TEXT NOT NULL, receipt BLOB NOT NULL);
 CREATE TABLE IF NOT EXISTS login_sessions (id_hash TEXT PRIMARY KEY, token_hash TEXT NOT NULL, expires INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS delivery_policies (project_id TEXT PRIMARY KEY, version INTEGER NOT NULL, body BLOB NOT NULL);
 CREATE TABLE IF NOT EXISTS deliveries (id TEXT PRIMARY KEY, task_id TEXT NOT NULL, cycle INTEGER NOT NULL, active INTEGER NOT NULL, version INTEGER NOT NULL, body BLOB NOT NULL, UNIQUE(task_id,cycle));
 CREATE UNIQUE INDEX IF NOT EXISTS delivery_active_task ON deliveries(task_id) WHERE active=1;
 CREATE TABLE IF NOT EXISTS delivery_steps (id TEXT PRIMARY KEY, delivery_id TEXT NOT NULL, sequence INTEGER NOT NULL, fingerprint TEXT NOT NULL, body BLOB NOT NULL, UNIQUE(delivery_id,sequence));
 CREATE TABLE IF NOT EXISTS delivery_ci (delivery_id TEXT NOT NULL, stage TEXT NOT NULL, workflow_id INTEGER NOT NULL, run_id INTEGER NOT NULL, attempt INTEGER NOT NULL, body BLOB NOT NULL, PRIMARY KEY(delivery_id,stage,workflow_id,run_id,attempt));
 CREATE TABLE IF NOT EXISTS delivery_repairs (id TEXT PRIMARY KEY, delivery_id TEXT NOT NULL, number INTEGER NOT NULL, phase TEXT NOT NULL, body BLOB NOT NULL, UNIQUE(delivery_id,number));
 CREATE TABLE IF NOT EXISTS delivery_queue (ticket INTEGER PRIMARY KEY AUTOINCREMENT, delivery_id TEXT NOT NULL UNIQUE, common_key TEXT NOT NULL, remote_key TEXT NOT NULL, owner INTEGER NOT NULL DEFAULT 0);
 CREATE UNIQUE INDEX IF NOT EXISTS delivery_common_owner ON delivery_queue(common_key) WHERE owner=1;
 CREATE UNIQUE INDEX IF NOT EXISTS delivery_remote_owner ON delivery_queue(remote_key) WHERE owner=1;
 CREATE TABLE IF NOT EXISTS subagent_runs (id TEXT PRIMARY KEY, project_id TEXT NOT NULL, conversation_id TEXT NOT NULL, task_id TEXT NOT NULL, round_id TEXT NOT NULL, parent_session_id TEXT NOT NULL, body BLOB NOT NULL);
 CREATE INDEX IF NOT EXISTS subagent_runs_conversation ON subagent_runs(conversation_id);
 CREATE TABLE IF NOT EXISTS subagent_events (run_id TEXT NOT NULL, sequence INTEGER NOT NULL, event_id TEXT NOT NULL, fingerprint TEXT NOT NULL, body BLOB NOT NULL, PRIMARY KEY(run_id,sequence), UNIQUE(run_id,event_id));
 CREATE TABLE IF NOT EXISTS project_creations (operation_id TEXT PRIMARY KEY, fingerprint TEXT NOT NULL, phase TEXT NOT NULL, body BLOB NOT NULL);
 CREATE TRIGGER IF NOT EXISTS project_creation_reserves_operation BEFORE INSERT ON operations
 WHEN EXISTS(SELECT 1 FROM project_creations WHERE operation_id=NEW.id AND fingerprint!=NEW.fingerprint)
 BEGIN SELECT RAISE(ABORT,'operation id reserved for project creation'); END;
 CREATE TRIGGER IF NOT EXISTS operation_blocks_creation BEFORE INSERT ON project_creations
 WHEN EXISTS(SELECT 1 FROM operations WHERE id=NEW.operation_id)
 BEGIN SELECT RAISE(ABORT,'operation id already used'); END;
 CREATE TABLE IF NOT EXISTS runtime_runs (run_id TEXT PRIMARY KEY, project_id TEXT NOT NULL, conversation_id TEXT NOT NULL, task_id TEXT NOT NULL, kind TEXT NOT NULL, writer INTEGER NOT NULL CHECK(writer IN (0,1)), phase TEXT NOT NULL, version INTEGER NOT NULL CHECK(version>0), body BLOB NOT NULL);
 CREATE UNIQUE INDEX IF NOT EXISTS runtime_pi_conversation ON runtime_runs(conversation_id) WHERE kind='pi' AND phase!='stopped';
 CREATE UNIQUE INDEX IF NOT EXISTS runtime_task_writer ON runtime_runs(project_id,task_id) WHERE writer=1 AND phase!='stopped';
 PRAGMA user_version=5;`)
	if err != nil {
		return err
	}
	return tx.Commit()
}
