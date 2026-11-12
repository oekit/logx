package logx

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"testing"
)

// 测试用 KVCarrier 形态错误（oekit/pkg/errors wrappedError 的同构替身——
// logx 不 import pkg/errors，测试同样只用 duck-type 形状钉住对接面）。
type kvWrappedErr struct {
	msg  string
	kv   []any
	next error
}

func (e *kvWrappedErr) Error() string { return e.msg }
func (e *kvWrappedErr) Unwrap() error { return e.next }
func (e *kvWrappedErr) Kv() []any     { return e.kv }

// 测试用多分支链形态（errors.Join 的 Unwrap() []error 同款形状）。
type kvJoinErr struct{ errs []error }

func (e *kvJoinErr) Error() string { return "joined" }
func (e *kvJoinErr) Unwrap() []error {
	return e.errs
}

// 测试用成环链形态：self 指回自身。
type kvCyclicErr struct {
	kv   []any
	self error
}

func (e *kvCyclicErr) Error() string { return "cyclic" }
func (e *kvCyclicErr) Unwrap() error { return e.self }
func (e *kvCyclicErr) Kv() []any     { return e.kv }

// TestErrAttrs_NilAndNoCarrier 钉住空面：nil、无 KVCarrier 的普通错误、
// 非 Wrap 包装链一律返回 nil。
func TestErrAttrs_NilAndNoCarrier(t *testing.T) {
	if got := ErrAttrs(nil); got != nil {
		t.Fatalf("ErrAttrs(nil) 应返回 nil, got %v", got)
	}
	if got := ErrAttrs(errors.New("plain")); got != nil {
		t.Fatalf("无 kv 的普通错误应返回 nil, got %v", got)
	}
	// fmt.Errorf %w 包装链不携带 kv：一路 unwrap 到链底也没有 KVCarrier。
	wrapped := fmt.Errorf("io: %w", errors.New("no such file"))
	if got := ErrAttrs(wrapped); got != nil {
		t.Fatalf("非 KVCarrier 包装链应返回 nil, got %v", got)
	}
	// KVCarrier 层但 kv 为空切片：等价无 kv。
	if got := ErrAttrs(&kvWrappedErr{msg: "empty"}); got != nil {
		t.Fatalf("空 kv 的 KVCarrier 应返回 nil, got %v", got)
	}
}

// TestErrAttrs_SingleLayer 单层 Wrap 的 kv 依序展开为 Attr。
func TestErrAttrs_SingleLayer(t *testing.T) {
	err := &kvWrappedErr{msg: "query user", kv: []any{"user_id", 42, "task_key", "tk-1"}}
	attrs := ErrAttrs(err)
	if len(attrs) != 2 {
		t.Fatalf("应展开 2 个 Attr, got %d: %v", len(attrs), attrs)
	}
	if attrs[0].Key != "user_id" || fmt.Sprint(attrs[0].Value.Any()) != "42" {
		t.Fatalf("attrs[0] = %v, want user_id=42", attrs[0])
	}
	if attrs[1].Key != "task_key" || fmt.Sprint(attrs[1].Value.Any()) != "tk-1" {
		t.Fatalf("attrs[1] = %v, want task_key=tk-1", attrs[1])
	}
}

// TestErrAttrs_ChainOuterFirst 多层 Wrap：外层在前、内层在后（与
// pkg/errors.KvFromChain 的收集序一致）；同名 key 不去重、全量保留。
func TestErrAttrs_ChainOuterFirst(t *testing.T) {
	inner := &kvWrappedErr{msg: "inner", kv: []any{"user_id", 42, "deep", true}}
	outer := &kvWrappedErr{msg: "outer", kv: []any{"user_id", 43, "op", "query"}, next: inner}
	attrs := ErrAttrs(outer)
	if len(attrs) != 4 {
		t.Fatalf("多层链应全量展开 4 个 Attr（不去重）, got %d: %v", len(attrs), attrs)
	}
	// 外层在前
	if attrs[0].Key != "user_id" || fmt.Sprint(attrs[0].Value.Any()) != "43" {
		t.Fatalf("attrs[0] 应为外层 user_id=43, got %v", attrs[0])
	}
	if attrs[1].Key != "op" {
		t.Fatalf("attrs[1] 应为 op, got %v", attrs[1])
	}
	// 内层在后，同名 key 保留第二份
	if attrs[2].Key != "user_id" || fmt.Sprint(attrs[2].Value.Any()) != "42" {
		t.Fatalf("attrs[2] 应为内层 user_id=42（不去重）, got %v", attrs[2])
	}
	if attrs[3].Key != "deep" {
		t.Fatalf("attrs[3] 应为 deep, got %v", attrs[3])
	}
}

// TestErrAttrs_MultiBranchDepthFirst 多分支链（Unwrap() []error，errors.Join
// 形态）：深度先序，分支 kv 依序展开。
func TestErrAttrs_MultiBranchDepthFirst(t *testing.T) {
	b1 := &kvWrappedErr{msg: "b1", kv: []any{"branch", 1}}
	b2 := &kvWrappedErr{msg: "b2", kv: []any{"branch", 2, "extra", "x"}}
	joined := &kvJoinErr{errs: []error{b1, b2}}
	outer := &kvWrappedErr{msg: "outer", kv: []any{"op", "fanout"}, next: joined}
	attrs := ErrAttrs(outer)
	// 期望序：outer(op=fanout) → b1(branch=1) → b2(branch=2, extra=x)
	if len(attrs) != 4 {
		t.Fatalf("多分支链应展开 4 个 Attr, got %d: %v", len(attrs), attrs)
	}
	want := []struct {
		key string
		val string
	}{{"op", "fanout"}, {"branch", "1"}, {"branch", "2"}, {"extra", "x"}}
	for i, w := range want {
		if attrs[i].Key != w.key || fmt.Sprint(attrs[i].Value.Any()) != w.val {
			t.Fatalf("attrs[%d] = %v, want %s=%s（深度先序）", i, attrs[i], w.key, w.val)
		}
	}
}

