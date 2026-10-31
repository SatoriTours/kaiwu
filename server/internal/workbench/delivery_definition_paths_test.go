package workbench

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestDeliveryDefinitionPathsRejectUnsafePoliciesAndDrafts(t *testing.T) {
	for _, bad := range []string{"", ".", "../check.sh", "dir/../check.sh", "/check.sh", "C:/check.sh", "dir//check.sh", "dir/", "./check.sh", ".git/config", "dir/.GIT/config", "dir\\check.sh", " check.sh", "check.sh\n"} {
		for _, enabled := range []bool{true, false} {
			t.Run(fmt.Sprintf("%q/enabled=%v", bad, enabled), func(t *testing.T) {
				p := validDeliveryPolicy()
				p.Enabled = enabled
				b, _ := json.Marshal([]string{bad})
				if err := json.Unmarshal([]byte(fmt.Sprintf(`{"checks":[{"id":"test","argv":["true"],"dir":".","timeoutSeconds":30,"definitionPaths":%s}]}`, b)), &p); err != nil {
					t.Fatal(err)
				}
				if ValidateDeliveryPolicy(p) == nil {
					t.Fatal("unsafe definition path accepted")
				}
			})
		}
	}
}
func TestDeliveryDefinitionPathsBoundAndDeduplicate(t *testing.T) {
	p := validDeliveryPolicy()
	p.Checks[0].DefinitionPaths = []string{"scripts/check file.sh", "go.mod", "go.mod"}
	paths, err := DeliveryDefinitionPaths(p.Checks)
	if err != nil || len(paths) != 2 || paths[0] != "go.mod" || paths[1] != "scripts/check file.sh" {
		t.Fatalf("invalid canonical scope: %v %v", paths, err)
	}
	p.Checks[0].DefinitionPaths = make([]string, 33)
	for i := range p.Checks[0].DefinitionPaths {
		p.Checks[0].DefinitionPaths[i] = fmt.Sprintf("file-%d", i)
	}
	if ValidateDeliveryPolicy(p) == nil {
		t.Fatal("unbounded scope accepted")
	}
	p.Checks[0].DefinitionPaths = []string{strings.Repeat("a", 513)}
	if ValidateDeliveryPolicy(p) == nil {
		t.Fatal("unbounded name accepted")
	}
	p.Checks = make([]DeliveryCheck, 9)
	for i := range p.Checks {
		p.Checks[i].DefinitionPaths = make([]string, 32)
		for j := range p.Checks[i].DefinitionPaths {
			p.Checks[i].DefinitionPaths[j] = fmt.Sprintf("check-%d/file-%d", i, j)
		}
	}
	if _, err := DeliveryDefinitionPaths(p.Checks); err == nil {
		t.Fatal("unbounded aggregate accepted")
	}
}

func TestDeliveryDefinitionPathsAffectAuthorizationFingerprint(t *testing.T) {
	p := validDeliveryPolicy()
	before := ProtectedDeliveryFingerprint(p)
	if err := json.Unmarshal([]byte(`{"checks":[{"id":"test","argv":["go","test","./..."],"dir":".","timeoutSeconds":300,"definitionPaths":["go.mod","scripts/check file.sh"]}]}`), &p); err != nil {
		t.Fatal(err)
	}
	if err := ValidateDeliveryPolicy(p); err != nil {
		t.Fatal(err)
	}
	if before == ProtectedDeliveryFingerprint(p) {
		t.Fatal("declared definitions were not included in protected policy scope")
	}
}
