package logx

import (
	"bytes"
	"context"
	"log"
	"os"
	"strings"
	"sync"
	"testing"
)

// withCapturedStderr redirects the global log output to a buffer for
// the duration of the test, then restores the original. The mutex
// serializes tests that touch the global log writer.
var logMu sync.Mutex

func captureStderr(t *testing.T) *bytes.Buffer {
	t.Helper()
	logMu.Lock()
	t.Cleanup(func() { logMu.Unlock() })

	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })
	return &buf
}

// TestStdLogger_WithAttrsRendered 验证 WithAttrs 的持久化属性会渲染到输出
// （此前 std 后端存储了 Attrs 但从不输出，与 slog/hlog 后端行为不一致）。
func TestStdLogger_WithAttrsRendered(t *testing.T) {
	buf := captureStderr(t)
	l := NewStdLogger("svc").WithAttrs(Attr{Key: "component", Value: "nats"})
	l.Info("msg-with-attrs", "k", "v")
	out := buf.String()
	if !strings.Contains(out, "component=nats") {
		t.Errorf("expected component=nats attr in output, got: %s", out)
	}
	if !strings.Contains(out, "k=v") {
		t.Errorf("expected call-site kv in output, got: %s", out)
	}
}

// TestStdLogger_Debug covers the Debug structured-style method.
func TestStdLogger_Debug(t *testing.T) {
	buf := captureStderr(t)
	NewStdLogger("svc", LevelDebug).Debug("debug-msg", "k", "v")
	out := buf.String()
	if !strings.Contains(out, "[DEBUG]") {
		t.Errorf("expected [DEBUG] in output, got: %s", out)
	}
	if !strings.Contains(out, "debug-msg") {
		t.Errorf("expected message in output, got: %s", out)
	}
	if !strings.Contains(out, "k=v") {
		t.Errorf("expected key=value pair in output, got: %s", out)
	}
}

// TestStdLogger_Info covers the Info structured-style method.
// (Some Info paths are already tested by TestWithCtx_StdTraceInOutput;
// this verifies the message-only path.)
func TestStdLogger_Info(t *testing.T) {
	buf := captureStderr(t)
	NewStdLogger("svc", LevelDebug).Info("info-msg")
	if !strings.Contains(buf.String(), "info-msg") {
		t.Errorf("expected info-msg in output, got: %s", buf.String())
	}
}

// TestStdLogger_Warn covers the Warn structured-style method.
func TestStdLogger_Warn(t *testing.T) {
	buf := captureStderr(t)
	NewStdLogger("svc", LevelDebug).Warn("warn-msg", "code", 42)
	out := buf.String()
	if !strings.Contains(out, "[WARN]") {
		t.Errorf("expected [WARN] in output, got: %s", out)
	}
	if !strings.Contains(out, "warn-msg") {
		t.Errorf("expected message in output, got: %s", out)
	}
	if !strings.Contains(out, "code=42") {
		t.Errorf("expected key=value pair, got: %s", out)
	}
}

// TestStdLogger_Error covers the Error structured-style method.
func TestStdLogger_Error(t *testing.T) {
	buf := captureStderr(t)
	NewStdLogger("svc", LevelDebug).Error("err-msg", "err", "boom")
	out := buf.String()
	if !strings.Contains(out, "[ERROR]") {
		t.Errorf("expected [ERROR] in output, got: %s", out)
	}
	if !strings.Contains(out, "err-msg") {
		t.Errorf("expected message in output, got: %s", out)
	}
}

// TestStdLogger_Debugf covers the printf-style Debug.
func TestStdLogger_Debugf(t *testing.T) {
	buf := captureStderr(t)
	NewStdLogger("svc", LevelDebug).Debugf("hello %s, %d", "world", 7)
	out := buf.String()
	if !strings.Contains(out, "hello world, 7") {
		t.Errorf("expected formatted message, got: %s", out)
	}
}

// TestStdLogger_Infof covers the printf-style Info.
func TestStdLogger_Infof(t *testing.T) {
	buf := captureStderr(t)
	NewStdLogger("svc", LevelDebug).Infof("hello %s", "world")
	if !strings.Contains(buf.String(), "hello world") {
		t.Errorf("expected formatted message, got: %s", buf.String())
	}
}

// TestStdLogger_Warnf covers the printf-style Warn.
func TestStdLogger_Warnf(t *testing.T) {
	buf := captureStderr(t)
	NewStdLogger("svc", LevelDebug).Warnf("warn %s", "msg")
	if !strings.Contains(buf.String(), "warn msg") {
		t.Errorf("expected formatted message, got: %s", buf.String())
	}
}

// TestStdLogger_Errorf covers the printf-style Error.
func TestStdLogger_Errorf(t *testing.T) {
	buf := captureStderr(t)
	NewStdLogger("svc", LevelDebug).Errorf("err %s", "boom")
	if !strings.Contains(buf.String(), "err boom") {
		t.Errorf("expected formatted message, got: %s", buf.String())
	}
}

// --- Ctx methods ---

