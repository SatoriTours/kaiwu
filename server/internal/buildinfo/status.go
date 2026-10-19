package buildinfo

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
)

// StatusFileName is the read-only deployment record written atomically by the
// local deployment scripts inside the service data directory.
const StatusFileName = "deployment-status.json"

// maxStatusBytes bounds the status read. A larger file is treated as unknown
// instead of being loaded.
const maxStatusBytes = 64 << 10

// Public deployment stages. Any other value is reported as unknown.
const (
	StatusTesting   = "testing"
	StatusBuilding  = "building"
	StatusDeploying = "deploying"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusUnknown   = "unknown"
)

// Deployment is the most recent deployment attempt recorded by the scripts.
type Deployment struct {
	Revision string
	Status   string
	At       string
}

// DeploymentStatus is the bounded, read-only view of the target's
// deployment-status.json.
type DeploymentStatus struct {
	// Revision and DeployedAt describe the last successful activation.
	Revision   string
	DeployedAt string
	// Deployment describes the most recent attempt, which may be a failure.
	Deployment Deployment
}

type statusFile struct {
	Revision   string `json:"revision"`
	DeployedAt string `json:"deployedAt"`
	Deployment struct {
		Revision string `json:"revision"`
		Status   string `json:"status"`
		At       string `json:"at"`
	} `json:"deployment"`
}

// ReadDeploymentStatus reads dataDir/deployment-status.json read-only and
// returns the zero value for a missing, unreadable, corrupt, oversized or
// partially valid file. It never writes and never returns file contents.
func ReadDeploymentStatus(dataDir string) DeploymentStatus {
	var out DeploymentStatus
	if !filepath.IsAbs(dataDir) {
		return out
	}
	f, err := os.Open(filepath.Join(dataDir, StatusFileName))
	if err != nil {
		return out
	}
	defer f.Close()
	body, err := io.ReadAll(io.LimitReader(f, maxStatusBytes+1))
	if err != nil || len(body) > maxStatusBytes {
		return out
	}
	var parsed statusFile
	if json.Unmarshal(body, &parsed) != nil {
		return out
	}
	if ValidRevision(parsed.Revision) {
		out.Revision = parsed.Revision
		out.DeployedAt = cleanTime(parsed.DeployedAt)
	}
	if ValidRevision(parsed.Deployment.Revision) {
		out.Deployment.Revision = parsed.Deployment.Revision
	}
	out.Deployment.Status = cleanStatus(parsed.Deployment.Status)
	out.Deployment.At = cleanTime(parsed.Deployment.At)
	if out.Deployment.Status != StatusUnknown && (out.Deployment.Revision == "" || out.Deployment.At == "") {
		// A stage or success without usable revision and time is incomplete
		// evidence; report unknown instead of echoing a partial record.
		out.Deployment.Status = StatusUnknown
	}
	return out
}

func cleanStatus(s string) string {
	switch s {
	case StatusTesting, StatusBuilding, StatusDeploying, StatusSucceeded, StatusFailed:
		return s
	default:
		return StatusUnknown
	}
}

// stageMessages are the only deployment texts exposed publicly. Arbitrary file
// contents are never echoed, so paths, commands and credentials cannot leak.
var stageMessages = map[string]string{
	StatusTesting:   "正在本机测试与构建",
	StatusBuilding:  "正在构建候选版本",
	StatusDeploying: "正在切换新版本",
	StatusSucceeded: "部署成功",
	StatusFailed:    "部署未完成，请核对当前版本/服务状态",
	StatusUnknown:   "暂无部署记录",
}

// StageMessage returns the fixed Chinese text for a public status.
func StageMessage(status string) string {
	if message, ok := stageMessages[status]; ok {
		return message
	}
	return stageMessages[StatusUnknown]
}

// DeploymentView is the public projection of the most recent attempt.
type DeploymentView struct {
	Revision string `json:"revision"`
	Status   string `json:"status"`
	At       string `json:"at"`
	Message  string `json:"message"`
}

// Version is the exact GET /api/version response body.
type Version struct {
	Revision      string         `json:"revision"`
	ShortRevision string         `json:"shortRevision"`
	Summary       string         `json:"summary"`
	BuiltAt       string         `json:"builtAt"`
	DeployedAt    string         `json:"deployedAt"`
	Deployment    DeploymentView `json:"deployment"`
}

// Report merges the running binary's metadata with the last recorded
// deployment. The running revision is authoritative; deployedAt is exposed
// only when it belongs to that same revision.
func Report(info Info, status DeploymentStatus) Version {
	info = normalize(info)
	out := Version{
		Revision:      info.Revision,
		ShortRevision: info.ShortRevision,
		Summary:       info.Summary,
		BuiltAt:       info.BuiltAt,
		Deployment: DeploymentView{
			Revision: status.Deployment.Revision,
			Status:   cleanStatus(status.Deployment.Status),
			At:       status.Deployment.At,
		},
	}
	out.Deployment.Message = StageMessage(out.Deployment.Status)
	if out.Deployment.Status != StatusUnknown && (out.Deployment.Revision == "" || out.Deployment.At == "") {
		// A stage or success without usable revision and time is incomplete
		// evidence.
		out.Deployment.Status = StatusUnknown
		out.Deployment.Message = StageMessage(StatusUnknown)
	}
	if info.Revision != Unknown && status.Revision == info.Revision {
		out.DeployedAt = status.DeployedAt
	}
	return out
}
