package logx

import (
	"context"
	"log/slog"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
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

// TestSlogHandler_FastPathSkipsCallerScan 钉住 #130：桥接快路径（后端为
// slog 后端，logxBridge 直通 r.PC）此前在 emitToHandler 里**无条件**先执行
// 一次 realCallerOutsideLogx() 全栈回溯，结果随即被 bridgePC 覆盖丢弃——
// #84 宣称的「不再栈回溯」对注入 Handler 路径不成立，快路径每条记录仍
// 白付 #87 实测 465-1515ns/op 的回溯成本。值语义全对所以既有测试没抓到，
// 这里以测试期探针（callerScanProbe，生产恒 nil）计数回溯触发次数钉住：
// 快路径 0 次；bridgePC==0 的本地路径仍回溯、caller 归属不回退。
func TestSlogHandler_FastPathSkipsCallerScan(t *testing.T) {
	var scans atomic.Int64
	prev := callerScanProbe
	callerScanProbe = func() { scans.Add(1) }
	t.Cleanup(func() { callerScanProbe = prev })

	// 快路径：Record.PC 非 0 直通 emitToHandler，不得触发全栈回溯。
	h := newCaptureHandler()
	backend := NewSlogLogger("svc", WithHandler(h))
	thisPC, _, line, _ := runtime.Caller(0)
	record := slog.NewRecord(time.Now(), slog.LevelInfo, "fast-path", thisPC)
	record.AddAttrs(slog.String("k", "v"))
	if err := SlogHandler(backend).Handle(context.Background(), record); err != nil {
		t.Fatal(err)
	}

	if n := scans.Load(); n != 0 {
		t.Fatalf("桥接快路径触发全栈回溯 %d 次, want 0（#130：bridgePC 非 0 不得回溯）", n)
	}
	recs := h.snapshot()
	if len(recs) != 1 {
		t.Fatalf("records = %d, want 1", len(recs))
	}
	// 值语义不回退：caller 仍归属桥接侧传入的 PC（本测试文件该行）。
	caller, _ := recs[0]["caller"].(string)
	if !strings.Contains(caller, "slogger_bridge_test.go") {
		t.Fatalf("caller = %q, want 归属 Record.PC 指向的本测试文件（行 %d）", caller, line)
	}

	// 本地直调路径（bridgePC==0）：仍走全栈回溯，探测保活。
	backend.Info("slow-path")
	if n := scans.Load(); n == 0 {
		t.Fatal("本地路径未触发回溯，探测失效（caller 归属路径被误伤）")
	}
}

// stringValuer / groupValuer 是 slog.LogValuer 测试桩（#238）。
type stringValuer string

func (v stringValuer) LogValue() slog.Value { return slog.StringValue(string(v)) }

type groupValuer struct{}

func (groupValuer) LogValue() slog.Value {
	return slog.GroupValue(slog.String("a", "b"), slog.Int("n", 1))
}

// TestSlogHandler_ExpandsGroupsAndLogValuer 回归（#238）：桥接层此前直接
//
//	append(a.Key, a.Value.Any())：KindGroup 的 Any() 是 []slog.Attr，内置
//	JSON 后端 json.Marshal 后渲染成 {"Value":{}}（slog.Attr/Value 无导出
//	字段、无 MarshalJSON），经 logx.Slogger 接入的三方库（WithGroup /
//	slog.Group）嵌套字段静默变空对象；KindLogValuer 也未 Resolve。
//
// 修复后 group 递归平铺为点分键（与内置 WithGroup 口径一致）、LogValuer
// 显式 Resolve（含解析结果为 group 的形态），空键组内联。
func TestSlogHandler_ExpandsGroupsAndLogValuer(t *testing.T) {
	base := &fakeLogger{}
	sl := Slogger(base)

	sl.Info("nested",
		slog.Group("http",
			slog.String("method", "GET"),
			slog.Group("peer", slog.String("ip", "10.0.0.1")),
		),
		slog.Any("user", stringValuer("alice")),
		slog.Any("gv", groupValuer{}),
		slog.Group("", slog.String("flat", "v")),
	)
	// WithAttrs 路径：挂载时刻固化组前缀，组内嵌套同样展开
	sl.WithGroup("outer").With(
		slog.Group("cfg", slog.Group("db", slog.String("dsn", "postgres://x"))),
	).Info("with-attrs", slog.Group("rec", slog.Int("n", 7)))

	if len(base.entries) != 2 {
		t.Fatalf("entries = %d, want 2: %v", len(base.entries), base.text())
	}
	cases := []struct {
		idx  int
		want []any
	}{
		{0, []any{
			"http.method", "GET",
			"http.peer.ip", "10.0.0.1",
			"user", "alice",
			"gv.a", "b",
			"gv.n", int64(1),
			"flat", "v",
		}},
		{1, []any{"outer.cfg.db.dsn", "postgres://x", "outer.rec.n", int64(7)}},
	}
	for _, tc := range cases {
		got := base.entries[tc.idx].kvs
		if len(got) != len(tc.want) {
			t.Fatalf("entry %d kvs = %v, want %v", tc.idx, got, tc.want)
		}
		for i := range tc.want {
			if got[i] != tc.want[i] {
				t.Fatalf("entry %d kvs[%d] = %v, want %v (all: %v)", tc.idx, i, got[i], tc.want[i], got)
			}
		}
		for i := 0; i < len(got); i += 2 {
			if _, isGroup := got[i+1].([]slog.Attr); isGroup {
				t.Fatalf("entry %d 残留未展开的 KindGroup 值: %v", tc.idx, got[i+1])
			}
		}
	}
}

// TestSlogHandler_GroupToJSONNotNull 端到端钉住 #238 的原始症状：内置 JSON
// 后端输出嵌套组时必须是平铺字段，绝不能出现 {"Value":{}} 空对象。
func TestSlogHandler_GroupToJSONNotNull(t *testing.T) {
	read, _ := captureStdout(t)
	sl := Slogger(NewSlogLogger("svc"))

	sl.Info("group-json", slog.Group("http", slog.String("method", "GET")))

	out := read()
	rec := readJSONRecord(t, out)
	if rec["http.method"] != "GET" {
		t.Fatalf("nested group not flattened: %v (%s)", rec, out)
	}
	if strings.Contains(out, `"Value"`) {
		t.Fatalf("KindGroup leaked as slog.Value object: %s", out)
	}
}

// TestSlogHandler_WrappedChainCaller 见 slog_caller_ext_test.go（外部测试包）：
// 包装链（auditLogger，未实现 PC 直通）走栈回溯路径，需真实包外探针帧。
