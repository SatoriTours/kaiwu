package workbench

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"sync"
)

type Store struct {
	mu          sync.Mutex
	db          *sql.DB
	state       State
	subscribers map[chan uint64]struct{}
	closed      bool
}

func id() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func Open(path string, profile Profile) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, subscribers: make(map[chan uint64]struct{})}
	failed := true
	defer func() {
		if failed {
			db.Close()
		}
	}()
	var schema int
	if err = db.QueryRow("PRAGMA user_version").Scan(&schema); err != nil {
		return nil, err
	}
	if schema > 1 {
		return nil, errors.New("database schema is newer than this server")
	}
	if _, err = db.Exec(`PRAGMA busy_timeout=5000; CREATE TABLE IF NOT EXISTS state (id INTEGER PRIMARY KEY CHECK(id=1), revision INTEGER NOT NULL, body BLOB NOT NULL); CREATE TABLE IF NOT EXISTS operations (id TEXT PRIMARY KEY, fingerprint TEXT NOT NULL, receipt BLOB NOT NULL); CREATE TABLE IF NOT EXISTS login_sessions (id_hash TEXT PRIMARY KEY, token_hash TEXT NOT NULL, expires INTEGER NOT NULL); PRAGMA user_version=1;`); err != nil {
		return nil, err
	}
	var body []byte
	err = db.QueryRow("SELECT body FROM state WHERE id=1").Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		s.state = State{Projects: map[string]Project{}, Topics: []Topic{}, Tasks: map[string]Task{}, Conversations: map[string]Conversation{}, Profile: profile}
		body, _ = json.Marshal(s.state)
		_, err = db.Exec("INSERT INTO state VALUES (1,0,?)", body)
	} else if err == nil {
		err = json.Unmarshal(body, &s.state)
	}
	if err != nil {
		return nil, err
	}
	if !s.state.ProfileConfigured {
		s.state.Profile = profile
	}
	if s.state.TaskGit == nil {
		s.state.TaskGit = map[string]GitStatus{}
	}
	if s.state.Merges == nil {
		s.state.Merges = map[string]MergeOperation{}
	}
	if s.state.Executions == nil {
		s.state.Executions = map[string]Execution{}
	}
	// Never replay requests that may already have reached a model or tool.
	if err = s.Update(func(st *State) error {
		for id, op := range st.Merges {
			if MergeActive(op.Phase) {
				op.Phase = "needs_check"
				op.Error = "服务重启，需核对 Git 实际状态；未自动重放"
				st.Merges[id] = op
			}
		}
		for id, x := range st.Executions {
			if x.Phase == "queued" || x.Phase == "preparing" || x.Phase == "starting" || x.Phase == "running" {
				x.Phase = "interrupted"
				x.Error = "服务已重启，未自动重发开发指令"
				st.Executions[id] = x
				t := st.Tasks[x.TaskID]
				if t.ExecutionID == id {
					t.Status = "needs_confirmation"
					st.Tasks[t.ID] = t
				}
			}
		}
		for key, c := range st.Conversations {
			if c.RunState == "starting" || c.RunState == "running" || (c.SessionFile != "" && c.RunState == "idle") {
				neverSent := c.RunState == "starting"
				c.RunState = "interrupted"
				c.Error = "服务已重启，请明确恢复会话；未自动重发消息"
				for i := range c.Messages {
					if c.Messages[i].Delivery == "pending" || c.Messages[i].Delivery == "accepted" {
						c.Messages[i].Delivery = "uncertain"
						if neverSent {
							c.Messages[i].Delivery = "not_sent"
						}
					}
				}
				st.Conversations[key] = c
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	failed = false
	return s, nil
}
func clone(st State) State {
	b, _ := json.Marshal(st)
	var out State
	if e := json.Unmarshal(b, &out); e != nil {
		panic(e)
	}
	return out
}
func (s *Store) Snapshot() State { s.mu.Lock(); defer s.mu.Unlock(); return clone(s.state) }
func (s *Store) persist(next State, operationID, fingerprint string, receipt Receipt) error {
	next.Revision = s.state.Revision + 1
	body, err := json.Marshal(next)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	r, err := tx.Exec("UPDATE state SET revision=?,body=? WHERE id=1 AND revision=?", next.Revision, body, s.state.Revision)
	if err != nil {
		return err
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return Conflict("数据库已被其他实例修改，请停止重复实例")
	}
	if operationID != "" {
		receipt.Revision = next.Revision
		b, _ := json.Marshal(receipt)
		if _, err = tx.Exec("INSERT INTO operations VALUES (?,?,?)", operationID, fingerprint, b); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	s.state = next
	for ch := range s.subscribers {
		select {
		case ch <- next.Revision:
		default:
		}
	}
	return nil
}
func (s *Store) Update(fn func(*State) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("store closed")
	}
	next := clone(s.state)
	if e := fn(&next); e != nil {
		return e
	}
	return s.persist(next, "", "", Receipt{})
}
func (s *Store) Command(c Command) (Receipt, error) {
	return s.PreparedCommand(c, nil)
}

// PreparedCommand validates and deduplicates before preparing external resources.
// Preparation runs under the command lock and must not call back into Store.
// A persistence failure may leave prepared resources; they are never deleted.
func (s *Store) PreparedCommand(c Command, prepare func() error) (Receipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Receipt{}, errors.New("store closed")
	}
	if c.Type == "send_message" && IsStartRequest(c.Text) {
		c.Type = "start_task"
		if c.TaskID == "" {
			c.TaskID = s.state.Conversations[c.ConversationID].TaskID
		}
	}
	if len(c.OperationID) < 3 || len(c.OperationID) > 100 {
		return Receipt{}, Invalid("操作标识无效")
	}
	f := c
	f.Revision = 0
	f.StructureRevision = nil
	b, _ := json.Marshal(f)
	sum := sha256.Sum256(b)
	fingerprint := hex.EncodeToString(sum[:])
	var previous string
	var saved []byte
	err := s.db.QueryRow("SELECT fingerprint,receipt FROM operations WHERE id=?", c.OperationID).Scan(&previous, &saved)
	if err == nil {
		if fingerprint != previous {
			return Receipt{}, Conflict("操作标识已用于不同内容")
		}
		var r Receipt
		err = json.Unmarshal(saved, &r)
		r.Replay = true
		return r, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Receipt{}, err
	}
	stale := c.Revision != s.state.Revision
	if c.StructureRevision != nil {
		stale = *c.StructureRevision != s.state.StructureRevision
	}
	if stale && c.Type != "send_message" && c.Type != "resume" && c.Type != "start_task" {
		return Receipt{}, Conflict("内容已变化，请刷新后重试")
	}
	var count int
	if err = s.db.QueryRow("SELECT count(*) FROM operations").Scan(&count); err != nil {
		return Receipt{}, err
	}
	if count >= 10000 {
		return Receipt{}, Invalid("操作记录已达当前版本上限")
	}
	next := clone(s.state)
	r, err := apply(&next, c)
	if err != nil {
		return Receipt{}, err
	}
	if prepare != nil {
		if err = prepare(); err != nil {
			return Receipt{}, err
		}
	}
	if c.Type != "send_message" && c.Type != "resume" && c.Type != "start_task" {
		next.StructureRevision++
	}
	r.Type = c.Type
	r.Revision = s.state.Revision + 1
	if err = s.persist(next, c.OperationID, fingerprint, r); err != nil {
		return Receipt{}, err
	}
	return r, nil
}
func (s *Store) Operation(key string) (Receipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var b []byte
	if e := s.db.QueryRow("SELECT receipt FROM operations WHERE id=?", key).Scan(&b); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return Receipt{}, Missing()
		}
		return Receipt{}, e
	}
	var r Receipt
	e := json.Unmarshal(b, &r)
	return r, e
}
func (s *Store) Subscribe() (<-chan uint64, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := make(chan uint64, 1)
	ch <- s.state.Revision
	s.subscribers[ch] = struct{}{}
	return ch, func() { s.mu.Lock(); defer s.mu.Unlock(); delete(s.subscribers, ch) }
}
func (s *Store) Close() error { s.mu.Lock(); defer s.mu.Unlock(); s.closed = true; return s.db.Close() }
