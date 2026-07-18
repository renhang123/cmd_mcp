package audit

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"time"

	"server-shell-mcp/internal/app"
)

type Event struct {
	EventVersion  int               `json:"event_version"`
	EventType     string            `json:"event_type"`
	Timestamp     time.Time         `json:"timestamp"`
	RequestID     string            `json:"request_id"`
	CommandID     string            `json:"command_id"`
	Source        app.SourceSummary `json:"source"`
	ResultStatus  app.Status        `json:"result_status"`
	ErrorCategory app.ErrorCategory `json:"error_category,omitempty"`
	DurationMS    int64             `json:"duration_ms"`
}

type JSONL struct {
	writer io.Writer
	mu     sync.Mutex
	now    func() time.Time
}

func NewJSONL(writer io.Writer) *JSONL {
	return &JSONL{writer: writer, now: time.Now}
}

func (j *JSONL) Record(ctx context.Context, event app.AuditEvent) error {
	encoded := Event{
		EventVersion:  1,
		EventType:     event.EventType,
		Timestamp:     j.now().UTC(),
		RequestID:     event.RequestID,
		CommandID:     event.CommandID,
		Source:        redactSource(event.Source),
		ResultStatus:  event.Status,
		ErrorCategory: event.ErrorCategory,
		DurationMS:    event.Duration.Milliseconds(),
	}
	data, err := json.Marshal(encoded)
	if err != nil {
		return err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	_, err = j.writer.Write(append(data, '\n'))
	return err
}

type Memory struct {
	Events []app.AuditEvent
	mu     sync.Mutex
}

func NewMemory() *Memory {
	return &Memory{}
}

func (m *Memory) Record(ctx context.Context, event app.AuditEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Events = append(m.Events, event)
	return nil
}

func redactSource(source app.SourceSummary) app.SourceSummary {
	return app.SourceSummary{
		ClientIDHash: source.ClientIDHash,
		UserIDHash:   source.UserIDHash,
		RemoteHash:   source.RemoteHash,
		Transport:    source.Transport,
		MCPTool:      source.MCPTool,
	}
}
