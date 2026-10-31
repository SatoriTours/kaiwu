package agent

import (
	"strings"
	"testing"

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

func TestQuietFailurePersistsOnlyActiveRoundReason(t *testing.T) {
	m, store, project, conversation := failureFixture(t)
	got := sendRound(t, m, store, project, conversation, "failed", "fail:"+rateLimitReason)
	if len(got.Rounds) != 1 || got.Rounds[0].Failure == nil {
		t.Fatal("active round lost structured failure")
	}
	failure := got.Rounds[0].Failure
	if failure.Kind != "execution" || !strings.Contains(failure.Detail, "429") {
		t.Fatalf("wrong failure: %+v", failure)
	}
	got = sendRound(t, m, store, project, conversation, "next", "继续")
	if got.Rounds[0].Failure == nil || got.Rounds[1].Failure != nil {
		t.Fatal("failure moved to new round or was lost")
	}
}

func TestQuietIdleDisconnectDoesNotAttachFailureToFinishedRound(t *testing.T) {
	m, store, project, conversation := failureFixture(t)
	got := sendRound(t, m, store, project, conversation, "finished", "完成普通回复")
	m.failed(conversation, "pi 会话已中断，发送下一条消息时自动接回；历史已保留", true)
	got = store.Snapshot().Conversations[conversation]
	if got.Rounds[0].Failure != nil {
		t.Fatal("idle disconnect marked completed answer as failed")
	}
}

func TestQuietFailurePersistsSendUncertaintyKind(t *testing.T) {
	m, store, _, conversation := failureFixture(t)
	if err := store.Update(func(st *workbench.State) error {
		c := st.Conversations[conversation]
		c.RunState = "running"
		st.Conversations[conversation] = c
		workbench.BeginRound(st, conversation, "u")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	m.failed(conversation, "消息未能确认完成；请检查后明确恢复，不会自动重发", true)
	got := store.Snapshot().Conversations[conversation]
	if got.Rounds[0].Failure == nil || got.Rounds[0].Failure.Kind != "send" {
		t.Fatal("uncertain send became execution failure")
	}
}
