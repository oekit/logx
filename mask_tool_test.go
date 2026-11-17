package logx

import (
	"testing"
)

// logx#230：脱敏工具面下沉根包导出（MergeMaskKeys/MatchMaskKey/MaskKvs/
// MaskAttrs）——hlog 子包此前逐字复制 4 份 helper（注释自认「语义变更需
// 两处同步」）。本文件钉住导出面的独立语义（含 logx#229 的前缀双匹配），
// 消费方（hlog）不再自持实现。

func TestMatchMaskKey(t *testing.T) {
	keys := map[string]struct{}{"authorization": {}, "User.Password": {}}
	if !MatchMaskKey(keys, "Authorization") {
		t.Fatal("大小写不敏感匹配失效（#85）")
	}
	if !MatchMaskKey(keys, "user.password") {
		t.Fatal("点路径字面量匹配失效")
	}
	if MatchMaskKey(keys, "token") {
		t.Fatal("未命中键误报")
	}
	if MatchMaskKey(nil, "authorization") {
		t.Fatal("空键集误报")
	}
}

func TestMatchMaskKeyScoped(t *testing.T) {
	keys := map[string]struct{}{"user.password": {}}
	if !matchMaskKeyScoped(keys, "user.", "password") {
		t.Fatal("前缀键兜底未命中（logx#229）")
	}
	if !matchMaskKeyScoped(keys, "USER.", "PASSWORD") {
		t.Fatal("前缀键兜底应大小写不敏感（与 #85 同口径）")
	}
	if !matchMaskKeyScoped(keys, "", "user.password") {
		t.Fatal("裸键半边失效")
	}
	if matchMaskKeyScoped(keys, "", "password") {
		t.Fatal("空前缀不应凭空给裸键匹配上点路径配置（#229 对偶护栏：组外属性不误伤）")
	}
	if matchMaskKeyScoped(keys, "db.", "password") {
		t.Fatal("异组前缀误命中")
	}
	// 组链前缀一律经 groupsPrefix 取：空组链得 ""，不得把 joinGroups 对空
	// 输入的 "." 返回值当 prefix（否则 "….password" 形态参与匹配）。
	if matchMaskKeyScoped(keys, groupsPrefix(nil), "password") {
		t.Fatal("空组链经 groupsPrefix 必须退化为裸键匹配")
	}
	if s := groupsPrefix([]string{"a", "b"}); s != "a.b." {
		t.Fatalf("groupsPrefix 拼接口径漂移: %q", s)
	}
}

func TestMergeMaskKeys(t *testing.T) {
	base := map[string]struct{}{"k1": {}}
	add := map[string]struct{}{"k2": {}}
	merged := MergeMaskKeys(base, add)
	if len(merged) != 2 {
		t.Fatalf("并集缺失: %v", merged)
	}
	// 无既有键集时共享入参 map（只读契约，#194）
	if got := MergeMaskKeys(nil, add); len(got) != 1 {
		t.Fatalf("空基集并入失效: %v", got)
	}
	// 空增量返回基集
	if got := MergeMaskKeys(base, nil); len(got) != 1 {
		t.Fatalf("空增量误改基集: %v", got)
	}
	// 入参不得被改写
	if len(base) != 1 || len(add) != 1 {
		t.Fatalf("MergeMaskKeys 改写了入参: %v / %v", base, add)
	}
}

func TestMaskKvs(t *testing.T) {
	keys := map[string]struct{}{"user.password": {}}
	// 前缀键命中（logx#229）
	out := MaskKvs(keys, "user.", []any{"password", "hunter2", "name", "alice"})
	if out[1] != "***" || out[3] != "alice" {
		t.Fatalf("MaskKvs 前缀键双匹配失效: %v", out)
	}
	// 裸键命中
	out = MaskKvs(keys, "", []any{"user.password", "hunter2"})
	if out[1] != "***" {
		t.Fatalf("MaskKvs 裸键匹配失效: %v", out)
	}
	// 未命中零拷贝原样返回
	in := []any{"name", "alice"}
	if got := MaskKvs(keys, "user.", in); len(got) != 2 || got[1] != "alice" {
		t.Fatalf("未命中误改 kv: %v", got)
	}
	// 非 string 键按 %v 归一（#85/#174）
	out = MaskKvs(map[string]struct{}{"42": {}}, "", []any{42, "s3cret"})
	if out[1] != "***" {
		t.Fatalf("非 string 键归一失效: %v", out)
	}
	// 空键集 / 空 kv 早退
	if got := MaskKvs(nil, "user.", []any{"password", "x"}); len(got) != 2 {
		t.Fatal("空键集早退失效")
	}
	if got := MaskKvs(keys, "user.", nil); got != nil {
		t.Fatal("空 kv 早退失效")
	}
}

func TestMaskAttrs(t *testing.T) {
	keys := map[string]struct{}{"user.password": {}, "token": {}}
	attrs := []Attr{
		{Key: "password", Value: "hunter2"}, // 挂在 user 组下（快照 "user."）
		{Key: "password", Value: "plain1"},  // 挂在顶层（快照 ""）——不误伤
		{Key: "token", Value: "tk-1"},       // 裸键命中
		{Key: "region", Value: "cn-north-1"},
	}
	prefixes := []string{"user.", "", "", ""}
	out := MaskAttrs(keys, attrs, prefixes, "")
	want := []string{"***", "plain1", "***", "cn-north-1"}
	for i, w := range want {
		if out[i].Value != w {
			t.Fatalf("attrs[%d] = %q, want %q", i, out[i].Value, w)
		}
	}
	// 快照缺失退回当前组前缀（防御路径）
	out = MaskAttrs(keys, attrs[:1], nil, "user.")
	if out[0].Value != "***" {
		t.Fatalf("快照缺失回退失效: %+v", out[0])
	}
	// 未命中零拷贝原样返回
	in := []Attr{{Key: "region", Value: "cn"}}
	if got := MaskAttrs(keys, in, nil, ""); len(got) != 1 || got[0].Value != "cn" {
		t.Fatalf("未命中误改 attrs: %v", got)
	}
}

func TestTraceIDFieldKey(t *testing.T) {
	// logx#231：字段名单源，与内置 JSON / 注入 Handler / TraceHandler 共用。
	if TraceIDFieldKey != "trace_id" {
		t.Fatalf("TraceIDFieldKey 漂移: %q", TraceIDFieldKey)
	}
	if !reservedFields[TraceIDFieldKey] {
		t.Fatal("reservedFields 未含 TraceIDFieldKey")
	}
}