// TestStdLogger_CtxDebug covers CtxDebug.
func TestStdLogger_CtxDebug(t *testing.T) {
	restore := setTestExtractor()
	defer restore()
	buf := captureStderr(t)

	ctx := withTestTrace(context.Background(), "t-cd")
	NewStdLogger("svc", LevelDebug).CtxDebug(ctx, "cd-msg")
	out := buf.String()
	if !strings.Contains(out, "[DEBUG]") {
		t.Errorf("expected [DEBUG], got: %s", out)
	}
	if !strings.Contains(out, "[trace=t-cd]") {
		t.Errorf("expected [trace=t-cd] in output, got: %s", out)
	}
}

// TestStdLogger_CtxInfo covers CtxInfo.
func TestStdLogger_CtxInfo(t *testing.T) {
	restore := setTestExtractor()
	defer restore()
	buf := captureStderr(t)

	ctx := withTestTrace(context.Background(), "t-ci")
	NewStdLogger("svc", LevelDebug).CtxInfo(ctx, "ci-msg", "k", "v")
	out := buf.String()
	if !strings.Contains(out, "[INFO]") {
		t.Errorf("expected [INFO], got: %s", out)
	}
	if !strings.Contains(out, "ci-msg") {
		t.Errorf("expected message, got: %s", out)
	}
	if !strings.Contains(out, "k=v") {
		t.Errorf("expected key=value, got: %s", out)
	}
}

// TestStdLogger_CtxWarn covers CtxWarn.
func TestStdLogger_CtxWarn(t *testing.T) {
	restore := setTestExtractor()
	defer restore()
	buf := captureStderr(t)

	ctx := withTestTrace(context.Background(), "t-cw")
	NewStdLogger("svc", LevelDebug).CtxWarn(ctx, "cw-msg")
	out := buf.String()
	if !strings.Contains(out, "[WARN]") {
		t.Errorf("expected [WARN], got: %s", out)
	}
	if !strings.Contains(out, "[trace=t-cw]") {
		t.Errorf("expected [trace=t-cw] in output, got: %s", out)
	}
}

// TestStdLogger_CtxError covers CtxError.
func TestStdLogger_CtxError(t *testing.T) {
	restore := setTestExtractor()
	defer restore()
	buf := captureStderr(t)

	ctx := withTestTrace(context.Background(), "t-ce")
	NewStdLogger("svc", LevelDebug).CtxError(ctx, "ce-msg")
	out := buf.String()
	if !strings.Contains(out, "[ERROR]") {
		t.Errorf("expected [ERROR], got: %s", out)
	}
	if !strings.Contains(out, "[trace=t-ce]") {
		t.Errorf("expected [trace=t-ce] in output, got: %s", out)
	}
}

// TestStdLogger_CtxDebugf covers CtxDebugf.
func TestStdLogger_CtxDebugf(t *testing.T) {
	restore := setTestExtractor()
	defer restore()
	buf := captureStderr(t)

	ctx := withTestTrace(context.Background(), "t-cdf")
	NewStdLogger("svc", LevelDebug).CtxDebugf(ctx, "cdf %s", "msg")
	out := buf.String()
	if !strings.Contains(out, "[DEBUG]") {
		t.Errorf("expected [DEBUG], got: %s", out)
	}
	if !strings.Contains(out, "cdf msg") {
		t.Errorf("expected formatted message, got: %s", out)
	}
}

// TestStdLogger_CtxInfof covers CtxInfof.
func TestStdLogger_CtxInfof(t *testing.T) {
	restore := setTestExtractor()
	defer restore()
	buf := captureStderr(t)

	ctx := withTestTrace(context.Background(), "t-cif")
	NewStdLogger("svc", LevelDebug).CtxInfof(ctx, "cif %s", "msg")
	out := buf.String()
	if !strings.Contains(out, "[INFO]") {
		t.Errorf("expected [INFO], got: %s", out)
	}
	if !strings.Contains(out, "cif msg") {
		t.Errorf("expected formatted message, got: %s", out)
	}
}

// TestStdLogger_CtxWarnf covers CtxWarnf.
func TestStdLogger_CtxWarnf(t *testing.T) {
	restore := setTestExtractor()
	defer restore()
	buf := captureStderr(t)

	ctx := withTestTrace(context.Background(), "t-cwf")
	NewStdLogger("svc", LevelDebug).CtxWarnf(ctx, "cwf %s", "msg")
	out := buf.String()
	if !strings.Contains(out, "[WARN]") {
		t.Errorf("expected [WARN], got: %s", out)
	}
	if !strings.Contains(out, "cwf msg") {
		t.Errorf("expected formatted message, got: %s", out)
	}
}

// TestStdLogger_CtxErrorf covers CtxErrorf.
func TestStdLogger_CtxErrorf(t *testing.T) {
	restore := setTestExtractor()
	defer restore()
	buf := captureStderr(t)

	ctx := withTestTrace(context.Background(), "t-cef")
	NewStdLogger("svc", LevelDebug).CtxErrorf(ctx, "cef %s", "msg")
	out := buf.String()
	if !strings.Contains(out, "[ERROR]") {
		t.Errorf("expected [ERROR], got: %s", out)
	}
	if !strings.Contains(out, "cef msg") {
		t.Errorf("expected formatted message, got: %s", out)
	}
}

