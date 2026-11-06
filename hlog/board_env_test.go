package hlog

import (
	"os"
	"testing"

	"github.com/oekit/logx"
)

// TestHlogLogger_ReadsLogLevelEnv 钉住 #66：NewHlogLogger 实例级别读 LOG_LEVEL
// （含 OFF 档——此前 switch 无 OFF 分支静默落 Info，LOG_LEVEL=OFF 下 Info 照打）。
func TestHlogLogger_ReadsLogLevelEnv(t *testing.T) {
	old := os.Getenv("LOG_LEVEL")
	defer os.Setenv("LOG_LEVEL", old)

	os.Setenv("LOG_LEVEL", "DEBUG")
	l := NewHlogLogger("s").(*hlogLogger)
	if l.Level != logx.LevelDebug {
		t.Fatalf("LOG_LEVEL=DEBUG ⇒ LevelDebug, got %v", l.Level)
	}

	os.Setenv("LOG_LEVEL", "OFF")
	l = NewHlogLogger("s").(*hlogLogger)
	if l.Level != logx.LevelOff {
		t.Fatalf("LOG_LEVEL=OFF ⇒ LevelOff, got %v", l.Level)
	}
	// hlog 无 OFF 档：进程全局级别抬到 Error 兜底（initHlogLevel OFF 分支），
	// 输出面由实例 ShouldLog 拦截
	if l.ShouldLog(logx.LevelInfo) {
		t.Fatal("OFF 档下实例 ShouldLog(LevelInfo) 应拦截")
	}
	if l.ShouldLog(logx.LevelError) {
		t.Fatal("OFF 档下实例 ShouldLog(LevelError) 应拦截")
	}

	os.Setenv("LOG_LEVEL", "INFO")
	l = NewHlogLogger("s").(*hlogLogger)
	if l.Level != logx.LevelInfo {
		t.Fatalf("LOG_LEVEL=INFO ⇒ LevelInfo, got %v", l.Level)
	}
}
