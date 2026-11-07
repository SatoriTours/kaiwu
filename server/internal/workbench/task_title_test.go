package workbench

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTaskTitleFromRequest(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"请帮我添加数据库的能力，我要能输入我的每段话", "添加数据库的能力"},
		{"  修复登录失败\n刷新后仍保持登录 ", "修复登录失败"},
		{"帮我做一个聊天页面。支持 Markdown。", "做一个聊天页面"},
		{"Add SQLite support. Keep all messages.", "Add SQLite support"},
		{"支持 Windows、macOS 和 Linux", "支持 Windows、macOS 和 Linux"},
		{"请帮我", "请帮我"},
		{"   ", "新任务"},
	} {
		if got := taskTitleFromRequest(tc.in); got != tc.want {
			t.Errorf("%q: got %q want %q", tc.in, got, tc.want)
		}
	}
	got := taskTitleFromRequest(strings.Repeat("需求", 40))
	if !utf8.ValidString(got) || utf8.RuneCountInString(got) > 24 || !strings.HasSuffix(got, "…") {
		t.Fatalf("invalid shortened title %q", got)
	}
}
