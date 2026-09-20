package observability

import (
	"encoding/json"
	"log"
	"os"
	"time"
)

type Logger struct{ base *log.Logger }

func NewLogger() *Logger { return &Logger{base: log.New(os.Stdout, "", 0)} }
func (l *Logger) Event(name string, fields map[string]any) {
	payload := map[string]any{"event": name, "at": time.Now().UTC().Format(time.RFC3339Nano)}
	for key, value := range fields {
		payload[key] = value
	}
	data, err := json.Marshal(payload)
	if err != nil {
		l.base.Printf(`{"event":"log_encode_error","error":%q}`, err.Error())
		return
	}
	l.base.Print(string(data))
}
func (l *Logger) Request(method, path string, status int, duration time.Duration) {
	l.Event("http_request", map[string]any{"method": method, "path": path, "status": status, "duration_ms": duration.Milliseconds()})
}
func (l *Logger) Failure(operation string, err error) {
	if err == nil {
		return
	}
	l.Event("operation_failed", map[string]any{"operation": operation, "error": err.Error()})
}
