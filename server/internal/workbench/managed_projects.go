package workbench

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"

	"github.com/SatoriTours/kaiwu/server/internal/aiconfig"
	"github.com/SatoriTours/kaiwu/server/internal/projectstorage"
)

func (s *Store) rejectCreationOperationLocked(key string) error {
	var pending int
	if err := s.db.QueryRow("SELECT count(*) FROM project_creations WHERE operation_id=?", key).Scan(&pending); err != nil {
		return err
	}
	if pending != 0 {
		return Conflict("操作标识已用于托管项目创建")
	}
	return nil
}

func creationIncomplete(cause error) error {
	return errors.Join(&Error{Code: "project_creation_incomplete", Message: "托管目录创建未完成，原身份和文件已保留；请检查后以原操作重试"}, cause)
}

type PendingProjectCreation struct {
	OperationID string `json:"operationId"`
	ProjectID   string `json:"projectId"`
	Name        string `json:"name"`
	Phase       string `json:"phase"`
}

// PendingProjectCreations exposes only recovery identifiers, never private
// configuration. Listing never retries work; retry requires a new user click.
func (s *Store) PendingProjectCreations() ([]PendingProjectCreation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errors.New("store closed")
	}
	rows, err := s.db.Query("SELECT operation_id,phase,body FROM project_creations WHERE phase!='complete' ORDER BY operation_id LIMIT 100")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PendingProjectCreation{}
	for rows.Next() {
		var item PendingProjectCreation
		var body []byte
		var saved projectCreation
		if err := rows.Scan(&item.OperationID, &item.Phase, &body); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(body, &saved); err != nil {
			return nil, err
		}
		item.ProjectID = saved.Project.ID
		item.Name = saved.Project.Name
		out = append(out, item)
	}
	return out, rows.Err()
}

type projectCreation struct {
	Project      Project      `json:"project"`
	Conversation Conversation `json:"conversation"`
}

