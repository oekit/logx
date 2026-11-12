package logx

import (
	"errors"
	"testing"
)

// #175：FormatWithTraceID / FormatAttrs 是根包导出 API 且在 hlog/std 的
// printf 热路径被依赖，此前零直接测试（只被 hlog 包测试间接覆盖）。

func TestFormatWithTraceID(t *testing.T) {
	tests := []struct {
		name    string
		traceID string
		format  string
		args    []any
		want    string
	}{
		{
			name:    "空 traceID 无前缀",
			traceID: "",
			format:  "hello %s",
			args:    []any{"world"},
			want:    "hello world",
		},
		{
			name:    "非空 traceID 加前缀",
			traceID: "abc123",
			format:  "hello %s",
			args:    []any{"world"},
			want:    "[trace=abc123] hello world",
		},
		{
			name:    "traceID 非空且无格式参数",
			traceID: "t-1",
			format:  "plain",
			args:    nil,
			want:    "[trace=t-1] plain",
		},
		{
			name:    "traceID 非空且数值参数",
			traceID: "t-2",
			format:  "code=%d ok=%v",
			args:    []any{500, true},
			want:    "[trace=t-2] code=500 ok=true",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatWithTraceID(tt.traceID, tt.format, tt.args...)
			if got != tt.want {
				t.Errorf("FormatWithTraceID(%q, %q, ...) = %q, want %q", tt.traceID, tt.format, got, tt.want)
			}
		})
	}
}

func TestFormatAttrs(t *testing.T) {
	tests := []struct {
		name  string
		attrs []Attr
		want  string
	}{
		{
			name:  "0 个 Attr 返回空串",
			attrs: nil,
			want:  "",
		},
		{
			name:  "1 个 Attr",
			attrs: []Attr{{Key: "k", Value: "v"}},
			want:  "k=v",
		},
		{
			name:  "N 个 Attr 空格分隔",
			attrs: []Attr{{Key: "a", Value: 1}, {Key: "b", Value: true}, {Key: "c", Value: "x"}},
			want:  "a=1 b=true c=x",
		},
		{
			name:  "%v 值渲染（error 复合值）",
			attrs: []Attr{{Key: "e", Value: errors.New("boom")}},
			want:  "e=boom",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatAttrs(tt.attrs...)
			if got != tt.want {
				t.Errorf("FormatAttrs(...) = %q, want %q", got, tt.want)
			}
		})
	}
}

// #174：kv 键归一化 string 键直取、非 string 键回退 %v，两分支口径一致。
func TestKeyString(t *testing.T) {
	if got := keyString("plain"); got != "plain" {
		t.Errorf("keyString(string) = %q, want %q", got, "plain")
	}
	if got := keyString(42); got != "42" {
		t.Errorf("keyString(int) = %q, want %q", got, "42")
	}
	type customKey struct{ n int }
	if got := keyString(customKey{n: 7}); got != "{7}" {
		t.Errorf("keyString(struct) = %q, want %q", got, "{7}")
	}
}