// --- SetLevel ---

// TestStdLogger_SetLevel covers the SetLevel runtime API and the
// ShouldLog filter.
func TestStdLogger_SetLevel(t *testing.T) {
	l := NewStdLogger("svc", LevelDebug).(*stdLogger)
	l.SetLevel(LevelError)
	if l.ShouldLog(LevelDebug) {
		t.Error("expected Debug to be filtered at Error level")
	}
	if !l.ShouldLog(LevelError) {
		t.Error("expected Error to pass at Error level")
	}
}

// --- prefix ---

// TestStdLogger_Prefix_EmptyService covers the prefix() function
// when service is empty (returns "").
func TestStdLogger_Prefix_EmptyService(t *testing.T) {
	l := &stdLogger{}
	if got := l.Prefix(); got != "" {
		t.Errorf("expected empty prefix for empty service, got %q", got)
	}
}

// TestStdLogger_Prefix_WithService covers the Prefix() function
// when service is set (returns [name]).
func TestStdLogger_Prefix_WithService(t *testing.T) {
	l := &stdLogger{BaseLogger: BaseLogger{Service: "my-svc"}}
	if got := l.Prefix(); got != "[my-svc]" {
		t.Errorf("expected [my-svc], got %q", got)
	}
}

// --- logf path with empty key/values ---

// TestStdLogger_logf_Empty covers the path where logf is called with
// no format args (just a static string).
func TestStdLogger_logf_Empty(t *testing.T) {
	buf := captureStderr(t)
	NewStdLogger("svc", LevelDebug).Debugf("static-msg")
	if !strings.Contains(buf.String(), "static-msg") {
		t.Errorf("expected static-msg in output, got: %s", buf.String())
	}
}

// TestStdLogger_log_Empty covers the path where log is called with
// no keysAndValues (just a static message).
func TestStdLogger_log_Empty(t *testing.T) {
	buf := captureStderr(t)
	NewStdLogger("svc", LevelDebug).Info("static")
	if !strings.Contains(buf.String(), "static") {
		t.Errorf("expected static in output, got: %s", buf.String())
	}
}

// TestStdLogger_FilterLevel covers the ShouldLog filter when level
// is set to LevelInfo. Debug must be filtered, Info must pass.
func TestStdLogger_FilterLevel(t *testing.T) {
	l := NewStdLogger("svc", LevelInfo).(*stdLogger)
	if l.ShouldLog(LevelDebug) {
		t.Error("expected Debug to be filtered at Info level")
	}
	if !l.ShouldLog(LevelInfo) {
		t.Error("expected Info to pass at Info level")
	}
	if !l.ShouldLog(LevelWarn) {
		t.Error("expected Warn to pass at Info level")
	}
}

// TestStdLogger_log_NotCalledWhenLevelTooLow covers the
// shouldLog-returns-false branch: nothing should be written.
func TestStdLogger_log_NotCalledWhenLevelTooLow(t *testing.T) {
	buf := captureStderr(t)
	NewStdLogger("svc", LevelError).Debug("should-not-appear")
	if buf.Len() != 0 {
		t.Errorf("expected empty output, got: %s", buf.String())
	}
}

// TestStdLogger_logf_NotCalledWhenLevelTooLow covers the
// shouldLog-returns-false branch for logf.
func TestStdLogger_logf_NotCalledWhenLevelTooLow(t *testing.T) {
	buf := captureStderr(t)
	NewStdLogger("svc", LevelError).Debugf("should-not-appear %s", "x")
	if buf.Len() != 0 {
		t.Errorf("expected empty output, got: %s", buf.String())
	}
}

// TestStdLogger_NewStdLogger_DefaultLevel covers the default
// LevelInfo when no level argument is provided (#11 回归)。
func TestStdLogger_NewStdLogger_DefaultLevel(t *testing.T) {
	l := NewStdLogger("svc").(*stdLogger)
	if l.Level != LevelInfo {
		t.Errorf("expected default level=LevelInfo, got %d", l.Level)
	}
}

// TestStdLogger_NewStdLogger_WithLevel covers passing a level
// argument.
func TestStdLogger_NewStdLogger_WithLevel(t *testing.T) {
	l := NewStdLogger("svc", LevelWarn).(*stdLogger)
	if l.Level != LevelWarn {
		t.Errorf("expected level=LevelWarn, got %d", l.Level)
	}
}

// TestStdLogger_WithGroup_NoOp verifies WithGroup is a no-op for
// the stdLogger (which doesn't support groups).
func TestStdLogger_WithGroup_NoOp(t *testing.T) {
	l := NewStdLogger("svc", LevelDebug)
	g := l.WithGroup("g")
	if g == nil {
		t.Fatal("expected non-nil logger from WithGroup")
	}
	// We just verify the call doesn't panic; the resulting logger
	// should behave like the original.
	g.Info("post-group")
}

// Use os.Stderr reference so go vet doesn't complain about unused.
var _ = os.Stderr
