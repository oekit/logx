package logx

import (
	"context"
	"log/slog"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestSlogHandler_CustomLevelThresholds 钉住 #84：非四档命名级别按阈值映射——
// slog.LevelError+2（比 ERROR 更严重）必须落 Error，不再 default 落 Info；
// WARN/INFO 之间的自定义级别落 Info；低于 DEBUG 的落 Debug。
func TestSlogHandler_CustomLevelThresholds(t *testing.T) {
	base := &fakeLogger{}
	sl := Slogger(base)

	ctx := context.Background()
	sl.Log(ctx, slog.LevelError+2, "fatal-ish")
	sl.Log(ctx, slog.LevelError, "err")
	sl.Log(ctx, slog.LevelWarn-2, "info-ish")
	sl.Log(ctx, slog.LevelWarn, "warn")
	sl.Log(ctx, slog.LevelDebug-4, "trace-ish")
	sl.Log(ctx, slog.LevelDebug, "debug")

	want := []struct {
		msg string
		lvl Level
	}{
		{"fatal-ish", LevelError},
		{"err", LevelError},
		{"info-ish", LevelInfo},
		{"warn", LevelWarn},
		{"trace-ish", LevelDebug},
		{"debug", LevelDebug},
	}
	if len(base.entries) != len(want) {
		t.Fatalf("records = %d, want %d: %v", len(base.entries), len(want), base.text())
	}
	for i, w := range want {
		got := base.entries[i]
		if got.msg != w.msg || got.level != w.lvl {
			t.Errorf("entry %d = (%v,%s), want (%v,%s)", i, got.level, got.msg, w.lvl, w.msg)
		}
	}
}

// TestSlogHandler_WarnFloorNotSwallowed 钉住 #84：logx 设 WARN 档时，
// slog.LevelError+2 的致命记录不得被整条吞掉——此前被降为 INFO 后
// ShouldLog(Info) 拦截，记录数 = 0。
func TestSlogHandler_WarnFloorNotSwallowed(t *testing.T) {
	h := newCaptureHandler()
	backend := NewSlogLogger("svc", WithHandler(h), WithLevel(LevelWarn))
	sl := Slogger(backend)

	sl.Log(context.Background(), slog.LevelError+2, "fatal-ish")

	recs := h.snapshot()
	if len(recs) != 1 {
		t.Fatalf("WARN 档下 Error+2 记录被吞, got %d records", len(recs))
	}
	if h.state.lvls[0] != slog.LevelError {
		t.Errorf("record level = %v, want ERROR", h.state.lvls[0])
	}
}

// TestSlogHandler_CallerFromCallsite 钉住 #84：桥接日志的 caller 归属用户
// 调用点——此前 Handle 忽略 r.PC 走栈回溯，caller 恒为 log/slog/logger.go
// 的 stdlib 源码行。
func TestSlogHandler_CallerFromCallsite(t *testing.T) {
	read, _ := captureStdout(t)
	sl := Slogger(NewSlogLogger("svc"))
	sl.Info("bridge-msg")

	rec := readJSONRecord(t, read())
	caller, _ := rec["caller"].(string)
	if caller == "" {
		t.Fatal("caller 字段缺失")
	}
	if strings.Contains(caller, "log/slog") {
		t.Fatalf("caller 错标到 stdlib slog 源码: %s", caller)
	}
	if !strings.Contains(caller, "slogger_bridge_test.go") {
		t.Fatalf("caller 应归属用户调用点: %s", caller)
	}
}

// TestSlogHandler_PreservesRecordTimeAndPC 钉住 #84：桥接直通复用
// r.Time / r.PC——手工构造 Record（固定时间戳与 PC）经 Handle 后，
// 输出的 time/caller 与 Record 一致，而非输出时刻的时间与回溯 PC。
func TestSlogHandler_PreservesRecordTimeAndPC(t *testing.T) {
	fixed := time.Date(2026, 11, 6, 23, 59, 59, 123456789, time.UTC)
	thisPC, _, line, _ := runtime.Caller(0) // 本行 PC：caller 应指向该行
	record := slog.NewRecord(fixed, slog.LevelInfo, "preserved", thisPC)

	read, _ := captureStdout(t)
	h := SlogHandler(NewSlogLogger("svc"))
	if err := h.Handle(context.Background(), record); err != nil {
		t.Fatal(err)
	}

	rec := readJSONRecord(t, read())
	ts, _ := rec["time"].(string)
	if ts == "" {
		t.Fatal("time 字段缺失")
	}
	got, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		t.Fatalf("time 不是 RFC3339Nano: %v", err)
	}
	if !got.Equal(fixed) {
		t.Fatalf("time = %v, want 复用 Record 的 %v（而非输出时刻）", got, fixed)
	}
	caller, _ := rec["caller"].(string)
	if !strings.Contains(caller, "slogger_bridge_test.go") {
		t.Fatalf("caller = %q, want 归属 Record.PC 指向的测试文件", caller)
	}
	wantLine := "slogger_bridge_test.go:" + strconv.Itoa(line)
	if caller != wantLine {
		t.Logf("caller = %q, PC 行 %d（PC 指向 runtime.Caller(0) 所在行）", caller, line)
	}
}

// TestSlogHandler_WrappedChainCaller 见 slog_caller_ext_test.go（外部测试包）：
// 包装链（auditLogger，未实现 PC 直通）走栈回溯路径，需真实包外探针帧。
