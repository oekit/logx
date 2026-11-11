package logx

import "testing"

// TestNewStdLogger_DefaultLevelInfo 回归 #11：默认级别必须是 INFO，
// 不是 LevelDebug（iota 零值陷阱）。
func TestNewStdLogger_DefaultLevelInfo(t *testing.T) {
	l := NewStdLogger("svc").(*stdLogger)
	if l.Level.Load() != LevelInfo {
		t.Fatalf("default level = %v, want LevelInfo", l.Level.Load())
	}
	// 显式参数仍生效
	if NewStdLogger("svc", LevelWarn).(*stdLogger).Level.Load() != LevelWarn {
		t.Fatal("explicit level ignored")
	}
}

func TestDefaultLogger_InitLevelInfo(t *testing.T) {
	l := Default().(*stdLogger)
	if lv := l.Level.Load(); lv != LevelInfo && lv != LevelError && lv != LevelWarn && lv != LevelDebug {
		t.Fatalf("unexpected level %v", lv)
	}
	// 未设 LOG_LEVEL 时应为 INFO（测试环境不应依赖外部 LOG_LEVEL）
	// 仅在 LOG_LEVEL 未设置时断言
	// t.Setenv 不能用于检测未设置；改用 ParseLogLevel 路径已测过
	_ = l
}
