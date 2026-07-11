package app

import (
	"fmt"
	"sync"
	"time"
)

type LogBuffer struct {
	mu      sync.Mutex
	entries []string
	limit   int
}

func NewLogBuffer(limit int) *LogBuffer {
	if limit <= 0 {
		limit = 100
	}
	return &LogBuffer{limit: limit}
}

func (l *LogBuffer) Add(format string, args ...any) {
	if !l.mu.TryLock() {
		return
	}
	defer l.mu.Unlock()
	line := fmt.Sprintf("%s %s", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, args...))
	l.entries = append(l.entries, line)
	if len(l.entries) > l.limit {
		l.entries = l.entries[len(l.entries)-l.limit:]
	}
}

func (l *LogBuffer) Entries() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, len(l.entries))
	copy(out, l.entries)
	return out
}
