package githubactions

import (
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// mapConclusion translates a completed workflow run conclusion into a gate
// state. Skipped/neutral are configuration problems, not repairable code
// failures; cancelled/timed_out/action_required/stale fail without repair.
func mapConclusion(conclusion string) (state, code string, repairable bool) {
	switch conclusion {
	case "success":
		return StatePassed, "success", false
	case "failure":
		return StateFailed, "failure", true
	case "skipped", "neutral":
		return StateBlocked, conclusion, false
	case "cancelled", "timed_out", "action_required", "stale":
		return StateFailed, conclusion, false
	case "":
		return StateWaiting, "no_conclusion", false
	default:
		return StateBlocked, "unknown_conclusion", false
	}
}

// gateJobs applies required job gates to the complete job set of one attempt.
func gateJobs(wf workbench.DeliveryWorkflow, jobs []jobRow) (state, code string, repairable bool) {
	byName := map[string]jobRow{}
	for _, job := range jobs {
		byName[job.Name] = job
	}
	for _, required := range wf.Jobs {
		job, ok := byName[required]
		if !ok {
			return StateFailed, "required_job_missing", false
		}
		switch job.conclusion() {
		case "success":
		case "skipped":
			return StateBlocked, "required_job_skipped", false
		case "neutral":
			return StateBlocked, "required_job_neutral", false
		case "failure":
			return StateFailed, "failure", true
		case "cancelled", "timed_out", "action_required", "stale":
			return StateFailed, job.conclusion(), false
		default:
			return StateWaiting, "required_job_pending", false
		}
	}
	return StatePassed, "", false
}

func jobMap(jobs []jobRow) map[string]string {
	out := map[string]string{}
	for _, job := range jobs {
		out[job.Name] = job.conclusion()
	}
	return out
}
