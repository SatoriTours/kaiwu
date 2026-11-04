package executionenv

import (
	"context"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// UnitInspection proves current ownership only, not mounts, namespace isolation,
// project quota or admissibility. ActualBudget.DiskBytes is always unknown (0).
type UnitInspection struct {
	Observation Observation
	MainPID     int
}
type kernelSample struct {
	BootID, ProcessGroup string
	Populated            bool
	Budget               Budget
}
type kernelInspection interface {
	Read(context.Context) (kernelSample, error)
	Close() error
}

// SystemdInspector is read-only and never infers OwnedGone from absent metadata.
// Its native constructor uses only local user-manager show and pinned proc/cgroup
// reads. There is no start/stop/reset/kill operation or host-execution fallback.
type SystemdInspector struct {
	show func(context.Context, string) ([]byte, error)
	pin  func(context.Context, string, int) (kernelInspection, error)
}

func (i *SystemdInspector) Inspect(parent context.Context, expected Identity) (UnitInspection, error) {
	if expected.Validate(true) != nil {
		return UnitInspection{}, ErrInvalid
	}
	if i == nil || i.show == nil || i.pin == nil {
		return UnitInspection{}, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	fail := func() (UnitInspection, error) { return UnitInspection{}, ErrUnavailable }
	if ctx.Err() != nil {
		return fail()
	}
	raw, err := i.show(ctx, expected.Unit)
	if err != nil {
		return fail()
	}
	first, err := parseUnitView(raw, expected)
	if err != nil {
		return fail()
	}
	pin, err := i.pin(ctx, expected.ControlGroup, first.pid)
	if err != nil {
		if pin != nil {
			pin.Close()
		}
		return fail()
	}
	if pin == nil {
		return fail()
	}
	defer pin.Close()
	before, err := pin.Read(ctx)
	if err != nil || !validKernelSample(before, expected) {
		return fail()
	}
	raw, err = i.show(ctx, expected.Unit)
	if err != nil {
		return fail()
	}
	second, err := parseUnitView(raw, expected)
	if err != nil || first != second {
		return fail()
	}
	after, err := pin.Read(ctx)
	if err != nil || ctx.Err() != nil || after != before || !validKernelSample(after, expected) {
		return fail()
	}
	return UnitInspection{MainPID: second.pid, Observation: Observation{Identity: expected, State: OwnedRunning, GroupPopulated: true, ActualBudget: after.Budget}}, nil
}

type unitView struct{ pid int }

func parseUnitView(raw []byte, expected Identity) (unitView, error) {
	if len(raw) == 0 || len(raw) > 65536 || !utf8.Valid(raw) || strings.ContainsAny(string(raw), "\r\x00") {
		return unitView{}, ErrUnavailable
	}
	fields := map[string]string{}
	for _, line := range strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return unitView{}, ErrUnavailable
		}
		switch k {
		case "Id", "LoadState", "ActiveState", "SubState", "InvocationID", "ControlGroup", "MainPID":
		default:
			return unitView{}, ErrUnavailable
		}
		if _, seen := fields[k]; seen {
			return unitView{}, ErrUnavailable
		}
		fields[k] = v
	}
	if len(fields) != 7 || fields["Id"] != expected.Unit || fields["LoadState"] != "loaded" || fields["ActiveState"] != "active" || fields["SubState"] != "running" || fields["InvocationID"] != expected.InvocationID || fields["ControlGroup"] != expected.ControlGroup {
		return unitView{}, ErrUnavailable
	}
	pid, err := kernelNumber(fields["MainPID"])
	if err != nil || pid <= 0 || pid > 2147483647 {
		return unitView{}, ErrUnavailable
	}
	return unitView{pid: int(pid)}, nil
}
func kernelNumber(s string) (int64, error) {
	if s == "" || len(s) > 19 {
		return 0, ErrUnavailable
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, ErrUnavailable
		}
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, ErrUnavailable
	}
	return v, nil
}
func validKernelSample(s kernelSample, expected Identity) bool {
	return s.BootID == expected.BootID && s.ProcessGroup == expected.ControlGroup && s.Populated && validKernelBudget(s.Budget)
}
func validKernelBudget(b Budget) bool {
	if b.DiskBytes != 0 {
		return false
	}
	// Validate only the three observed kernel limits; 1 is not an observation or
	// quota assertion and never leaves this local validation copy.
	b.DiskBytes = 1
	return b.Validate() == nil
}