// CreateManagedProject persists a reservation before touching the filesystem.
// Failed/incomplete reservations retain their exact project and conversation
// identities. Only an explicit retry of the same request resumes preparation.
// The callback runs under the Store lock and must not call back into Store.
func (s *Store) CreateManagedProject(c Command, root string, prepare func(Project) error) (Receipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Receipt{}, errors.New("store closed")
	}
	if c.Type != "create_project" || len(c.OperationID) < 3 || len(c.OperationID) > 100 ||
		!validText(c.Name, 80) || !projectstorage.ValidDirectoryName(strings.TrimSpace(c.Name)) ||
		!filepath.IsAbs(root) || c.Path != "" || c.ProjectID != "" || c.ConversationID != "" || c.TaskID != "" ||
		c.RemoteURL != "" || c.InitializeGit != "" || prepare == nil {
		return Receipt{}, Invalid("托管项目只需有效名称，不接受客户端路径或 Git 操作")
	}
	f := Command{Type: c.Type, OperationID: c.OperationID, Name: strings.TrimSpace(c.Name)}
	body, _ := json.Marshal(f)
	sum := sha256.Sum256(body)
	fingerprint := hex.EncodeToString(sum[:])
	var previous string
	var saved []byte
	err := s.db.QueryRow("SELECT fingerprint,receipt FROM operations WHERE id=?", c.OperationID).Scan(&previous, &saved)
	if err == nil {
		if previous != fingerprint {
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
	var reservation projectCreation
	err = s.db.QueryRow("SELECT fingerprint,body FROM project_creations WHERE operation_id=?", c.OperationID).Scan(&previous, &saved)
	if err == nil {
		if previous != fingerprint {
			return Receipt{}, Conflict("操作标识已用于不同内容")
		}
		if err = json.Unmarshal(saved, &reservation); err != nil {
			return Receipt{}, err
		}
		// A changed administrator root must not redirect a retry into another tree.
		if reservation.Project.StorageRoot != filepath.Join(filepath.Clean(root), reservation.Project.DirectoryName) {
			return Receipt{}, Conflict("托管根目录已改变，原创建记录保留，需核查后重试")
		}
	} else if errors.Is(err, sql.ErrNoRows) {
		stale := c.Revision != s.state.Revision
		if c.StructureRevision != nil {
			stale = *c.StructureRevision != s.state.StructureRevision
		}
		if stale {
			return Receipt{}, Conflict("内容已变化，请刷新后重试")
		}
		var pending, operations int
		if err = s.db.QueryRow("SELECT count(*) FROM project_creations WHERE phase!='complete'").Scan(&pending); err != nil {
			return Receipt{}, err
		}
		if err = s.db.QueryRow("SELECT count(*) FROM operations").Scan(&operations); err != nil {
			return Receipt{}, err
		}
		if len(s.state.Projects)+pending >= 100 || operations+pending >= 10000 {
			return Receipt{}, Invalid("项目或操作记录已达上限")
		}
		key := id()
		// Always suffix the immutable directory name: concurrent equal display names
		// have deterministic disambiguation and never adopt an existing directory.
		name := strings.TrimSpace(c.Name) + "-" + key[:8]
		layout, err := projectstorage.Resolve(root, name)
		if err != nil {
			return Receipt{}, Invalid("项目名称无法用于安全目录")
		}
		conv := newConversation(key, "", s.state.Profile)
		if s.aiConfigRoot != "" {
			global, _, err := aiconfig.Read(s.aiConfigRoot)
			if err != nil {
				return Receipt{}, err
			}
			resolved, err := aiconfig.Resolve(global, aiconfig.Config{SchemaVersion: 1})
			if err != nil {
				return Receipt{}, err
			}
			if err := aiconfig.BindSkills(s.aiConfigRoot, &resolved); err != nil {
				return Receipt{}, err
			}
			conv.ConfigSnapshot = &resolved
			conv.Profile = resolved.Profile
		}
		reservation = projectCreation{Project: Project{ID: key, Name: strings.TrimSpace(c.Name), Path: layout.Repo, ConversationID: conv.ID,
			StorageMode: "managed", StorageRoot: layout.Root, DirectoryName: name, LayoutVersion: projectstorage.LayoutVersion}, Conversation: conv}
		saved, _ = json.Marshal(reservation)
		if _, err = s.db.Exec("INSERT INTO project_creations(operation_id,fingerprint,phase,body) VALUES(?,?,'preparing',?)", c.OperationID, fingerprint, saved); err != nil {
			return Receipt{}, err
		}
	} else {
		return Receipt{}, err
	}
	if err = prepare(reservation.Project); err != nil {
		// Preserve the intent and files; no cleanup and no automatic replay.
		_, saveErr := s.db.Exec("UPDATE project_creations SET phase='failed' WHERE operation_id=?", c.OperationID)
		return Receipt{}, creationIncomplete(errors.Join(err, saveErr))
	}
	if err := s.freezeConversation(reservation.Project, &reservation.Conversation); err != nil {
		return Receipt{}, creationIncomplete(err)
	}
	next := clone(s.state)
	p := reservation.Project
	next.Projects[p.ID] = p
	next.Conversations[p.ConversationID] = reservation.Conversation
	next.Topics = append(next.Topics, Topic{p.ID + "_other", p.ID, "其他", true})
	next.StructureRevision++
	r := Receipt{Type: c.Type, CreatedID: p.ID, ConversationID: p.ConversationID, Revision: s.state.Revision + 1, Notice: "已创建托管目录；目录归属不代表安全沙箱"}
	err = s.persistWith(next, c.OperationID, fingerprint, r, func(tx *sql.Tx) error {
		_, err := tx.Exec("UPDATE project_creations SET phase='complete' WHERE operation_id=?", c.OperationID)
		return err
	})
	if err != nil {
		return r, creationIncomplete(err)
	}
	return r, nil
}
