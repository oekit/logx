package logx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	stdlog "log"
	"math"
	"strings"
	"testing"
)

// assertRecordField 断言输出是合法 JSON 且指定字段等于期望值。
func assertRecordField(t *testing.T, out, key string, want any) {
	t.Helper()
	line := strings.TrimSpace(out)
	if line == "" {
		t.Fatal("无日志输出")
	}
	var record map[string]any
	if err := json.Unmarshal([]byte(line), &record); err != nil {
		t.Fatalf("输出不是合法 JSON: %v\n%s", err, line)
	}
	got, ok := record[key]
	if !ok {
		t.Fatalf("字段 %q 缺失: %s", key, line)
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("字段 %q = %v, 期望 %v\n%s", key, got, want, line)
	}
}

type kvCustomErr struct{ code int }

func (e kvCustomErr) Error() string { return fmt.Sprintf("custom-%d", e.code) }

// TestSlogLogger_ErrorValueRendering kv 中的 error 值应渲染为错误文本而非 "{}"
// （issue #2；对齐 log/slog JSONHandler 的 error 处理）。
func TestSlogLogger_ErrorValueRendering(t *testing.T) {
	restore := setTestExtractor()
	defer restore()

	tests := []struct {
		name string
		kv   []any
		key  string
		want any
	}{
		{"普通错误", []any{"err", errors.New("boom")}, "err", "boom"},
		{"包装错误链", []any{"err", fmt.Errorf("read config: %w", errors.New("no such file"))}, "err", "read config: no such file"},
		{"自定义错误类型", []any{"err", kvCustomErr{code: 7}}, "err", "custom-7"},
		{"非错误数值原样", []any{"code", 500}, "code", float64(500)},
		{"非错误字符串原样", []any{"op", "open"}, "op", "open"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			read, _ := captureStdout(t)
			NewSlogLogger("svc").Error("create failed", tt.kv...)
			out := read()
			assertRecordField(t, out, tt.key, tt.want)
		})
	}
}

// TestSlogLogger_ErrorAttrRendering WithAttrs 的 error 值同样渲染为错误文本。
func TestSlogLogger_ErrorAttrRendering(t *testing.T) {
	restore := setTestExtractor()
	defer restore()

	logger := NewSlogLogger("svc").WithAttrs(Attr{Key: "err", Value: errors.New("attr-boom")})
	read, _ := captureStdout(t)
	logger.Info("hello")
	out := read()
	assertRecordField(t, out, "err", "attr-boom")
}

// TestSlogLogger_NilKvValue 未类型化 nil 值渲染为 null，不被 error 分支吞掉。
func TestSlogLogger_NilKvValue(t *testing.T) {
	restore := setTestExtractor()
	defer restore()

	read, _ := captureStdout(t)
	NewSlogLogger("svc").Warn("maybe", "data", nil)
	out := read()
	assertRecordField(t, out, "data", nil)
}

// ---- #136：序列化降级 rawKv 的可读性 ----

// TestSlogLogger_FallbackRawKvKeepsKvSeparators 钉住 #136：序列化降级记录的
// rawKv 此前用 fmt.Sprint 直接拼接——键是 string 时相邻操作数不加空格，
// 键值糊成一体（实测 "rawKv":"nanNaNok1"），降级行（恰恰是排障最需要可读
// 的那条）无法按 k=v 读。改走 FormatKeysAndValues 后产出 "k1=v1 k2=v2"，
// 与 std/hlog 后端的 kv 渲染同一口径。
func TestSlogLogger_FallbackRawKvKeepsKvSeparators(t *testing.T) {
	restore := setTestExtractor()
	defer restore()

	read, _ := captureStdout(t)
	NewSlogLogger("svc").Error("boom", "nan", math.NaN(), "ok", 1)
	out := read()
	assertRecordField(t, out, "rawKv", "nan=NaN ok=1")
	assertRecordField(t, out, "message", "boom")
	// 降级行仍带序列化失败原因与级别（#100 的可检索性不回退）
	var record map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &record); err != nil {
		t.Fatalf("降级行不是合法 JSON: %v\n%s", err, out)
	}
	if s, _ := record["marshalErr"].(string); s == "" {
		t.Fatalf("降级行应保留 marshalErr: %s", out)
	}
	if s, _ := record["level"].(string); s != LevelString(LevelError) {
		t.Fatalf("降级行应保留级别: %s", out)
	}
}

