package logx

import (
	"encoding/json"
	"errors"
	"fmt"
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
