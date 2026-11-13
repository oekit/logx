package logx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"
)

// #192 回归：WithAttrs 先于 WithGroup 时属性不得被卷进组——slog.Handler
// WithGroup 契约原文「The keys of all **subsequent** attributes, whether
// added by With or in a Record, should be qualified」。此前三条输出路径
//（SlogHandler 反向桥 / 内置 JSON / 注入 Handler）把 attrs 切片与 group
// 链分槽累计、输出时刻统一回填**最终**前缀，丢失「哪些属性是在 WithGroup
// 之前挂上的」顺序信息——同一序列在 stdlib 与 logx 的字段位置静默分叉，
// 按 .request_id 检索的链路换库即断。三条路径各钉一条，并与 stdlib 对拍。

// flattenJSONKeys 把嵌套 JSON 对象展平为 "a.b.k" 点分键（对拍归一化用）。
func flattenJSONKeys(rec map[string]any, prefix string, out map[string]string) {
	for k, v := range rec {
		full := k
		if prefix != "" {
			full = prefix + "." + k
		}
		if sub, ok := v.(map[string]any); ok {
			flattenJSONKeys(sub, full, out)
			continue
		}
		out[full] = fmt.Sprintf("%v", v)
	}
}

// TestSlogHandler_AttrBeforeGroupStaysOutside 钉住桥接路径（#192）：
// With("before",1).WithGroup("g").With("inside",2).Info("m","rec",3)——
// before 必须在组外，g 只含组后属性（With 后挂的 inside 与 Record 的 rec）。
func TestSlogHandler_AttrBeforeGroupStaysOutside(t *testing.T) {
	base := &fakeLogger{}
	Slogger(base).With("before", 1).WithGroup("g").With("inside", 2).Info("m", "rec", 3)

	if len(base.entries) != 1 {
		t.Fatalf("records = %d, want 1: %s", len(base.entries), base.text())
	}
	got := map[string]string{}
	for i := 0; i+1 < len(base.entries[0].kvs); i += 2 {
		k, ok := base.entries[0].kvs[i].(string)
		if !ok {
			t.Fatalf("kv 键非 string: %#v", base.entries[0].kvs[i])
		}
		got[k] = fmt.Sprintf("%v", base.entries[0].kvs[i+1])
	}
	if got["before"] != "1" {
		t.Errorf("before = %v, want 组外顶层 1: %v", got["before"], got)
	}
	if _, ok := got["g.before"]; ok {
		t.Errorf("WithGroup 之前的属性被卷进组（g.before 存在）: %v", got)
	}
	if got["g.inside"] != "2" {
		t.Errorf("g.inside = %v, want 2: %v", got["g.inside"], got)
	}
	if got["g.rec"] != "3" {
		t.Errorf("g.rec = %v, want 3（Record attr 施加当前组）: %v", got["g.rec"], got)
	}
}

// TestSlogHandler_DeepGroupOrder 钉住桥接路径的深层链（#192）：组链单调
// 嵌套，各属性落在**挂载时刻**的组层级。
func TestSlogHandler_DeepGroupOrder(t *testing.T) {
	base := &fakeLogger{}
	Slogger(base).
		WithGroup("a").
		With("x", 1).
		WithGroup("b").
		With("y", 2).
		Info("m", "z", 3)

	if len(base.entries) != 1 {
		t.Fatalf("records = %d, want 1: %s", len(base.entries), base.text())
	}
	got := map[string]string{}
	for i := 0; i+1 < len(base.entries[0].kvs); i += 2 {
		got[base.entries[0].kvs[i].(string)] = fmt.Sprintf("%v", base.entries[0].kvs[i+1])
	}
	if got["a.x"] != "1" {
		t.Errorf("a.x = %v, want 1: %v", got["a.x"], got)
	}
	if got["a.b.y"] != "2" {
		t.Errorf("a.b.y = %v, want 2: %v", got["a.b.y"], got)
	}
	if got["a.b.z"] != "3" {
		t.Errorf("a.b.z = %v, want 3: %v", got["a.b.z"], got)
	}
	if _, ok := got["x"]; ok {
		t.Errorf("WithGroup(a) 之后挂的 x 不应留顶层: %v", got)
	}
}

