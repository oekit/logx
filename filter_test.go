package logx

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// fakeLogger 记录全部下发条目，供 filter/slogger 断言。
type fakeLogger struct {
	entries []fakeEntry
}

type fakeEntry struct {
	level Level
	msg   string
	kvs   []any
}

func (f *fakeLogger) record(level Level, msg string, kvs []any) {
	f.entries = append(f.entries, fakeEntry{level: level, msg: msg, kvs: kvs})
}

func (f *fakeLogger) text() string {
	var sb strings.Builder
	for _, e := range f.entries {
		sb.WriteString(levelName(e.level) + " " + e.msg)
		for i := 0; i+1 < len(e.kvs); i += 2 {
			fmt.Fprintf(&sb, " %v=%v", e.kvs[i], e.kvs[i+1])
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func (f *fakeLogger) Debug(msg string, kvs ...any) { f.record(LevelDebug, msg, kvs) }
func (f *fakeLogger) Info(msg string, kvs ...any)  { f.record(LevelInfo, msg, kvs) }
func (f *fakeLogger) Warn(msg string, kvs ...any)  { f.record(LevelWarn, msg, kvs) }
func (f *fakeLogger) Error(msg string, kvs ...any) { f.record(LevelError, msg, kvs) }
func (f *fakeLogger) Debugf(format string, args ...any) {
	f.record(LevelDebug, fmt.Sprintf(format, args...), nil)
}
func (f *fakeLogger) Infof(format string, args ...any) {
	f.record(LevelInfo, fmt.Sprintf(format, args...), nil)
}
func (f *fakeLogger) Warnf(format string, args ...any) {
	f.record(LevelWarn, fmt.Sprintf(format, args...), nil)
}
func (f *fakeLogger) Errorf(format string, args ...any) {
	f.record(LevelError, fmt.Sprintf(format, args...), nil)
}
func (f *fakeLogger) CtxDebug(_ context.Context, msg string, kvs ...any) {
	f.record(LevelDebug, msg, kvs)
}
func (f *fakeLogger) CtxInfo(_ context.Context, msg string, kvs ...any) {
	f.record(LevelInfo, msg, kvs)
}
func (f *fakeLogger) CtxWarn(_ context.Context, msg string, kvs ...any) {
	f.record(LevelWarn, msg, kvs)
}
func (f *fakeLogger) CtxError(_ context.Context, msg string, kvs ...any) {
	f.record(LevelError, msg, kvs)
}
func (f *fakeLogger) CtxDebugf(_ context.Context, format string, args ...any) {
	f.record(LevelDebug, fmt.Sprintf(format, args...), nil)
}
func (f *fakeLogger) CtxInfof(_ context.Context, format string, args ...any) {
	f.record(LevelInfo, fmt.Sprintf(format, args...), nil)
}
func (f *fakeLogger) CtxWarnf(_ context.Context, format string, args ...any) {
	f.record(LevelWarn, fmt.Sprintf(format, args...), nil)
}
func (f *fakeLogger) CtxErrorf(_ context.Context, format string, args ...any) {
	f.record(LevelError, fmt.Sprintf(format, args...), nil)
}
func (f *fakeLogger) WithCtx(_ context.Context) Logger { return f }
func (f *fakeLogger) WithService(_ string) Logger      { return f }
func (f *fakeLogger) WithAttrs(_ ...Attr) Logger       { return f }
func (f *fakeLogger) WithGroup(_ string) Logger        { return f }
func (f *fakeLogger) SetLevel(_ Level)                 {}

func TestFilterKeyRedaction(t *testing.T) {
	base := &fakeLogger{}
	l := WithFilter(base, FilterKey("password", "user.token"))
	l.Info("login", "user", "alice", "password", "s3cret")
	l.CtxInfo(context.Background(), "api", "user.token", "t-xyz", "plain", "ok")

	out := base.text()
	if strings.Contains(out, "s3cret") || strings.Contains(out, "t-xyz") {
		t.Fatalf("secret leaked: %s", out)
	}
	if !strings.Contains(out, "***") {
		t.Fatalf("redacted marker missing: %s", out)
	}
	if !strings.Contains(out, "alice") || !strings.Contains(out, "plain=ok") {
		t.Fatalf("non-sensitive kv dropped: %s", out)
	}
}

func TestFilterFuncDrop(t *testing.T) {
	base := &fakeLogger{}
	l := WithFilter(base, FilterFunc(func(msg string, _ []any) bool {
		return strings.Contains(msg, "noisy")
	}))
	l.Info("noisy", "k", 1)
	l.Info("keep", "k", 2)
	l.Infof("noisy %d", 3)
	l.Infof("keep %d", 4)

	out := base.text()
	if strings.Contains(out, "noisy") {
		t.Fatalf("noisy records leaked: %s", out)
	}
	if !strings.Contains(out, "keep") || !strings.Contains(out, "keep 4") {
		t.Fatalf("kept records missing: %s", out)
	}
}

func TestFilterSubLoggerKeepsRules(t *testing.T) {
	base := &fakeLogger{}
	l := WithFilter(base, FilterKey("password"))
	sub := l.WithService("svc")
	sub.Info("login", "password", "abc")

	out := base.text()
	if strings.Contains(out, "abc") || !strings.Contains(out, "***") {
		t.Fatalf("sub logger lost filter rules: %s", out)
	}
}

func TestSloggerRoundTrip(t *testing.T) {
	base := &fakeLogger{}
	sl := Slogger(base)
	sl.Info("hello", "k", "v")
	sl.Warn("warned")
	sl.Debug("debug-msg")
	sl.Error("boom", "err", "x")

	out := base.text()
	for _, want := range []string{"INFO hello", "k=v", "WARN warned", "DEBUG debug-msg", "ERROR boom", "err=x"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in output: %s", want, out)
		}
	}
}

func TestSloggerGroupPrefix(t *testing.T) {
	base := &fakeLogger{}
	sl := Slogger(base).WithGroup("user")
	sl.Info("login", "name", "alice", "password", "xyz")

	out := base.text()
	if !strings.Contains(out, "user.name=alice") || !strings.Contains(out, "user.password=xyz") {
		t.Fatalf("group prefix missing: %s", out)
	}
}

func levelName(l Level) string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	default:
		return "INFO"
	}
}
