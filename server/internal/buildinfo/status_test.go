package buildinfo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const statusRev = "8b54309f03dd75759291be48ff3f3b766ab89b5e"

func writeStatus(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, StatusFileName), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestReadDeploymentStatusMissingOrUnsafe(t *testing.T) {
	if got := ReadDeploymentStatus(filepath.Join(t.TempDir(), "absent")); got != (DeploymentStatus{}) {
		t.Fatalf("missing file: %+v", got)
	}
	if got := ReadDeploymentStatus("relative/path"); got != (DeploymentStatus{}) {
		t.Fatalf("relative data dir: %+v", got)
	}
	if got := ReadDeploymentStatus(filepath.Join(writeStatus(t, "{"), "")); got != (DeploymentStatus{}) {
		t.Fatalf("corrupt file: %+v", got)
	}
	big := `{"revision":"` + statusRev + `","deployedAt":"2026-10-19T02:00:00Z","pad":"` + strings.Repeat("x", maxStatusBytes) + `"}`
	if got := ReadDeploymentStatus(writeStatus(t, big)); got != (DeploymentStatus{}) {
		t.Fatalf("oversize file: %+v", got)
	}
}

func TestReadDeploymentStatusValidRecord(t *testing.T) {
	body := `{
	  "revision": "` + statusRev + `",
	  "deployedAt": "2026-10-19T10:00:00+08:00",
	  "extra": "ignored",
	  "deployment": {"revision": "` + statusRev + `", "status": "succeeded", "at": "2026-10-19T10:00:00+08:00"}
	}`
	got := ReadDeploymentStatus(writeStatus(t, body))
	if got.Revision != statusRev || got.DeployedAt != "2026-10-19T02:00:00Z" {
		t.Fatalf("activation: %+v", got)
	}
	if got.Deployment.Revision != statusRev || got.Deployment.Status != StatusSucceeded || got.Deployment.At != "2026-10-19T02:00:00Z" {
		t.Fatalf("attempt: %+v", got.Deployment)
	}
}

func TestReadDeploymentStatusRejectsUnusableFields(t *testing.T) {
	cases := map[string]string{
		"unknown status":        `{"deployment":{"revision":"` + statusRev + `","status":"victorious","at":"2026-10-19T02:00:00Z"}}`,
		"succeeded without sha": `{"deployment":{"status":"succeeded","at":"2026-10-19T02:00:00Z"}}`,
		"bad revision":          `{"deployment":{"revision":"main","status":"failed","at":"2026-10-19T02:00:00Z"}}`,
		"bad time":              `{"deployment":{"revision":"` + statusRev + `","status":"failed","at":"soon"}}`,
		"partial json":          `{"revision":`,
	}
	for name, body := range cases {
		got := ReadDeploymentStatus(writeStatus(t, body))
		if public := Report(Info{}, got).Deployment.Status; public != StatusUnknown {
			t.Fatalf("%s: %+v", name, got.Deployment)
		}
		if got.Deployment.Revision == "main" || got.Deployment.At == "soon" {
			t.Fatalf("%s leaked raw value: %+v", name, got.Deployment)
		}
	}
}

func TestReadDeploymentStatusIsReadOnly(t *testing.T) {
	dir := writeStatus(t, `{"revision":"`+statusRev+`","deployedAt":"2026-10-19T02:00:00Z"}`)
	path := filepath.Join(dir, StatusFileName)
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatal(err)
	}
	got := ReadDeploymentStatus(dir)
	if got.Revision != statusRev {
		t.Fatalf("read-only file not readable: %+v", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o400 || info.Size() == 0 {
		t.Fatalf("file modified: %v %d", info.Mode(), info.Size())
	}
}

func TestReportUnknownByDefault(t *testing.T) {
	got := Report(Info{}, DeploymentStatus{})
	if got.Revision != Unknown || got.ShortRevision != Unknown {
		t.Fatalf("revision: %+v", got)
	}
	if got.DeployedAt != "" {
		t.Fatalf("unknown deployment claimed a time: %q", got.DeployedAt)
	}
	if got.Deployment.Status != StatusUnknown || got.Deployment.Message == "" || got.Deployment.Revision != "" || got.Deployment.At != "" {
		t.Fatalf("deployment: %+v", got.Deployment)
	}
}

func TestReportDeployedAtRequiresRunningRevision(t *testing.T) {
	info := Info{Revision: statusRev}
	status := DeploymentStatus{Revision: strings.Repeat("b", 40), DeployedAt: "2026-10-19T02:00:00Z"}
	if got := Report(info, status); got.DeployedAt != "" {
		t.Fatalf("stale activation time exposed: %q", got.DeployedAt)
	}
	status.Revision = statusRev
	if got := Report(info, status); got.DeployedAt != "2026-10-19T02:00:00Z" {
		t.Fatalf("matching activation time hidden: %q", got.DeployedAt)
	}
}

func TestReportDeploymentReflectsLatestAttempt(t *testing.T) {
	failed := strings.Repeat("c", 40)
	status := DeploymentStatus{Deployment: Deployment{Revision: failed, Status: StatusFailed, At: "2026-10-19T03:00:00Z"}}
	got := Report(Info{Revision: statusRev}, status)
	if got.Revision != statusRev || got.Deployment.Revision != failed || got.Deployment.Status != StatusFailed {
		t.Fatalf("attempt: %+v", got)
	}
	if got.Deployment.Message != "部署未完成，请核对当前版本/服务状态" {
		t.Fatalf("message not fixed: %q", got.Deployment.Message)
	}
}

func TestReportNeverEchoesFileMessageOrPath(t *testing.T) {
	body := `{
	  "revision": "` + statusRev + `",
	  "deployedAt": "2026-10-19T02:00:00Z",
	  "deployment": {"revision":"` + statusRev + `","status":"succeeded","at":"2026-10-19T02:00:00Z","message":"/home/user/secret/token=abc"}
	}`
	got := Report(Info{Revision: statusRev}, ReadDeploymentStatus(writeStatus(t, body)))
	if strings.Contains(got.Deployment.Message, "secret") || strings.Contains(got.Deployment.Message, "token") {
		t.Fatalf("file message leaked: %+v", got.Deployment)
	}
	if got.Deployment.Status != StatusSucceeded || got.Deployment.Message != "部署成功" {
		t.Fatalf("deployment: %+v", got.Deployment)
	}
}

func TestStageMessagesAreFixedAndNonEmpty(t *testing.T) {
	for _, status := range []string{StatusTesting, StatusBuilding, StatusDeploying, StatusSucceeded, StatusFailed, StatusUnknown, "", "made-up"} {
		if StageMessage(status) == "" {
			t.Fatalf("empty message for %q", status)
		}
	}
	if StageMessage("made-up") != StageMessage(StatusUnknown) {
		t.Fatal("arbitrary status produced a distinct message")
	}
}
