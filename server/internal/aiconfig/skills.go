package aiconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/SatoriTours/kaiwu/server/internal/projectstorage"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

func BindSkills(root string, r *Resolved) error {
	if len(r.Skills) == 0 {
		return nil
	}
	r.SkillContents = map[string]string{}
	total := 0
	for _, id := range r.Skills {
		dir, err := openDir(filepath.Join(root, "skills", id), false)
		if err != nil {
			return errors.New("authorized skill file unavailable")
		}
		b, err := readFile(dir, "SKILL.md")
		dir.Close()
		if err != nil {
			return errors.New("authorized skill file unavailable")
		}
		total += len(b)
		if len(b) > 64<<10 || total > 128<<10 || !utf8.Valid(b) || bytes.IndexByte(b, 0) >= 0 {
			return errors.New("invalid skill content")
		}
		r.SkillContents[id] = string(b)
	}
	b, _ := json.Marshal(struct {
		Version  string
		Contents map[string]string
	}{r.Version, r.SkillContents})
	r.Version = fileHash(b)
	return nil
}
func freezeSkills(dir, id string, r Resolved) error {
	for _, skill := range r.Skills {
		if !projectstorage.ValidDirectoryName(skill) || strings.ContainsAny(skill, ".:") {
			return errors.New("invalid frozen skill identifier")
		}
	}
	for _, skill := range r.Skills {
		content, ok := r.SkillContents[skill]
		if !ok {
			return errors.New("frozen skill content missing")
		}
		root, err := openDir(filepath.Join(dir, id+".skills", skill), true)
		if err != nil {
			return err
		}
		old, err := readFile(root, "SKILL.md")
		if err == nil {
			root.Close()
			if string(old) != content {
				return errors.New("immutable skill snapshot mismatch")
			}
			continue
		}
		if !os.IsNotExist(err) {
			root.Close()
			return err
		}
		err = atomicFile(root, "SKILL.md", []byte(content), true)
		root.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