// TestErrAttrs_OddKvBadKey 奇数长度 kv：末尾孤值按 slog 约定落 "!BADKEY"
// （对齐 pkg/errors formatKV 的 "!BADKEY=<value>" 渲染）。
func TestErrAttrs_OddKvBadKey(t *testing.T) {
	err := &kvWrappedErr{msg: "odd", kv: []any{"user_id", 42, "lonely"}}
	attrs := ErrAttrs(err)
	if len(attrs) != 2 {
		t.Fatalf("应展开 2 个 Attr, got %d: %v", len(attrs), attrs)
	}
	if attrs[1].Key != "!BADKEY" || fmt.Sprint(attrs[1].Value.Any()) != "lonely" {
		t.Fatalf("attrs[1] = %v, want !BADKEY=lonely", attrs[1])
	}
}

// TestErrAttrs_CyclicChainTerminates 成环链防御：无上限遍历会永久迭代
// （对齐 pkg/errors maxFlattenDepth 纪律）——超限返回已收集部分并终止。
func TestErrAttrs_CyclicChainTerminates(t *testing.T) {
	c := &kvCyclicErr{kv: []any{"ring", 1}}
	c.self = c // 成环
	attrs := ErrAttrs(c)
	if len(attrs) == 0 {
		t.Fatal("成环链也应返回已收集的部分")
	}
	if len(attrs) > maxErrAttrsDepth {
		t.Fatalf("应受 maxErrAttrsDepth=%d 上限约束, got %d", maxErrAttrsDepth, len(attrs))
	}
}

// toLogxAttrs 把 ErrAttrs 产出的 slog.Attr 转成 logx 自有 Attr（WithAttrs
// 收自有类型；转换即 doc 注释里给出的消费方写法）。
func toLogxAttrs(as []slog.Attr) []Attr {
	out := make([]Attr, 0, len(as))
	for _, a := range as {
		out = append(out, Attr{Key: a.Key, Value: a.Value.Any()})
	}
	return out
}

// TestErrAttrs_ExplicitKvOverridesExplicitWins 端到端钉住设计点 2：链上 kv
// 经 WithAttrs 先落地，调用方显式 kv 后写覆盖同名 key；未冲突的链上 kv
// 保持独立字段（#135 的升格语义）。
func TestErrAttrs_ExplicitKvOverridesExplicitWins(t *testing.T) {
	err := &kvWrappedErr{msg: "query user", kv: []any{"user_id", 42, "channel_id", "ch-1"}}
	h := newCaptureHandler()
	l := NewSlogLogger("svc", WithHandler(h)).WithAttrs(toLogxAttrs(ErrAttrs(err))...)
	l.Error("query failed", ErrKey, err, "user_id", 999) // 显式 user_id 后写覆盖

	recs := h.snapshot()
	if len(recs) != 1 {
		t.Fatalf("want 1 record, got %d", len(recs))
	}
	rec := recs[0]
	if v, ok := rec["user_id"]; !ok || fmt.Sprint(v) != "999" {
		t.Fatalf("显式 kv 应覆盖链上同名 kv: user_id = %v", v)
	}
	if v, ok := rec["channel_id"]; !ok || fmt.Sprint(v) != "ch-1" {
		t.Fatalf("链上 kv 应升格为独立字段: channel_id = %v", v)
	}
	if v, ok := rec[ErrKey]; !ok || fmt.Sprint(v) != "query user" {
		t.Fatalf("err 字段保留文本: err = %v", v)
	}
}

// TestErrAttrs_BuiltinJSONPath 升格后的 Attr 在内置 JSON 路径上落为独立
// JSON 字段（ErrAttrs 的消费主路径）。
func TestErrAttrs_BuiltinJSONPath(t *testing.T) {
	err := &kvWrappedErr{msg: "query user", kv: []any{"user_id", 42}}
	read, _ := captureStdout(t)
	l := NewSlogLogger("svc").WithAttrs(toLogxAttrs(ErrAttrs(err))...)
	l.Error("query failed", ErrKey, err)
	out := read()

	var record map[string]any
	if jerr := json.Unmarshal([]byte(firstJSONLine(t, out)), &record); jerr != nil {
		t.Fatalf("输出不是合法 JSON: %v\n%s", jerr, out)
	}
	if v, ok := record["user_id"]; !ok || fmt.Sprint(v) != "42" {
		t.Fatalf("链上 kv 应落为独立 JSON 字段 user_id=42, got %v\n%s", v, out)
	}
}

// firstJSONLine 取第一行完整 JSON（并发下 captureStdout 可能捕获多行）。
func firstJSONLine(t *testing.T, out string) string {
	t.Helper()
	for i := range out {
		if out[i] == '\n' {
			return out[:i]
		}
	}
	t.Fatalf("无 JSON 行输出: %q", out)
	return ""
}
