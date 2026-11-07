package logx

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// readJSONRecord 从捕获的 stdout 输出解析出首条 JSON 记录。
func readJSONRecord(t *testing.T, out string) map[string]any {
	t.Helper()
	line := strings.TrimSpace(out)
	if line == "" {
		t.Fatal("无日志输出")
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		t.Fatalf("输出不是合法 JSON: %v\n%s", err, line)
	}
	return rec
}

// TestSlogGroup_BuiltinJSONPrefix 钉住 #75：内置 JSON 路径（默认配置，无
// WithHandler）必须应用 WithGroup 命名空间——此前 emit 全函数不读 group，
// 默认路径下组被整层静默丢弃，依赖 group 做字段聚合（Loki/ELK）全断。
func TestSlogGroup_BuiltinJSONPrefix(t *testing.T) {
	read, _ := captureStdout(t)
	l := NewSlogLogger("svc").
		WithGroup("http").
		WithAttrs(Attr{Key: "method", Value: "GET"})
	l.Info("request", "path", "/a")

	rec := readJSONRecord(t, read())
	if rec["http.method"] != "GET" {
		t.Errorf("http.method = %v（WithAttrs 键未加组前缀）: %v", rec["http.method"], rec)
	}
	if rec["http.path"] != "/a" {
		t.Errorf("http.path = %v（kv 键未加组前缀）: %v", rec["http.path"], rec)
	}
	// 框架字段不进组
	if _, ok := rec["message"]; !ok || rec["message"] != "request" {
		t.Errorf("框架字段 message 被加前缀或丢失: %v", rec)
	}
	if _, ok := rec["http.level"]; ok {
		t.Errorf("框架字段 level 不应进组: %v", rec)
	}
}

// TestSlogGroup_ChainAccumulates 钉住 #83：链式 WithGroup("a").WithGroup("b")
// 语义为嵌套命名空间 a.b——此前单 string 槽位静默只留最后一层。
func TestSlogGroup_ChainAccumulates(t *testing.T) {
	read, _ := captureStdout(t)
	NewSlogLogger("svc").WithGroup("a").WithGroup("b").Info("m", "k", 1)

	rec := readJSONRecord(t, read())
	if rec["a.b.k"] != float64(1) {
		t.Errorf("a.b.k = %v, 期望嵌套前缀落地: %v", rec["a.b.k"], rec)
	}
	if _, ok := rec["b.k"]; ok {
		t.Errorf("只留最后一层（b.k 存在）: %v", rec)
	}
}

// TestSlogGroup_HandlerPathChained 钉住 #83+#99 handler 路径：链式组以
// slog.Group 嵌套表达（a 包 b 包用户字段），而非只留最后一层；框架字段
// 不进组（#99：此前整体 h.WithGroup 把框架字段一并嵌进组）。
func TestSlogGroup_HandlerPathChained(t *testing.T) {
	var buf bytes.Buffer
	NewSlogLogger("svc", WithHandler(slog.NewJSONHandler(&buf, nil))).
		WithGroup("a").WithGroup("b").Info("m", "k", 1)

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("输出不是合法 JSON: %v\n%s", err, buf.String())
	}
	a, ok := rec["a"].(map[string]any)
	if !ok {
		t.Fatalf("a 组未落地: %v", rec)
	}
	b, ok := a["b"].(map[string]any)
	if !ok {
		t.Fatalf("a.b 嵌套组未落地（只留最后一层？）: %v", rec)
	}
	if b["k"] != float64(1) {
		t.Errorf("a.b.k = %v: %v", b["k"], rec)
	}
	if rec["service"] != "svc" {
		t.Errorf("框架字段 service 应在顶层: %v", rec)
	}
	if rec["msg"] != "m" {
		t.Errorf("框架字段 message（slog JSONHandler 渲染为 msg）应在顶层: %v", rec)
	}
}

// TestSlogGroup_EmptyNameNoop 对齐 slog.Handler.WithGroup：空组名是 no-op。
func TestSlogGroup_EmptyNameNoop(t *testing.T) {
	read, _ := captureStdout(t)
	NewSlogLogger("svc").WithGroup("").Info("m", "k", 1)

	rec := readJSONRecord(t, read())
	if rec["k"] != float64(1) {
		t.Errorf("空组名应无前缀, k = %v: %v", rec["k"], rec)
	}
}

// TestSlogGroup_NamespacesReservedKeys 组内用户键与框架字段同名时不再冲突：
// 前缀即命名空间（"g.time" 不撞框架 "time"）。
func TestSlogGroup_NamespacesReservedKeys(t *testing.T) {
	read, _ := captureStdout(t)
	NewSlogLogger("svc").WithGroup("g").Info("m", "time", "user-ts")

	rec := readJSONRecord(t, read())
	if rec["g.time"] != "user-ts" {
		t.Errorf("g.time = %v: %v", rec["g.time"], rec)
	}
	if _, ok := rec["_renamed"]; ok {
		t.Errorf("组内键不应触发改名标记: %v", rec)
	}
	ts, _ := rec["time"].(string)
	if ts == "" || ts == "user-ts" {
		t.Errorf("框架 time 被污染: %v", rec)
	}
}

// TestSlogGroup_NoGroupUnprefixed 护栏：不带组时键平铺（既有行为不变）。
func TestSlogGroup_NoGroupUnprefixed(t *testing.T) {
	read, _ := captureStdout(t)
	NewSlogLogger("svc").Info("m", "k", 1)

	rec := readJSONRecord(t, read())
	if rec["k"] != float64(1) {
		t.Errorf("k = %v: %v", rec["k"], rec)
	}
}

// TestSlogGroup_CtxPathAlsoGrouped 护栏：Ctx* 方法同样走组前缀。
func TestSlogGroup_CtxPathAlsoGrouped(t *testing.T) {
	read, _ := captureStdout(t)
	NewSlogLogger("svc").WithGroup("g").CtxInfo(context.Background(), "m", "k", 1)

	rec := readJSONRecord(t, read())
	if rec["g.k"] != float64(1) {
		t.Errorf("g.k = %v: %v", rec["g.k"], rec)
	}
}
