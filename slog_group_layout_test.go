package logx

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
)

// TestSlogGroup_HandlerPathFrameworkFieldsTopLevel（#99）：注入 Handler 路径
// 的框架字段（service/trace_id/caller）必须保持记录顶层——与内置 JSON 路径
// 同一层级。此前 WithGroup 把整个 handler 组化，service/trace_id/caller 作为
// record attr 一并被嵌进组：同一 logger 切后端即换 schema，按 .service/
// .trace_id 检索的链路在 Handler 路径上直接断掉。
func TestSlogGroup_HandlerPathFrameworkFieldsTopLevel(t *testing.T) {
	restore := setTestExtractor()
	defer restore()

	// 注入 Handler 路径：用户字段进 g 组（嵌套对象），框架字段在顶层
	var buf bytes.Buffer
	l := NewSlogLogger("svc", WithHandler(slog.NewJSONHandler(&buf, nil))).WithGroup("g")
	l.CtxInfo(withTestTrace(context.Background(), "trace-abc"), "m", "k", "v")

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("输出不是合法 JSON: %v\n%s", err, buf.String())
	}
	if rec["service"] != "svc" {
		t.Errorf("Handler 路径 service 应在顶层: %v", rec)
	}
	if rec["trace_id"] != "trace-abc" {
		t.Errorf("Handler 路径 trace_id 应在顶层: %v", rec)
	}
	if c, _ := rec["caller"].(string); c == "" {
		t.Errorf("Handler 路径 caller 应在顶层: %v", rec)
	}
	g, ok := rec["g"].(map[string]any)
	if !ok {
		t.Fatalf("用户字段未以 g 组落地: %v", rec)
	}
	if g["k"] != "v" {
		t.Errorf("g.k = %v: %v", g["k"], rec)
	}
	for _, f := range []string{"service", "trace_id", "caller"} {
		if _, dup := g[f]; dup {
			t.Errorf("框架字段 %s 不应同时出现在组内", f)
		}
	}

	// 对照：同一 logger 语义在内置 JSON 路径下框架字段同样在顶层
	// （用户字段以 "g." 前缀平铺，见 WithGroup godoc 的双路径口径）。
	read, _ := captureStdout(t)
	builtin := NewSlogLogger("svc").WithGroup("g")
	builtin.CtxInfo(withTestTrace(context.Background(), "trace-abc"), "m", "k", "v")

	brec := readJSONRecord(t, read())
	if brec["service"] != "svc" {
		t.Errorf("内置路径 service 应在顶层: %v", brec)
	}
	if brec["trace_id"] != "trace-abc" {
		t.Errorf("内置路径 trace_id 应在顶层: %v", brec)
	}
	if brec["g.k"] != "v" {
		t.Errorf("内置路径 g.k = %v: %v", brec["g.k"], brec)
	}
}