// TestSlogLogger_FallbackRawKvMissingSlot #136 顺带补齐 MISSING 档：奇数
// 长度 kv 在降级 rawKv 里按 FormatKeysAndValues 渲染为 "(MISSING)"。
func TestSlogLogger_FallbackRawKvMissingSlot(t *testing.T) {
	restore := setTestExtractor()
	defer restore()

	read, _ := captureStdout(t)
	NewSlogLogger("svc").Error("boom", "k1", math.NaN(), "lonely")
	out := read()
	assertRecordField(t, out, "rawKv", "k1=NaN lonely=(MISSING)")
}

// ---- #137：Error() panic 隔离 ----

// panicErr 是 Error() 实现 panic 的 evil error（#137 复现形态）。
type panicErr struct{}

func (panicErr) Error() string { panic("Error() explodes") }

// mustNotPanic 执行 fn，panic 逃逸则让测试失败并带上 panic 值。
func mustNotPanic(t *testing.T, name string, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("%s: panic 逃逸出日志调用点（#137）: %v", name, r)
		}
	}()
	fn()
}

// TestLogValue_ErrorPanicIsolated 单元钉住 #137：logValue 对 Error() panic
// 的 error 降级为占位串，不向上传播；普通 error 渲染不受影响。
func TestLogValue_ErrorPanicIsolated(t *testing.T) {
	if got := logValue(panicErr{}); got != "<error.String panicked: Error() explodes>" {
		t.Fatalf("logValue(panicErr{}) = %q, want panic 隔离占位串", got)
	}
	if got := logValue(errors.New("normal")); got != "normal" {
		t.Fatalf("普通 error 渲染不受影响: got %q", got)
	}
}

// TestSlogLogger_ErrorValuePanicDoesNotEscape 端到端钉住 #137：同一 error
// 值在 slog 后端的三条路径（内置 JSON、注入 Handler、WithAttrs 持久属性）
// 上都让日志调用正常返回且落占位串——此前 panic 从序列化阶段直接逃逸出
// 日志调用点打崩调用方 goroutine，且与 std 后端行为分裂（std 走 fmt 恒
// 正常返回）。
func TestSlogLogger_ErrorValuePanicDoesNotEscape(t *testing.T) {
	restore := setTestExtractor()
	defer restore()

	const want = "<error.String panicked: Error() explodes>"

	// 内置 JSON 路径
	mustNotPanic(t, "内置 JSON 路径", func() {
		read, _ := captureStdout(t)
		l := NewSlogLogger("probe")
		l.Error("boom", ErrKey, panicErr{})
		out := read()
		assertRecordField(t, out, "err", want)
	})

	// 注入 Handler 路径
	h := newCaptureHandler()
	lh := NewSlogLogger("probe", WithHandler(h))
	mustNotPanic(t, "注入 Handler 路径", func() { lh.Error("boom", ErrKey, panicErr{}) })
	recs := h.snapshot()
	if len(recs) != 1 {
		t.Fatalf("want 1 record, got %d", len(recs))
	}
	if v, ok := recs[0]["err"]; !ok || fmt.Sprint(v) != want {
		t.Fatalf("Handler 路径 err = %v, want %q", v, want)
	}

	// WithAttrs 持久属性路径（内置 JSON）
	mustNotPanic(t, "WithAttrs 持久属性路径", func() {
		read, _ := captureStdout(t)
		la := NewSlogLogger("probe").WithAttrs(Attr{Key: ErrKey, Value: panicErr{}})
		la.Info("boom")
		out := read()
		assertRecordField(t, out, "err", want)
	})
}

// TestStdLogger_ErrorValuePanicStillContained std 后端回归护栏（#137）：
// fmt 把 Error() 的 panic 收敛成 %!v(PANIC=...) 字面量、日志调用正常返回
// ——修复后两后端对同一输入都不再让 panic 逃逸，行为重新一致。
func TestStdLogger_ErrorValuePanicStillContained(t *testing.T) {
	restore := setTestExtractor()
	defer restore()

	var buf bytes.Buffer
	oldWriter := stdlog.Writer()
	stdlog.SetOutput(&buf)
	defer stdlog.SetOutput(oldWriter)

	mustNotPanic(t, "std 后端", func() {
		NewStdLogger("probe").Error("boom", ErrKey, panicErr{})
	})
	if !strings.Contains(buf.String(), "%!v(PANIC=") {
		t.Fatalf("std 后端应把 panic 收敛为 %%!v(PANIC=...) 字面量, got: %s", buf.String())
	}
}
