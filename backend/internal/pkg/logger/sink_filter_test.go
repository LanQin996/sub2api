package logger

import (
	"sync"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type recordingSink struct {
	mu     sync.Mutex
	events []*LogEvent
}

func (s *recordingSink) WriteLogEvent(event *LogEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
}

type indexedRecordingSink struct {
	recordingSink
}

func (*indexedRecordingSink) IndexedEventsOnly() bool { return true }

func TestShouldForwardToSinkFiltersUnindexedInfoBeforeEncoding(t *testing.T) {
	info := zapcore.Entry{Level: zapcore.InfoLevel}
	if shouldForwardToSink(info, nil, []zapcore.Field{zap.String("request_id", "req-1")}) {
		t.Fatal("ordinary info log should not be forwarded to the ops sink")
	}
	if !shouldForwardToSink(info, nil, []zapcore.Field{zap.String("component", "http.access")}) {
		t.Fatal("access log should be forwarded to the ops sink")
	}
	if !shouldForwardToSink(info, []zapcore.Field{zap.String("component", "security.audit")}, nil) {
		t.Fatal("inherited audit component should be forwarded to the ops sink")
	}
	if !shouldForwardToSink(info,
		[]zapcore.Field{zap.String("component", "http")},
		[]zapcore.Field{zap.String("component", "http.access")},
	) {
		t.Fatal("later access component should override inherited request component")
	}
	if shouldForwardToSink(info,
		[]zapcore.Field{zap.String("component", "security.audit")},
		[]zapcore.Field{zap.String("component", "http")},
	) {
		t.Fatal("later ordinary component should override inherited audit component")
	}
	if !shouldForwardToSink(info,
		[]zapcore.Field{zap.String("component", "http.access")},
		[]zapcore.Field{zap.Any("component", struct{ Name string }{Name: "dynamic"})},
	) {
		t.Fatal("dynamic final component should be forwarded conservatively")
	}
	if !shouldForwardToSink(zapcore.Entry{Level: zapcore.WarnLevel}, nil, nil) {
		t.Fatal("warn log should always be forwarded to the ops sink")
	}
}

func TestSinkCoreOnlyPreFiltersOptedInSink(t *testing.T) {
	entry := zapcore.Entry{Level: zapcore.InfoLevel, Message: "ordinary info"}
	core := &sinkCore{}

	ordinary := &recordingSink{}
	SetSink(ordinary)
	t.Cleanup(func() { SetSink(nil) })
	if err := core.Write(entry, nil); err != nil {
		t.Fatalf("write ordinary sink: %v", err)
	}
	if len(ordinary.events) != 1 {
		t.Fatalf("ordinary sink got %d events, want 1", len(ordinary.events))
	}

	indexed := &indexedRecordingSink{}
	SetSink(indexed)
	if err := core.Write(entry, nil); err != nil {
		t.Fatalf("write indexed sink: %v", err)
	}
	if len(indexed.events) != 0 {
		t.Fatalf("indexed sink got %d ordinary info events, want 0", len(indexed.events))
	}
}
