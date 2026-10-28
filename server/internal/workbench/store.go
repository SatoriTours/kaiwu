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
	"strings"
	"sync"
	"time"
)

type Store struct {
	mu          sync.Mutex
	db          *sql.DB
	state       State
	subscribers map[chan uint64]struct{}
	closed      bool
	syncEvents  []SyncEvent
	syncBytes   int
	// persistBarrier is a durability test seam: when set it runs before the
	// state write and may fail it, so tests can prove what survives a store
	// failure without changing production behaviour (nil by default).
	persistBarrier func(State) error
	// userTurnHook is notified after a command that starts a user run commits.
	// It is called after the store lock is released and must not call back.
	userTurnHook func(taskID, conversationID string)
}

// SetUserTurnHook installs the delivery manager's user-turn observer. It runs
// after the command lock is released, so the hook may use its own locks/clock.
func (s *Store) SetUserTurnHook(hook func(taskID, conversationID string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.userTurnHook = hook
}

// NotifyUserTurn marks the exact moment a user action resumed AI work. The
// command path calls it after commit; the agent manager calls it after a raw
// native extension response was actually written (native answers never create
// a product round, so they have no command-side user-turn boundary).
func (s *Store) NotifyUserTurn(taskID, conversationID string) {
	s.mu.Lock()
	hook := s.userTurnHook
	s.mu.Unlock()
	if hook != nil && taskID != "" {
		hook(taskID, conversationID)
	}
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
	if err = migrate(db); err != nil {
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
	if s.state.Deliveries == nil {
		s.state.Deliveries = map[string]DeliverySummary{}
	}
	if s.state.DeliveryPolicies == nil {
		s.state.DeliveryPolicies = map[string]DeliveryPolicySummary{}
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
					t.Status = "interrupted"
					st.Tasks[t.ID] = t
				}
			}
		}
		// Normalize legacy records where an interrupted execution was called "needs confirmation".
		for id, t := range st.Tasks {
			if unfinishedExecution(*st, t) && (t.Status == "running" || t.Status == "needs_confirmation") {
				x := st.Executions[t.ExecutionID]
				x.Phase = "interrupted"
				st.Executions[x.ID] = x
				t.Status = "interrupted"
				st.Tasks[id] = t
			}
		}

		for key, c := range st.Conversations {
			for i := range c.Rounds {
				if c.Rounds[i].Phase == "running" {
					c.Rounds[i].Phase = "interrupted"
					c.Rounds[i].FinishedAt = time.Now().UTC().Format(time.RFC3339)
				}
			}
			if c.RunState == "starting" || c.RunState == "running" || (c.SessionFile != "" && c.RunState == "idle") {
				neverSent := c.RunState == "starting"
				c.RunState = "interrupted"
				c.Error = "服务已重启，发送时自动接回原会话；未重发旧消息"
				for i := range c.Messages {
					if c.Messages[i].Delivery == "pending" || c.Messages[i].Delivery == "accepted" {
						c.Messages[i].Delivery = "uncertain"
						if neverSent {
							c.Messages[i].Delivery = "not_sent"
						}
					}
				}
			}
			if c.RunState == "interrupted" {
				c.Error = strings.ReplaceAll(c.Error, "请明确恢复", "发送时自动接回")
			}
			st.Conversations[key] = c
			// Interaction state is normalized after message delivery so an
			// answer never claims to be sent while its message says otherwise.
			NormalizeInteractionsOnRestart(st, key)
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

// SetPersistBarrier installs the durability fault seam used by tests that prove
// which facts survive a failed state write. Passing nil removes it.
func (s *Store) SetPersistBarrier(fn func(State) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.persistBarrier = fn
}

func (s *Store) Snapshot() State { s.mu.Lock(); defer s.mu.Unlock(); return clone(s.state) }
func (s *Store) persist(next State, operationID, fingerprint string, receipt Receipt) error {
	return s.persistWith(next, operationID, fingerprint, receipt, nil)
}
func (s *Store) persistWith(next State, operationID, fingerprint string, receipt Receipt, write func(*sql.Tx) error) error {
	if s.persistBarrier != nil {
		if e := s.persistBarrier(next); e != nil {
			return e
		}
	}
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
	if write != nil {
		if err := write(tx); err != nil {
			return err
		}
	}
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
	s.recordSync(s.state, next)
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
	hook := s.userTurnHook
	var turnTask, turnConv string
	// Registered before the unlock defer (LIFO) so the hook runs after the
	// store lock is released.
	defer func() {
		if turnTask != "" && hook != nil {
			hook(turnTask, turnConv)
		}
	}()
	defer s.mu.Unlock()
	if s.closed {
		return Receipt{}, errors.New("store closed")
	}
	if c.Type == "send_message" && len(c.Attachments) == 0 && IsStartRequest(c.Text) {
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
	if stale && c.Type != "send_message" && c.Type != "resume" && c.Type != "start_task" && !IsInteractionCommand(c.Type) {
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
		// A lazy interaction timeout is the one refused command whose state
		// transition still has to become durable, so the UI never keeps
		// offering an answer that upstream already abandoned.
		if errors.Is(err, errInteractionExpired) {
			if persistErr := s.persist(next, "", "", Receipt{}); persistErr != nil {
				return Receipt{}, persistErr
			}
			return Receipt{}, Conflict("请求已超时，请等待 AI 重新询问")
		}
		return Receipt{}, err
	}
	if (c.Type == "send_message" || c.Type == "start_task" || c.Type == "answer_interaction") && r.CreatedID != "" {
		BeginRound(&next, r.ConversationID, r.CreatedID)
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
	var deliveryWrite func(*sql.Tx) error
	if c.Type == "start_task" {
		deliveryWrite, err = s.prepareDeliveryStart(&next, c, &r)
		if err != nil {
			return Receipt{}, err
		}
	}
	if err = s.persistWith(next, c.OperationID, fingerprint, r, deliveryWrite); err != nil {
		return Receipt{}, err
	}
	if turnCreatesRun(c.Type) && r.CreatedID != "" {
		if conv, ok := next.Conversations[r.ConversationID]; ok && conv.TaskID != "" {
			turnTask, turnConv = conv.TaskID, conv.ID
		}
	}
	return r, nil
}

func turnCreatesRun(typ string) bool {
	switch typ {
	case "send_message", "start_task", "answer_interaction", "retry_interaction_answer":
		return true
	}
	return false
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
