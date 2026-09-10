package logx

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// Logger provides structured JSON logging.
type Logger struct {
	mu      sync.Mutex
	w       io.Writer
	service string
}

// Entry is a single log line in JSON format.
type Entry struct {
	Time      string      `json:"time"`
	Level     string      `json:"level"`
	Service   string      `json:"service"`
	Message   string      `json:"message"`
	Fields    map[string]any `json:"fields,omitempty"`
}

var (
	defaultLogger *Logger
)

func init() {
	defaultLogger = &Logger{
		w:       os.Stdout,
		service: "modigo-runner",
	}
}

// SetOutput sets the output writer for the default logger.
func SetOutput(w io.Writer) {
	defaultLogger.mu.Lock()
	defer defaultLogger.mu.Unlock()
	defaultLogger.w = w
}

// SetService sets the service name for the default logger.
func SetService(name string) {
	defaultLogger.mu.Lock()
	defer defaultLogger.mu.Unlock()
	defaultLogger.service = name
}

// Info logs an informational message.
func Info(msg string, fields ...map[string]any) {
	defaultLogger.log("info", msg, fields...)
}

// Warn logs a warning message.
func Warn(msg string, fields ...map[string]any) {
	defaultLogger.log("warn", msg, fields...)
}

// Error logs an error message.
func Error(msg string, fields ...map[string]any) {
	defaultLogger.log("error", msg, fields...)
}

// Debug logs a debug message.
func Debug(msg string, fields ...map[string]any) {
	defaultLogger.log("debug", msg, fields...)
}

// Printf provides backward compatibility with log.Printf.
// Converts format strings to structured log entries.
func Printf(format string, args ...any) {
	defaultLogger.log("info", fmt.Sprintf(format, args...))
}

// Fatalf provides backward compatibility with log.Fatalf.
func Fatalf(format string, args ...any) {
	defaultLogger.log("fatal", fmt.Sprintf(format, args...))
	os.Exit(1)
}

func (l *Logger) log(level, msg string, fields ...map[string]any) {
	l.mu.Lock()
	defer l.mu.Unlock()

	entry := Entry{
		Time:    time.Now().UTC().Format(time.RFC3339),
		Level:   level,
		Service: l.service,
		Message: msg,
	}

	if len(fields) > 0 && fields[0] != nil {
		entry.Fields = fields[0]
	}

	data, _ := json.Marshal(entry)
	l.w.Write(append(data, '\n'))
}
