package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"

	"github.com/SatoriTours/kaiwu/server/internal/preview"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// A human terminal group owns one workspace gate, but EACH shell has a durable
// launch window and process group. No single-process marker is reused for siblings.
type terminalLeaseGroup struct {
	root    *toolLease
	id      string
	members map[string]*terminalMemberLease
	failed  bool
}
type terminalMemberLease struct {
	manager                      *Manager
	group                        *terminalLeaseGroup
	key, id                      string
	pid                          int
	attempted, noProcess, closed bool
}

func terminalNonce() (string, error) {
	b := make([]byte, 16)
	_, e := rand.Read(b)
	return hex.EncodeToString(b), e
}

func (m *Manager) AcquireTerminalLease(ctx context.Context, projectID, taskID string) (preview.Lease, error) {
	m.terminalMu.Lock()
	defer m.terminalMu.Unlock()
	st := m.store.Snapshot()
	p, ok := st.Projects[projectID]
	if !ok {
		return nil, workbench.Missing()
	}
	c := st.Conversations[p.ConversationID]
	if taskID != "" {
		t, ok := st.Tasks[taskID]
		if !ok || t.ProjectID != projectID || t.ExecutionID == "" {
			return nil, workbench.Missing()
		}
		c = st.Conversations[t.ConversationID]
	}
	m.mu.Lock()
	closed := m.closed
	m.mu.Unlock()
	if closed || c.RunState == "starting" || c.RunState == "running" || workbench.TaskMerging(st, taskID) {
		return nil, workbench.Conflict("AI 或合并正在使用工作区，请等待结束")
	}
	key := projectID + "/" + taskID
	if m.terminalGroups == nil {
		m.terminalGroups = map[string]*terminalLeaseGroup{}
	}
	g := m.terminalGroups[key]
	if g != nil && g.failed {
		return nil, workbench.Conflict("原终端退出或归属未确认，工作区锁仍保留")
	}
	id, e := terminalNonce()
	if e != nil {
		return nil, e
	}
	if g == nil {
		lease, e := m.AcquireToolLease(ctx, projectID, taskID)
		if e != nil {
			return nil, e
		}
		g = &terminalLeaseGroup{root: lease.(*toolLease), id: id, members: map[string]*terminalMemberLease{}}
		m.terminalGroups[key] = g
	}
	member := &terminalMemberLease{manager: m, group: g, key: key, id: id}
	g.members[id] = member
	if e = g.persist(); e != nil {
		g.failed = true
		return nil, e
	}
	return member, nil
}
func (g *terminalLeaseGroup) persist() error {
	owners := map[string]int{}
	for id, member := range g.members {
		owners[id] = member.pid
	}
	return g.root.lease.writeTerminalOwners(owners)
}
func (l *terminalMemberLease) TerminalGroup() string { return l.group.id }
func (l *terminalMemberLease) File() *os.File        { return l.group.root.File() }
func (l *terminalMemberLease) Attempt() error {
	l.manager.terminalMu.Lock()
	defer l.manager.terminalMu.Unlock()
	if l.closed || l.attempted || l.group.failed {
		return errors.New("终端启动归属不可用")
	}
	l.attempted = true
	return l.group.persist()
}
func (l *terminalMemberLease) Started(pid int) error {
	l.manager.terminalMu.Lock()
	defer l.manager.terminalMu.Unlock()
	if l.closed || !l.attempted || l.pid != 0 || pid <= 0 {
		l.group.failed = true
		return errors.New("无法记录终端进程归属")
	}
	l.pid = pid // retain in memory even when durable write fails
	return l.group.persist()
}
func (l *terminalMemberLease) NoProcess() {
	l.manager.terminalMu.Lock()
	defer l.manager.terminalMu.Unlock()
	if !l.closed && (l.pid == 0 || processGroupGone(l.pid)) {
		l.noProcess = true
	}
}
func (l *terminalMemberLease) Release(_ bool) error {
	m := l.manager
	m.terminalMu.Lock()
	defer m.terminalMu.Unlock()
	if l.closed {
		return nil
	}
	if l.pid > 0 && !processGroupGone(l.pid) || l.pid == 0 && l.attempted && !l.noProcess {
		l.group.failed = true
		return errors.New("终端进程组未确认退出，工作区锁仍保留")
	}
	g := l.group
	delete(g.members, l.id)
	l.closed = true
	if len(g.members) > 0 {
		if e := g.persist(); e != nil {
			g.failed = true
			return e
		}
		return nil
	}
	// Only the last proven member may clear the root marker and workspace gate.
	g.root.NoProcess()
	if e := g.root.Release(true); e != nil {
		g.failed = true
		return e
	}
	delete(m.terminalGroups, l.key)
	return nil
}
