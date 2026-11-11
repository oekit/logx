package hlog

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/oekit/logx"
)

// TestToHlogLevel 钉住 #161：initHlogLevel/SetLevel 共用一份级别映射——
// hlog 无 OFF 档，LevelOff 落 Error（#66/#102 口径，绝不放宽到 Info）。
func TestToHlogLevel(t *testing.T) {
	cases := []struct {
		in   logx.Level
		want hlog.Level
	}{
		{logx.LevelDebug, hlog.LevelDebug},
		{logx.LevelInfo, hlog.LevelInfo},
		{logx.LevelWarn, hlog.LevelWarn},
		{logx.LevelError, hlog.LevelError},
		{logx.LevelOff, hlog.LevelError}, // 无 OFF 档：收到 Error 兜底
	}
	for _, c := range cases {
		if got := toHlogLevel(c.in); got != c.want {
			t.Errorf("toHlogLevel(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestNewHlogLoggerWith_LevelPriority 钉住 #167：WithHlogLevel > LOG_LEVEL >
// Info，与 std/slog 后端的文档口径一致；无参旧形态行为不变。
func TestNewHlogLoggerWith_LevelPriority(t *testing.T) {
	t.Setenv("LOG_LEVEL", "WARN")
	if got := NewHlogLoggerWith("s").(*hlogLogger).Level.Load(); got != logx.LevelWarn {
		t.Fatalf("LOG_LEVEL should apply: got %v", got)
	}
	if got := NewHlogLoggerWith("s", WithHlogLevel(logx.LevelDebug)).(*hlogLogger).Level.Load(); got != logx.LevelDebug {
		t.Fatalf("WithHlogLevel should win over LOG_LEVEL: got %v", got)
	}
	t.Setenv("LOG_LEVEL", "")
	if got := NewHlogLoggerWith("s").(*hlogLogger).Level.Load(); got != logx.LevelInfo {
		t.Fatalf("default should be Info: got %v", got)
	}
	// 旧无参形态照常转发（显式确认 os.Getenv 读取路径未被破坏）
	if got := NewHlogLogger("s").(*hlogLogger).Level.Load(); got != logx.LevelInfo {
		t.Fatalf("legacy form must keep working: got %v", got)
	}
}

// TestHlogLogger_ExplicitLevelDrivesGlobal 钉住 #167 与 #161 的联动：
// WithHlogLevel 指定的级别经 initHlogLevel 同步到 hlog 进程全局级别。
func TestHlogLogger_ExplicitLevelDrivesGlobal(t *testing.T) {
	var buf syncBuffer
	hlog.SetOutput(&buf)
	t.Cleanup(func() { hlog.SetLogger(hlog.DefaultLogger()) })

	l := NewHlogLoggerWith("svc-opt", WithHlogLevel(logx.LevelWarn))
	l.Info("must-be-filtered-by-global-warn")
	hlog.CtxInfof(context.Background(), "GLOBAL-INFO-MUST-BE-FILTERED")
	hlog.CtxWarnf(context.Background(), "GLOBAL-WARN-MUST-PASS")
	out := buf.String()
	if strings.Contains(out, "must-be-filtered-by-global-warn") {
		t.Errorf("WithHlogLevel(Warn) 后实例 Info 不应输出: %s", out)
	}
	if strings.Contains(out, "GLOBAL-INFO-MUST-BE-FILTERED") {
		t.Errorf("WithHlogLevel(Warn) 后 hlog 全局 Info 不应输出: %s", out)
	}
	if !strings.Contains(out, "GLOBAL-WARN-MUST-PASS") {
		t.Errorf("WithHlogLevel(Warn) 后 hlog 全局 Warn 应输出: %s", out)
	}
}