// TestSlogAttrOrder_MatchesStdlib 对拍（#192）：同一调用序列在 stdlib
// JSONHandler 与 logx 桥接路径的用户字段键位逐键一致。
func TestSlogAttrOrder_MatchesStdlib(t *testing.T) {
	// stdlib 基准
	var stdBuf bytes.Buffer
	std := slog.New(slog.NewJSONHandler(&stdBuf, nil))
	std.With("before", 1).WithGroup("g").With("inside", 2).Info("m", "rec", 3)

	var stdRec map[string]any
	if err := json.Unmarshal(stdBuf.Bytes(), &stdRec); err != nil {
		t.Fatalf("stdlib 输出不是合法 JSON: %v\n%s", err, stdBuf.String())
	}
	stdKeys := map[string]string{}
	flattenJSONKeys(stdRec, "", stdKeys)

	// logx 桥接
	base := &fakeLogger{}
	Slogger(base).With("before", 1).WithGroup("g").With("inside", 2).Info("m", "rec", 3)
	if len(base.entries) != 1 {
		t.Fatalf("records = %d, want 1: %s", len(base.entries), base.text())
	}
	bridgeKeys := map[string]string{}
	for i := 0; i+1 < len(base.entries[0].kvs); i += 2 {
		bridgeKeys[base.entries[0].kvs[i].(string)] = fmt.Sprintf("%v", base.entries[0].kvs[i+1])
	}

	// 用户字段键位逐键一致（stdlib 的 time/level/msg 是框架面，键名不同，
	// 只比 logx 会产出的用户键）
	for _, k := range []string{"before", "g.inside", "g.rec"} {
		if stdKeys[k] != bridgeKeys[k] {
			t.Errorf("键 %s: stdlib=%q logx=%q（字段位置与 stdlib 分叉）", k, stdKeys[k], bridgeKeys[k])
		}
	}
	if _, ok := stdKeys["g.before"]; ok {
		t.Errorf("stdlib 基准自证失败：before 在 stdlib 输出中也进了组？%v", stdKeys)
	}
}

// TestSlogGroup_AttrBeforeGroup_BuiltinJSON 钉住内置 JSON 路径（#192）：
// WithGroup 之前的 WithAttrs 属性留顶层平铺（不加组前缀），组后属性照旧
// 施加当前组前缀（#75/#83 语义不变）。
func TestSlogGroup_AttrBeforeGroup_BuiltinJSON(t *testing.T) {
	read, _ := captureStdout(t)
	NewSlogLogger("svc").
		WithAttrs(Attr{Key: "before", Value: 1}).
		WithGroup("g").
		WithAttrs(Attr{Key: "inside", Value: 2}).
		Info("m", "rec", 3)

	rec := readJSONRecord(t, read())
	if rec["before"] != float64(1) {
		t.Errorf("before = %v, want 组外顶层 1: %v", rec["before"], rec)
	}
	if _, ok := rec["g.before"]; ok {
		t.Errorf("WithGroup 之前的属性被卷进组（g.before 存在）: %v", rec)
	}
	if rec["g.inside"] != float64(2) {
		t.Errorf("g.inside = %v, want 2: %v", rec["g.inside"], rec)
	}
	if rec["g.rec"] != float64(3) {
		t.Errorf("g.rec = %v, want 3: %v", rec["g.rec"], rec)
	}
	// 组后无任何属性时不落组键（空组不落，stdlib 约定）
	read2, _ := captureStdout(t)
	NewSlogLogger("svc").WithAttrs(Attr{Key: "solo", Value: 1}).WithGroup("g").Info("m")
	rec2 := readJSONRecord(t, read2())
	if _, ok := rec2["g"]; ok {
		t.Errorf("空组不应落键: %v", rec2)
	}
	if rec2["solo"] != float64(1) {
		t.Errorf("solo = %v, want 1: %v", rec2["solo"], rec2)
	}
}

// TestSlogGroup_AttrBeforeGroup_HandlerPath 钉住注入 Handler 路径（#192+#99）：
// WithGroup 之前的属性以顶层 Attr 下发（不进嵌套组），组后属性照旧经
// slog.Group 嵌套——注入 JSONHandler 后与 stdlib 直写输出一致。
func TestSlogGroup_AttrBeforeGroup_HandlerPath(t *testing.T) {
	var buf bytes.Buffer
	NewSlogLogger("svc", WithHandler(slog.NewJSONHandler(&buf, nil))).
		WithAttrs(Attr{Key: "before", Value: "b1"}).
		WithGroup("g").
		WithAttrs(Attr{Key: "inside", Value: "i1"}).
		Info("m", "rec", "r1")

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("输出不是合法 JSON: %v\n%s", err, buf.String())
	}
	if rec["before"] != "b1" {
		t.Errorf("before = %v, want 组外顶层 b1: %v", rec["before"], rec)
	}
	g, ok := rec["g"].(map[string]any)
	if !ok {
		t.Fatalf("g 组缺失: %v", rec)
	}
	if _, ok := g["before"]; ok {
		t.Errorf("WithGroup 之前的属性被卷进组: %v", rec)
	}
	if g["inside"] != "i1" || g["rec"] != "r1" {
		t.Errorf("g 组内容 = %v, want inside=i1 rec=r1", g)
	}
}
