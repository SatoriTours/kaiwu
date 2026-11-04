package pimodel

import "testing"

func TestStoredKeyCannotMoveToNewEndpointSilently(t *testing.T) {
	c, _ := OpenConnections(t.TempDir())
	v, e := c.Save(ConnectionInput{Name: "a", API: "openai-completions", BaseURL: "https://example.com/v1", Key: "secret", Models: []string{"one"}})
	if e != nil {
		t.Fatal(e)
	}
	_, e = c.Save(ConnectionInput{ID: v.ID, Revision: v.Revision, Name: "a", API: v.API, BaseURL: "https://other.example/v1", Models: v.Models})
	if e == nil {
		t.Fatal("stored key silently moved")
	}
}
