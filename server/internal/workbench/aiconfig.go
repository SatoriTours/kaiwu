package workbench

import (
	"errors"
	"github.com/SatoriTours/kaiwu/server/internal/aiconfig"
	"github.com/SatoriTours/kaiwu/server/internal/projectstorage"
	"path/filepath"
)

type AIConfigView struct {
	Config   aiconfig.Config   `json:"config"`
	Version  string            `json:"version"`
	Resolved aiconfig.Resolved `json:"resolved"`
}

// ConfigureAI bootstraps only declarative defaults, never imports credentials.
// Existing SQLite task/session snapshots remain untouched.
func (s *Store) ConfigureAI(root string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("store closed")
	}
	c, v, err := aiconfig.Read(root)
	if err != nil {
		return err
	}
	if v == "" {
		c = aiconfig.FromProfile(s.state.Profile)
		if _, err = aiconfig.Write(root, "", c); err != nil {
			return err
		}
	}
	resolved, err := aiconfig.Resolve(c, aiconfig.Config{SchemaVersion: 1})
	if err != nil {
		return err
	}
	s.aiConfigRoot = root
	next := clone(s.state)
	next.Profile = resolved.Profile
	next.ProfileConfigured = true
	return s.persist(next, "", "", Receipt{})
}
func (s *Store) aiConfigLocked(projectID string) (AIConfigView, error) {
	if s.closed {
		return AIConfigView{}, errors.New("store closed")
	}
	if s.aiConfigRoot == "" {
		return AIConfigView{}, Invalid("文件配置尚未装配")
	}
	global, gv, err := aiconfig.Read(s.aiConfigRoot)
	if err != nil {
		return AIConfigView{}, err
	}
	project := aiconfig.Config{SchemaVersion: 1}
	v := gv
	c := global
	if projectID != "" {
		p, ok := s.state.Projects[projectID]
		if !ok {
			return AIConfigView{}, Missing()
		}
		if p.StorageMode != "managed" {
			return AIConfigView{}, Invalid("旧项目保留原配置方式，不写入其目录")
		}
		l, err := projectstorage.ResolveProject(p.Storage(), "")
		if err != nil {
			return AIConfigView{}, err
		}
		project, v, err = aiconfig.Read(l.Config)
		if err != nil {
			return AIConfigView{}, err
		}
		c = project
	}
	resolved, err := aiconfig.Resolve(global, project)
	return AIConfigView{Config: c, Version: v, Resolved: resolved}, err
}
func (s *Store) AIConfig(projectID string) (AIConfigView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.aiConfigLocked(projectID)
}
func (s *Store) SaveAIConfig(projectID, version string, c aiconfig.Config) (AIConfigView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.aiConfigLocked(projectID)
	if err != nil {
		return AIConfigView{}, err
	}
	if current.Version != version {
		return AIConfigView{}, aiconfig.ErrConflict
	}
	dir := s.aiConfigRoot
	if projectID == "" {
		resolved, err := aiconfig.Resolve(c, aiconfig.Config{SchemaVersion: 1})
		if err != nil {
			return AIConfigView{}, err
		}
		if err := aiconfig.BindSkills(s.aiConfigRoot, &resolved); err != nil {
			return AIConfigView{}, err
		}
	} else {
		global, _, err := aiconfig.Read(s.aiConfigRoot)
		if err != nil {
			return AIConfigView{}, err
		}
		resolved, err := aiconfig.Resolve(global, c)
		if err != nil {
			return AIConfigView{}, err
		}
		if err := aiconfig.BindSkills(s.aiConfigRoot, &resolved); err != nil {
			return AIConfigView{}, err
		}
		l, err := projectstorage.ResolveProject(s.state.Projects[projectID].Storage(), "")
		if err != nil {
			return AIConfigView{}, err
		}
		dir = l.Config
	}
	if _, err := aiconfig.Write(dir, version, c); err != nil {
		return AIConfigView{}, err
	}
	// SQLite profile is a display projection only; task creation resolves files.
	if projectID == "" {
		view, err := s.aiConfigLocked("")
		if err != nil {
			return AIConfigView{}, err
		}
		next := clone(s.state)
		next.Profile = view.Resolved.Profile
		next.ProfileConfigured = true
		if err := s.persist(next, "", "", Receipt{}); err != nil {
			return AIConfigView{}, err
		}
	}
	return s.aiConfigLocked(projectID)
}
func (s *Store) resolvedFor(p Project) (aiconfig.Resolved, error) {
	global, _, err := aiconfig.Read(s.aiConfigRoot)
	if err != nil {
		return aiconfig.Resolved{}, err
	}
	project := aiconfig.Config{SchemaVersion: 1}
	if p.StorageMode == "managed" {
		l, err := projectstorage.ResolveProject(p.Storage(), "")
		if err != nil {
			return aiconfig.Resolved{}, err
		}
		project, _, err = aiconfig.Read(l.Config)
		if err != nil {
			return aiconfig.Resolved{}, err
		}
	}
	resolved, err := aiconfig.Resolve(global, project)
	if err != nil {
		return resolved, err
	}
	if err := aiconfig.BindSkills(s.aiConfigRoot, &resolved); err != nil {
		return resolved, err
	}
	return resolved, nil
}
func (s *Store) freezeConversation(p Project, c *Conversation) error {
	if s.aiConfigRoot == "" {
		return nil
	}
	if c.ConfigSnapshot == nil {
		resolved, err := s.resolvedFor(p)
		if err != nil {
			return err
		}
		c.ConfigSnapshot = &resolved
		c.Profile = resolved.Profile
	}
	l, err := projectstorage.ResolveProject(p.Storage(), s.dataDir)
	if err != nil {
		return err
	}
	return aiconfig.Freeze(l.Snapshots, c.ID, *c.ConfigSnapshot)
}
func (s *Store) configureCommand(next *State, c Command, r Receipt) error {
	if s.aiConfigRoot == "" {
		return nil
	}
	if c.Type == "set_default_profile" {
		config, version, err := aiconfig.Read(s.aiConfigRoot)
		if err != nil {
			return err
		}
		config.Profile = aiconfig.FromProfile(next.Profile).Profile
		source := next.Profile.CredentialSource
		config.CredentialRef = &source
		if _, err := aiconfig.Resolve(config, aiconfig.Config{SchemaVersion: 1}); err != nil {
			return err
		}
		_, err = aiconfig.Write(s.aiConfigRoot, version, config)
		return err
	}
	var convID, projectID string
	switch c.Type {
	case "add_project":
		projectID = r.CreatedID
		convID = next.Projects[projectID].ConversationID
	case "add_task":
		projectID = c.ProjectID
		convID = next.Tasks[r.CreatedID].ConversationID
	default:
		return nil
	}
	conv := next.Conversations[convID]
	if err := s.freezeConversation(next.Projects[projectID], &conv); err != nil {
		return err
	}
	next.Conversations[conv.ID] = conv
	if conv.TaskID != "" {
		task := next.Tasks[conv.TaskID]
		task.ConfigSnapshot = conv.ConfigSnapshot
		task.Profile = conv.Profile
		next.Tasks[task.ID] = task
	}
	return nil
}

// SnapshotPath is derived solely from server-owned project/conversation IDs.
func SnapshotPath(p Project, conversationID string) (string, error) {
	l, err := projectstorage.ResolveProject(p.Storage(), "")
	if err != nil {
		return "", err
	}
	if !projectstorage.ValidDirectoryName(conversationID) {
		return "", Invalid("invalid conversation identity")
	}
	return filepath.Join(l.Snapshots, conversationID+".json"), nil
}
