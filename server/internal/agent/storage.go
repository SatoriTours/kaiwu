package agent

import (
	"github.com/SatoriTours/kaiwu/server/internal/projectstorage"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
	"path/filepath"
)

func (m *Manager) projectLayout(projectID string) (projectstorage.Layout, error) {
	p, ok := m.store.Snapshot().Projects[projectID]
	if !ok {
		return projectstorage.Layout{}, workbench.Missing()
	}
	return projectstorage.ResolveProject(p.Storage(), m.cfg.DataDir)
}
func (m *Manager) sessionRoot(c workbench.Conversation) (string, error) {
	if !projectstorage.ValidDirectoryName(c.ID) {
		return "", workbench.Invalid("invalid conversation identity")
	}
	l, err := m.projectLayout(c.ProjectID)
	if err != nil {
		return "", err
	}
	return filepath.Join(l.Sessions, c.ID), nil
}
