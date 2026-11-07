package workbench

import "strings"

// taskTitleFromRequest extracts a display title locally. It does not call a
// model or change the complete request stored with the task.
func taskTitleFromRequest(request string) string {
	text := strings.TrimSpace(request)
	if text == "" {
		return "新任务"
	}
	for _, prefix := range []string{"请帮我", "麻烦帮我", "请帮助我", "帮我", "请"} {
		if strings.HasPrefix(text, prefix) {
			rest := strings.TrimSpace(strings.TrimPrefix(text, prefix))
			if rest != "" {
				text = rest
			}
			break
		}
	}
	for i, r := range text {
		if strings.ContainsRune("，。！？；\n\r", r) || (r == '.' && (i+1 == len(text) || text[i+1] == ' ')) {
			if first := strings.TrimSpace(text[:i]); first != "" {
				text = first
				break
			}
		}
	}
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) > 24 {
		text = string(runes[:23]) + "…"
	}
	return text
}
