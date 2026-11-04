package workbench

import (
	"path/filepath"
	"testing"
)

func TestGlobalCannotPublishUnavailableSkills(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "db"), Profile{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.ConfigureAI(filepath.Join(t.TempDir(), "config")); err != nil {
		t.Fatal(err)
	}
	before, err := s.AIConfig("")
	if err != nil {
		t.Fatal(err)
	}
	missing := []string{"not-installed"}
	c := before.Config
	c.Skills = &missing
	if _, err := s.SaveAIConfig("", before.Version, c); err == nil {
		t.Fatal("published unavailable skill")
	}
	after, err := s.AIConfig("")
	if err != nil || after.Version != before.Version {
		t.Fatal("failed config write changed source")
	}
}
