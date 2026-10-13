package workbench

import (
	"path/filepath"
	"testing"
	"time"
)

func TestExpiredLoginCannotAuthenticate(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.db"), Profile{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.SaveLogin("expired-hash", "token-hash", time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if s.ValidLogin("expired-hash", "token-hash") {
		t.Fatal("expired login accepted")
	}
	if err = s.SaveLogin("current-hash", "token-hash", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if !s.ValidLogin("current-hash", "token-hash") {
		t.Fatal("valid login rejected")
	}
	if s.ValidLogin("current-hash", "other-token-hash") {
		t.Fatal("wrong access token accepted")
	}
	if err = s.RevokeLogin("current-hash"); err != nil {
		t.Fatal(err)
	}
	if s.ValidLogin("current-hash", "token-hash") {
		t.Fatal("revoked login accepted")
	}
}
