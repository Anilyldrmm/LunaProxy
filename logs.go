package main

import (
	"sync"
	"time"
)

type LogEntry struct {
	Time    string `json:"time"`
	Level   string `json:"level"` // INFO | WARN | ERROR
	Message string `json:"message"`
}

type logBuffer struct {
	mu      sync.RWMutex
	entries []LogEntry
}

var appLog = &logBuffer{}

const maxLogEntries = 500

func (l *logBuffer) Add(level, msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, LogEntry{
		Time:    time.Now().Format("15:04:05"),
		Level:   level,
		Message: msg,
	})
	if len(l.entries) > maxLogEntries {
		l.entries = l.entries[len(l.entries)-maxLogEntries:]
	}
}

func (l *logBuffer) All() []LogEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]LogEntry, len(l.entries))
	copy(out, l.entries)
	return out
}

func (l *logBuffer) Clear() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = nil
}

func (l *logBuffer) Len() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.entries)
}

// Kısayollar
func logInfo(msg string)  { appLog.Add("INFO", msg) }
func logWarn(msg string)  { appLog.Add("WARN", msg) }
func logError(msg string) { appLog.Add("ERROR", msg) }
