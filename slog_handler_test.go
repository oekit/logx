package logx

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
)

// captureState 跨 WithAttrs/WithGroup 派生 handler 共享的记录缓冲。
type captureState struct {
	mu   sync.Mutex
	recs []map[string]any
	msgs []string
	lvls []slog.Level
}

type captureHandler struct {
	state  *captureState
	prefix []slog.Attr
	group  string
}

func newCaptureHandler() *captureHandler {
	return &captureHandler{state: &captureState{}}
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	m := map[string]any{}
	for _, a := range h.prefix {
		m[a.Key] = a.Value.Any()
	}
	if h.group != "" {
		m["#group"] = h.group
	}
	r.Attrs(func(a slog.Attr) bool {
		m[a.Key] = a.Value.Any()
		return true
	})
	h.state.mu.Lock()
	defer h.state.mu.Unlock()
	h.state.recs = append(h.state.recs, m)
	h.state.msgs = append(h.state.msgs, r.Message)
	h.state.lvls = append(h.state.lvls, r.Level)
	return nil
}

func (h *captureHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	c := *h
	c.prefix = append(append([]slog.Attr(nil), h.prefix...), attrs...)
	return &c
}

func (h *captureHandler) WithGroup(name string) slog.Handler {
	c := *h
	if c.group == "" {
		c.group = name
	} else {
		c.group = c.group + "." + name
	}
	return &c
}

func (h *captureHandler) snapshot() []map[string]any {
	h.state.mu.Lock()
	defer h.state.mu.Unlock()
	return append([]map[string]any(nil), h.state.recs...)
}

func TestNewSlogLogger_WithHandler(t *testing.T) {
	h := newCaptureHandler()
	l := NewSlogLogger("svc", WithHandler(h))
	l.Info("hello", "k", "v", "err", errors.New("boom"))

	recs := h.snapshot()
	if len(recs) != 1 {
		t.Fatalf("want 1 record, got %d (%v)", len(recs), recs)
	}
	if h.state.msgs[0] != "hello" {
		t.Errorf("msg = %q", h.state.msgs[0])
	}
	if h.state.lvls[0] != slog.LevelInfo {
		t.Errorf("level = %v", h.state.lvls[0])
	}
	got := recs[0]
	if got["service"] != "svc" {
		t.Errorf("service = %v", got["service"])
	}
	if got["k"] != "v" {
		t.Errorf("k = %#v", got["k"])
	}
	if got["err"] != "boom" {
		t.Errorf("err = %#v", got["err"])
	}
	if _, ok := got["caller"]; !ok {
		t.Error("caller attr missing")
	}
}

func TestNewSlogLogger_WithHandler_LevelFilter(t *testing.T) {
	h := newCaptureHandler()
	l := NewSlogLogger("svc", WithHandler(h), WithLevel(LevelWarn))
	l.Debug("d")
	l.Info("i")
	l.Warn("w")
	l.Error("e")

	recs := h.snapshot()
	if len(recs) != 2 {
		t.Fatalf("want 2 records (warn+error), got %d", len(recs))
	}
	if h.state.msgs[0] != "w" || h.state.msgs[1] != "e" {
		t.Errorf("msgs = %q,%q", h.state.msgs[0], h.state.msgs[1])
	}
}

func TestNewSlogLogger_WithHandler_CloneKeepsHandler(t *testing.T) {
	h := newCaptureHandler()
	l := NewSlogLogger("svc", WithHandler(h)).
		WithAttrs(Attr{Key: "rid", Value: "r-1"}).
		WithService("child").
		WithGroup("g")

	l.Info("msg")

	recs := h.snapshot()
	if len(recs) != 1 {
		t.Fatalf("want 1 record, got %d", len(recs))
	}
	got := recs[0]
	if got["service"] != "child" {
		t.Errorf("service = %v", got["service"])
	}
	// #99：用户字段（含派生前挂的 Attr）进组、框架字段留顶层
	grp, ok := got["g"].([]slog.Attr)
	if !ok {
		t.Fatalf("g 组属性缺失: %#v", got["g"])
	}
	found := false
	for _, a := range grp {
		if a.Key == "rid" && a.Value.Any() == "r-1" {
			found = true
		}
	}
	if !found {
		t.Errorf("rid 未落进 g 组: %#v", grp)
	}
}

func TestNewSlogLogger_WithHandler_OddKvMissing(t *testing.T) {
	h := newCaptureHandler()
	NewSlogLogger("svc", WithHandler(h)).Info("msg", "a", 1, "b")
	got := h.snapshot()[0]
	// slog.ValueOf(int) 落 KindInt64，Any() 返回 int64
	if got["a"] != int64(1) {
		t.Errorf("a = %#v", got["a"])
	}
	if got["b"] != "(MISSING)" {
		t.Errorf("b = %#v", got["b"])
	}
}

func TestNewSlogLogger_WithHandler_NoHandlerStillJSON(t *testing.T) {
	// 不注入 handler 时走内置 JSON→stdout，不 panic 即可
	l := NewSlogLogger("svc")
	l.Info("builtin-path")
}

func TestNewSlogLogger_LevelPriority(t *testing.T) {
	t.Setenv("LOG_LEVEL", "ERROR")

	l := NewSlogLogger("svc")
	if l.(*slogLogger).Level.Load() != LevelError {
		t.Errorf("LOG_LEVEL should win over default, got %v", l.(*slogLogger).Level)
	}

	l = NewSlogLogger("svc", WithLevel(LevelDebug))
	if l.(*slogLogger).Level.Load() != LevelDebug {
		t.Errorf("WithLevel should win over LOG_LEVEL, got %v", l.(*slogLogger).Level)
	}

	t.Setenv("LOG_LEVEL", "")
	l = NewSlogLogger("svc")
	if l.(*slogLogger).Level.Load() != LevelInfo {
		t.Errorf("default should be LevelInfo, got %v", l.(*slogLogger).Level)
	}
}

func TestSlogLevel_Mapping(t *testing.T) {
	cases := []struct {
		in   Level
		want slog.Level
	}{
		{LevelDebug, slog.LevelDebug},
		{LevelInfo, slog.LevelInfo},
		{LevelWarn, slog.LevelWarn},
		{LevelError, slog.LevelError},
	}
	for _, tt := range cases {
		if got := slogLevel(tt.in); got != tt.want {
			t.Errorf("slogLevel(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestUseSlog_WithHandler(t *testing.T) {
	orig := Default()
	t.Cleanup(func() { SetDefault(orig) })

	h := newCaptureHandler()
	UseSlog("svc", WithHandler(h))
	Default().Info("via-default")
	if len(h.snapshot()) != 1 {
		t.Fatalf("want 1 record via SetDefault, got %d", len(h.snapshot()))
	}
}
