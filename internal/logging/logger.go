package logging

import (
	"encoding/json"
	"log"
	"os"
	"time"
)

// Logger is a simple structured logger that emits JSON objects to stdout.
// It is intended for use in command-line services where a lightweight
// logging facade is sufficient.
type Logger struct {
	l *log.Logger
}

type logEntry struct {
	Timestamp string                 `json:"timestamp"`
	Level     string                 `json:"level"`
	Event     string                 `json:"event"`
	Message   string                 `json:"message,omitempty"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
}

// New creates a new Logger that writes to stdout.
func New() *Logger {
	return &Logger{l: log.New(os.Stdout, "", 0)}
}

// Info logs an informational event.
func (l *Logger) Info(event, msg string, fields map[string]interface{}) {
	l.log("info", event, msg, fields)
}

// Error logs an error event.
func (l *Logger) Error(event, msg string, fields map[string]interface{}) {
	l.log("error", event, msg, fields)
}

func (l *Logger) log(level, event, msg string, fields map[string]interface{}) {
	entry := logEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Level:     level,
		Event:     event,
		Message:   msg,
		Fields:    fields,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		// Fallback to plain printing on marshal failure.
		l.l.Printf("%s %s %s %+v", level, event, msg, fields)
		return
	}
	l.l.Println(string(data))
}
