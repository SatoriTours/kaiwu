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
	if version > 2 {
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
 PRAGMA user_version=2;`)
	if err != nil {
		return err
	}
	return tx.Commit()
}
