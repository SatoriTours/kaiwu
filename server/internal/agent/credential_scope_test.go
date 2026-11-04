package agent

import (
	"encoding/json"
	"github.com/SatoriTours/kaiwu/server/internal/pimodel"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
	"os"
	"path/filepath"
	"testing"
)

func TestManagedCredentialsRequireSelectedProviderBeforeAnyCopy(t *testing.T) {
	for _, withConnections := range []bool{false, true} {
		t.Run(map[bool]string{false: "server-pi", true: "connections"}[withConnections], func(t *testing.T) {
			m, p := managedFixture(t)
			source := t.TempDir()
			m.cfg.PiAgentDir = source
			auth := map[string]any{"alpha": map[string]any{"type": "api_key", "key": "synthetic-alpha"}, "beta": map[string]any{"type": "api_key", "key": "synthetic-beta"}}
			b, _ := json.Marshal(auth)
			if err := os.WriteFile(filepath.Join(source, "auth.json"), b, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(source, "models.json"), []byte(`{"providers":{"alpha":{"baseUrl":"https://alpha.invalid","models":[{"id":"m"}]},"beta":{"baseUrl":"https://beta.invalid","models":[{"id":"m"}]}}}`), 0600); err != nil {
				t.Fatal(err)
			}
			selected := "alpha"
			expected := "synthetic-alpha"
			if withConnections {
				connections, err := pimodel.OpenConnections(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				for _, name := range []string{"alpha", "beta"} {
					record, err := connections.Save(pimodel.ConnectionInput{Name: name, API: "openai-completions", BaseURL: "https://" + name + ".invalid", Key: "connection-" + name, Models: []string{"m"}})
					if err != nil {
						t.Fatal(err)
					}
					if name == "alpha" {
						selected = record.Provider
					}
				}
				m.cfg.Connections = connections
				expected = "connection-alpha"
			}
			c := m.store.Snapshot().Conversations[p.ConversationID]
			c.Profile = workbench.Profile{CredentialSource: "server_pi"}
			if _, err := m.launch(c); err == nil {
				t.Fatal("empty provider launched with server credentials")
			}
			target := filepath.Join(p.StorageRoot, ".kaiwu", "sessions", c.ID, "agent", "auth.json")
			if _, err := os.Stat(target); !os.IsNotExist(err) {
				t.Fatal("empty provider copied credentials before refusal")
			}
			c.Profile.Provider = selected
			c.Profile.Model = "m"
			if _, err := m.launch(c); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			selectedAuth := map[string]struct {
				Key string `json:"key"`
			}{}
			if err := json.Unmarshal(data, &selectedAuth); err != nil || len(selectedAuth) != 1 || selectedAuth[selected].Key != expected {
				t.Fatal("wrong selected credential scope")
			}
			data, err = os.ReadFile(filepath.Join(filepath.Dir(target), "models.json"))
			if err != nil {
				t.Fatal(err)
			}
			models := struct {
				Providers map[string]json.RawMessage `json:"providers"`
			}{}
			if err := json.Unmarshal(data, &models); err != nil || len(models.Providers) != 1 || models.Providers[selected] == nil {
				t.Fatal("wrong selected model-definition scope")
			}
		})
	}
}
