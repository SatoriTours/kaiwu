package executionenv

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func inspectIdentity() Identity {
	return Identity{RunID: strings.Repeat("a", 32), BootID: "12345678-1234-1234-1234-123456789abc", Unit: "kaiwu-exec-" + strings.Repeat("a", 32) + ".service", InvocationID: strings.Repeat("b", 32), ControlGroup: "/user.slice/kaiwu-exec-" + strings.Repeat("a", 32) + ".service"}
}
func inspectShow(id Identity) string {
	return "Id=" + id.Unit + "\nLoadState=loaded\nActiveState=active\nSubState=running\nInvocationID=" + id.InvocationID + "\nControlGroup=" + id.ControlGroup + "\nMainPID=123\n"
}

type samplePin struct {
	sample        kernelSample
	reads, closed int
	err           error
	onRead        func(int, *kernelSample)
}

func (p *samplePin) Read(context.Context) (kernelSample, error) {
	p.reads++
	if p.onRead != nil {
		p.onRead(p.reads, &p.sample)
	}
	return p.sample, p.err
}
func (p *samplePin) Close() error { p.closed++; return nil }
func inspectFixture(t *testing.T) (*SystemdInspector, *samplePin, *int) {
	t.Helper()
	id := inspectIdentity()
	p := &samplePin{sample: kernelSample{BootID: id.BootID, ProcessGroup: id.ControlGroup, Populated: true, Budget: Budget{CPUQuotaUSec: 25000, MemoryBytes: 100663296, PidsMax: 32}}}
	calls := new(int)
	inspector := &SystemdInspector{show: func(ctx context.Context, unit string) ([]byte, error) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if unit != id.Unit {
			t.Fatal("inspected foreign unit")
		}
		*calls++
		return []byte(inspectShow(id)), nil
	}, pin: func(ctx context.Context, cg string, pid int) (kernelInspection, error) {
		if cg != id.ControlGroup || pid != 123 {
			t.Fatal("wrong kernel target")
		}
		return p, nil
	}}
	return inspector, p, calls
}
func TestInspectorCorroboratesUnitAndKernelBeforeOwnership(t *testing.T) {
	i, p, calls := inspectFixture(t)
	view, err := i.Inspect(context.Background(), inspectIdentity())
	if err != nil {
		t.Fatal(err)
	}
	if view.MainPID != 123 || !CanStop(inspectIdentity(), view.Observation) || view.Observation.ActualBudget.CPUQuotaUSec != 25000 || view.Observation.ActualBudget.MemoryBytes != 100663296 || view.Observation.ActualBudget.PidsMax != 32 || view.Observation.ActualBudget.DiskBytes != 0 {
		t.Fatal("missing actual ownership/budget or invented disk quota")
	}
	if *calls != 2 || p.reads != 2 || p.closed != 1 {
		t.Fatal("missing stable second observation or leaked pin")
	}
}
func TestInspectorRejectsMissingForeignAndChangingUnit(t *testing.T) {
	for _, mutate := range []func(string) string{
		func(s string) string { return strings.Replace(s, "LoadState=loaded", "LoadState=not-found", 1) },
		func(s string) string { return strings.Replace(s, "ActiveState=active", "ActiveState=inactive", 1) },
		func(s string) string { return strings.Replace(s, "SubState=running", "SubState=exited", 1) },
		func(s string) string { return strings.Replace(s, "MainPID=123", "MainPID=0", 1) },
		func(s string) string { return strings.Replace(s, "MainPID=123", "MainPID=+123", 1) },
		func(s string) string { return strings.Replace(s, "MainPID=123", "MainPID=2147483648", 1) },
		func(s string) string { return strings.ReplaceAll(s, strings.Repeat("b", 32), strings.Repeat("c", 32)) },
		func(s string) string { return s + "MainPID=123\n" },
		func(s string) string { return s + "Secret=never-echo-this\n" },
		func(s string) string {
			return strings.Replace(s, "ControlGroup=/user.slice/", "ControlGroup=/other.slice/", 1)
		},
	} {
		for _, at := range []int{1, 2} {
			i, p, _ := inspectFixture(t)
			calls := 0
			i.show = func(context.Context, string) ([]byte, error) {
				calls++
				raw := inspectShow(inspectIdentity())
				if calls == at {
					raw = mutate(raw)
				}
				return []byte(raw), nil
			}
			view, err := i.Inspect(context.Background(), inspectIdentity())
			if !errors.Is(err, ErrUnavailable) || view.MainPID != 0 || strings.Contains(err.Error(), "never-echo-this") {
				t.Fatal("unproven ownership/secret reflected", err)
			}
			if at == 2 && p.closed != 1 {
				t.Fatal("kernel pin leak")
			}
		}
	}
}
func TestInspectorRejectsKernelMismatchAndNeverInfersGone(t *testing.T) {
	for _, mutate := range []func(*kernelSample){func(s *kernelSample) { s.BootID = "00000000-0000-0000-0000-000000000000" }, func(s *kernelSample) { s.ProcessGroup = "/foreign" }, func(s *kernelSample) { s.Populated = false }, func(s *kernelSample) { s.Budget.MemoryBytes = 0 }, func(s *kernelSample) { s.Budget.DiskBytes = 1 }} {
		i, p, _ := inspectFixture(t)
		mutate(&p.sample)
		view, err := i.Inspect(context.Background(), inspectIdentity())
		if !errors.Is(err, ErrUnavailable) || view.Observation.State != "" || p.closed != 1 {
			t.Fatal("kernel mismatch or missing group became proof", err)
		}
	}
	i, p, _ := inspectFixture(t)
	p.err = errors.New("missing cgroup")
	if v, err := i.Inspect(context.Background(), inspectIdentity()); err == nil || v.Observation.State == OwnedGone {
		t.Fatal("absence released ownership")
	}
}
func TestInspectorContextBoundsWholeInspection(t *testing.T) {
	i, _, _ := inspectFixture(t)
	i.show = func(ctx context.Context, _ string) ([]byte, error) {
		if d, ok := ctx.Deadline(); !ok || time.Until(d) > 5*time.Second {
			t.Fatal("missing shared inspection budget")
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := i.Inspect(ctx, inspectIdentity()); err == nil || time.Since(start) > time.Second {
		t.Fatal("ignored parent budget")
	}
	i, _, calls := inspectFixture(t)
	invalid := inspectIdentity()
	invalid.InvocationID = ""
	if _, err := i.Inspect(context.Background(), invalid); !errors.Is(err, ErrInvalid) || *calls != 0 {
		t.Fatal("partial identity accepted or spawned query")
	}
}

func TestInspectorRejectsChangingButIndividuallyValidKernelLimits(t *testing.T) {
	i, p, _ := inspectFixture(t)
	p.onRead = func(n int, s *kernelSample) {
		if n == 2 {
			s.Budget.CPUQuotaUSec = 24000
		}
	}
	if view, err := i.Inspect(context.Background(), inspectIdentity()); !errors.Is(err, ErrUnavailable) || view.MainPID != 0 || p.closed != 1 {
		t.Fatal("unstable kernel snapshot accepted")
	}
}
