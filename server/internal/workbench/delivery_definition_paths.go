package workbench

import (
	"path"
	"sort"
	"strings"
)

// DeliveryDefinitionPaths validates and deduplicates the explicit file scope.
// Git pathspec syntax is never part of this API. Interpret names literally,
// from the project root, regardless of the command's working directory.
func DeliveryDefinitionPaths(checks []DeliveryCheck) ([]string, error) {
	paths := map[string]bool{}
	total := 0
	for _, c := range checks {
		if len(c.DefinitionPaths) > 32 {
			return nil, Invalid("每项检查最多声明 32 个保护定义文件")
		}
		for _, p := range c.DefinitionPaths {
			total++
			if total > 256 {
				return nil, Invalid("保护定义文件合计最多 256 项")
			}
			if p == "" || len(p) > 512 || p == "." || path.Clean(p) != p || strings.HasPrefix(p, "/") || strings.HasPrefix(p, "../") || p == ".." || strings.ContainsAny(p, "\\:") || strings.TrimSpace(p) != p {
				return nil, Invalid("保护定义必须是项目根目录下的规范相对文件路径")
			}
			for _, r := range p {
				if r < 32 || r == 127 {
					return nil, Invalid("保护定义路径不能包含控制字符")
				}
			}
			for _, part := range strings.Split(p, "/") {
				if strings.EqualFold(part, ".git") {
					return nil, Invalid("保护定义不能指向 Git 内部文件")
				}
			}
			paths[p] = true
		}
	}
	out := make([]string, 0, len(paths))
	for p := range paths {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}
