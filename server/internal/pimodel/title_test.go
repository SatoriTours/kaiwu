package pimodel

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestTitleSummaryUsesToollessRPCAndCleansUp(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "unrelated-test-secret")
	program := filepath.Join(t.TempDir(), "fakepi")
	if out, err := exec.Command("go", "build", "-o", program, "../agent/testdata/fakepi").CombinedOutput(); err != nil {
		t.Fatalf("%s: %v", out, err)
	}
	c := &Catalog{Program: program, SourceDir: t.TempDir(), DataDir: t.TempDir()}
	title, err := c.SummarizeTitle(context.Background(), "界面保持原样。核心需求是把每段聊天保存进数据库，重启后恢复。", "fixture", "isolated-test", "", "server_pi")
	if err != nil || title != "聊天记录持久化与恢复" {
		t.Fatalf("%q %v", title, err)
	}
	entries, _ := os.ReadDir(c.DataDir)
	if len(entries) != 0 {
		t.Fatal("temporary credentials left behind")
	}

	// A task without server_pi authorization must not even read the source.
	if err = os.WriteFile(filepath.Join(c.SourceDir, "auth.json"), []byte("invalid private source"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = c.SummarizeTitle(context.Background(), "核心需求是保存聊天", "fixture", "isolated-test", "", ""); err != nil {
		t.Fatal("unauthorized source was loaded", err)
	}
	if _, err = c.SummarizeTitle(context.Background(), "核心需求是保存聊天", "fixture", "isolated-test", "", "server_pi"); err == nil {
		t.Fatal("invalid authorized source accepted")
	}
	if _, err = c.SummarizeTitle(context.Background(), "hello", "", "", "", ""); err == nil {
		t.Fatal("missing model accepted")
	}
}

func TestTitleValidationRejectsInstructionsAndMultiline(t *testing.T) {
	for _, v := range []string{"", "一行\n另一行", "<script>", "标题：" + string(make([]rune, 40))} {
		if _, err := validateTitle(v); err == nil {
			t.Fatalf("accepted %q", v)
		}
	}
	if v, e := validateTitle("「聊天记录持久化」"); e != nil || v != "聊天记录持久化" {
		t.Fatal(v, e)
	}
}
