package pirpc

type ErrorCode string
type Error struct {
	Code     ErrorCode
	Delivery Delivery
	cause    error
}

var (
	ErrInvalidInput    = &Error{Code: "invalid_input"}
	ErrProtocol        = &Error{Code: "protocol"}
	ErrRecordTooLarge  = &Error{Code: "record_too_large"}
	ErrTooManyRequests = &Error{Code: "too_many_requests"}
	ErrWriteQueueFull  = &Error{Code: "write_queue_full"}
	ErrEventOverflow   = &Error{Code: "event_overflow"}
	ErrRemoteRejected  = &Error{Code: "remote_rejected"}
	ErrClosed          = &Error{Code: "closed"}
	ErrProcessExit     = &Error{Code: "process_exit"}
	ErrIO              = &Error{Code: "io"}
	ErrWriteTimeout    = &Error{Code: "write_timeout"}
	ErrDrainTimeout    = &Error{Code: "drain_timeout"}
	ErrShutdownTimeout = &Error{Code: "shutdown_timeout"}
	ErrCleanup         = &Error{Code: "cleanup"}
	ErrCanceled        = &Error{Code: "canceled"}
	ErrDeadline        = &Error{Code: "deadline"}
	ErrIDExhausted     = &Error{Code: "id_exhausted"}
)

func (e *Error) Error() string {
	switch e.Code {
	case "invalid_input", "protocol", "record_too_large", "too_many_requests", "write_queue_full", "event_overflow", "remote_rejected", "closed", "process_exit", "io", "write_timeout", "drain_timeout", "shutdown_timeout", "cleanup", "canceled", "deadline", "id_exhausted":
		return "pi rpc: " + string(e.Code)
	default:
		return "pi rpc: failure"
	}
}
func (e *Error) Is(target error) bool { t, ok := target.(*Error); return ok && e.Code == t.Code }
func (e *Error) Unwrap() error        { return e.cause }
func newError(code ErrorCode, d Delivery, cause error) *Error {
	return &Error{Code: code, Delivery: d, cause: cause}
}
