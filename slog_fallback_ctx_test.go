package logx

import (
	"context"
	"math"
	"strings"
	"testing"
)

// TestSlogFallbackKeepsCorrelationFields（#100）：序列化失败的降级记录必须
// 「降级但不脱钩」——service/trace_id/caller 与 WithAttrs 持久属性一并带上
// （值统一转字符串），降级原因（marshalErr）写进记录。此前 fallback 只含
// level/message/marshalErr/rawKv：指标日志里常见的 NaN 一次就让整条日志
// 再也关联不到 trace。
func TestSlogFallbackKeepsCorrelationFields(t *testing.T) {
	restore := setTestExtractor()
	defer restore()

	read, _ := captureStdout(t)
	l := NewSlogLogger("svc").WithAttrs(Attr{Key: "request_id", Value: "req-1"})
	l.CtxInfo(withTestTrace(context.Background(), "trace-abc"), "m",
		"duration", math.NaN(), "status", "ok")

	rec := readJSONRecord(t, read())
	if rec["service"] != "svc" {
		t.Errorf("降级记录丢 service: %v", rec)
	}
	if rec["trace_id"] != "trace-abc" {
		t.Errorf("降级记录丢 trace_id: %v", rec)
	}
	if c, _ := rec["caller"].(string); c == "" {
		t.Errorf("降级记录丢 caller: %v", rec)
	}
	if rec["request_id"] != "req-1" {
		t.Errorf("降级记录丢 WithAttrs 持久属性: %v", rec)
	}
	me, _ := rec["marshalErr"].(string)
	if !strings.Contains(me, "NaN") {
		t.Errorf("降级原因（NaN 不可序列化）未写进记录: %v", rec)
	}
	// 值统一转字符串：非字符串值不再参与序列化，不会再触发二次失败
	if _, ok := rec["request_id"].(string); !ok {
		t.Errorf("持久属性值应转字符串: %v", rec)
	}
}
