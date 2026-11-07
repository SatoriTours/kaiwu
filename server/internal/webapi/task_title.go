package webapi

import (
	"context"
	"errors"

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

type TitleSummarizer interface {
	SummarizeTitle(context.Context, string, string, string, string, string) (string, error)
}

var errTitleNotPending = errors.New("title not pending")

// scheduleTitle claims the persisted intent before starting one helper. Repeat
// receipts cannot spend tokens again. Client disconnect does not own this job.
func (h *handler) scheduleTitle(id string) {
	if id == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.socketClosed {
		return
	}
	var request string
	var profile workbench.Profile
	run := false
	err := h.store.Update(func(st *workbench.State) error {
		t, ok := st.Tasks[id]
		if !ok || (t.TitleState != "pending" && t.TitleState != "needs_model") {
			return errTitleNotPending
		}
		c, ok := st.Conversations[t.ConversationID]
		if !ok {
			return errTitleNotPending
		}
		profile = c.Profile
		request = t.Request
		if profile.Provider == "" || profile.Model == "" {
			t.TitleState = "needs_model"
			t.TitleError = "选择模型后自动总结标题"
		} else if h.cfg.Titles == nil {
			t.TitleState = "failed"
			t.TitleError = "标题总结不可用，暂用需求摘要"
		} else if h.titleActive >= 2 {
			t.TitleState = "failed"
			t.TitleError = "标题服务繁忙，暂用需求摘要"
		} else {
			t.TitleState = "running"
			t.TitleError = ""
			run = true
		}
		st.Tasks[id] = t
		return nil
	})
	if err != nil || !run {
		return
	}
	h.titleActive++
	h.titleWG.Add(1)
	go func() {
		defer h.titleWG.Done()
		defer func() { h.mu.Lock(); h.titleActive--; h.mu.Unlock() }()
		title, err := h.cfg.Titles.SummarizeTitle(h.cfg.Context, request, profile.Provider, profile.Model, profile.Thinking, profile.CredentialSource)
		_ = h.store.Update(func(st *workbench.State) error {
			t, ok := st.Tasks[id]
			if !ok || t.TitleState != "running" || t.Request != request {
				return errTitleNotPending
			}
			if err != nil {
				t.TitleState = "failed"
				t.TitleError = "标题总结未完成，暂用需求摘要"
			} else {
				t.TitleState = "ready"
				t.TitleError = ""
				t.Title = title
			}
			st.Tasks[id] = t
			return nil
		})
	}()
}

// A crash may have consumed tokens; never infer permission to reissue it.
func (h *handler) recoverTitles() {
	for _, t := range h.store.Snapshot().Tasks {
		if t.TitleState == "running" || t.TitleState == "pending" {
			_ = h.store.Update(func(st *workbench.State) error {
				for id, t := range st.Tasks {
					if t.TitleState == "running" || t.TitleState == "pending" {
						t.TitleState = "failed"
						t.TitleError = "标题总结已中断，暂用需求摘要"
						st.Tasks[id] = t
					}
				}
				return nil
			})
			return
		}
	}
}
