package aiconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestFileVersionRejectsConcurrentOverwriteAndLinks(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "config")
	c := FromProfile(Profile{Provider: "fake", Model: "first", CredentialSource: "server_pi"})
	v, err := Write(dir, "", c)
	if err != nil {
		t.Fatal(err)
	}
	loaded, version, err := Read(dir)
	if err != nil || version != v || *loaded.Profile.Model != "first" {
		t.Fatal("read mismatch", err)
	}
	var wg sync.WaitGroup
	wins := make(chan bool, 2)
	for _, model := range []string{"a", "b"} {
		wg.Add(1)
		go func(model string) {
			defer wg.Done()
			c := FromProfile(Profile{Provider: "fake", Model: model})
			_, err := Write(dir, version, c)
			wins <- err == nil
		}(model)
	}
	wg.Wait()
	close(wins)
	count := 0
	for won := range wins {
		if won {
			count++
		}
	}
	if count != 1 {
		t.Fatal("lost update", count)
	}
	if _, err := Write(dir, v, c); err == nil {
		t.Fatal("stale version overwrote config")
	}
	other := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(other, []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "ai.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, filepath.Join(dir, "ai.json")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Read(dir); err == nil {
		t.Fatal("read linked secret")
	}
	if _, err := Write(dir, "", c); err == nil {
		t.Fatal("overwrote linked config")
	}
}
func TestFrozenSnapshotCannotBeSilentlyRewritten(t *testing.T) {
	r, err := Resolve(FromProfile(Profile{Provider: "fake", Model: "first"}), Config{SchemaVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := Freeze(dir, "conversation", r); err != nil {
		t.Fatal(err)
	}
	r.Profile.Model = "modified"
	if err := Freeze(dir, "conversation", r); err == nil {
		t.Fatal("rewrote immutable snapshot")
	}
	b, err := os.ReadFile(filepath.Join(dir, "conversation.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got Resolved
	json.Unmarshal(b, &got)
	if got.Profile.Model != "first" {
		t.Fatal("snapshot changed")
	}
}
