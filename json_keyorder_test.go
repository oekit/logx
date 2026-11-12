package logx

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// #191：内置 JSON 记录键序契约钉扎。
//
// #170 把编码从 map + json.Marshal（键按字典序输出）换成有序 jsonField
// 切片 + append 后，wire 键序变为**插入序**——这是一次未在 CHANGELOG
// 点名的 wire 可观察变更，且此后键序既无契约也无护栏。本文件把「插入序」
// 拍板为有意选择（保留 #170 的热路径收益，不回吐排序成本），并整记录
// 钉扎：任何 emit 顺序调整都会在这里炸响，消费侧再无「键序静默漂移」。

// topLevelKeys 按出现序取 JSON 对象的顶层键（json.Decoder Token 流，
// 不经 map 重排）。
func topLevelKeys(t *testing.T, line string) []string {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(line))
	tok, err := dec.Token()
	if err != nil {
		t.Fatalf("open token: %v; line=%.120s", err, line)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		t.Fatalf("记录不是 JSON 对象: %v", tok)
	}
	var keys []string
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			t.Fatalf("key token: %v", err)
		}
		k, ok := tok.(string)
		if !ok {
			t.Fatalf("顶层 token 不是键: %v", tok)
		}
		keys = append(keys, k)
		if _, err := dec.Token(); err != nil { // 跳过值
			t.Fatalf("value token: %v", err)
		}
	}
	return keys
}

// TestBuiltinJSONRecordKeyOrder 钉住整记录键序 = 插入序（#191）：
// 框架字段按固定顺序（time/level/message/service/caller；trace_id 按启用
// 插在 service 与 caller 之间），用户字段按到达序追加——非字典序。
// 修复前本用例无对照物：键序既无契约也无测试，emit 顺序调整零信号。
func TestBuiltinJSONRecordKeyOrder(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = old }()

	sl := NewSlogLogger("svc191").(*slogLogger)
	ts := time.Date(2026, 11, 13, 8, 30, 0, 0, time.UTC)
	// 用户字段刻意按「非字典序」到达：beta 先于 alpha——若键序漂回字典序
	// （#170 前 map 编码的行为），本断言即报。
	sl.emitBuiltinJSON(context.Background(), LevelInfo, "hello", 0, ts,
		"beta", 1, "alpha", 2)

	w.Close()
	sc := bufio.NewScanner(r)
	if !sc.Scan() {
		t.Fatal("stdout 未产出记录行")
	}
	line := sc.Text()

	want := []string{"time", "level", "message", "service", "caller", "beta", "alpha"}
	got := topLevelKeys(t, line)
	if len(got) != len(want) {
		t.Fatalf("键数 = %d (%v), want %d (%v)", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("键序漂移：pos %d = %q, want %q；整序 got=%v want=%v\nline=%s",
				i, got[i], want[i], got, want, line)
		}
	}
	// 有意选择的反向钉扎：插入序 ≠ 字典序——若未来有人「顺手」改回排序
	// 输出，这里给出显式信号（wire 二次变更必须是有意的）。
	if got[0] == "caller" || got[1] == "caller" {
		t.Fatal("键序呈字典序形态（#170 前 map 编码行为）：wire 变更必须有意选择并披露")
	}
}

// TestAppendRecordJSON_PreservesInsertionOrder 钉住编码层（#191）：字段
// 按切片序逐键输出；setField 的 map 语义「后者覆盖前者」保持首现位置，
// 不产生重复键、不重排。
func TestAppendRecordJSON_PreservesInsertionOrder(t *testing.T) {
	fields := []jsonField{
		{"zebra", 1}, {"apple", 2}, {"mango", 3},
	}
	fields = setField(fields, "zebra", 9) // 覆盖已有键：位置必须保持首现序
	fields = append(fields, jsonField{"kiwi", 4})

	got, err := appendRecordJSON(nil, fields)
	if err != nil {
		t.Fatalf("appendRecordJSON: %v", err)
	}
	want := `{"zebra":9,"apple":2,"mango":3,"kiwi":4}`
	if string(got) != want {
		t.Fatalf("记录 = %s, want %s（键序必须保持插入序，覆盖键保持首现位置）", got, want)
	}

	// 对照组：同字段集的字典序形态与插入序必须不同（键序契约不是字典序）。
	keys := topLevelKeys(t, string(got))
	sorted := append([]string(nil), keys...)
	for i := 1; i < len(sorted); i++ {
		for j := 0; j < i; j++ {
			if sorted[j] > sorted[i] {
				sorted[j], sorted[i] = sorted[i], sorted[j]
			}
		}
	}
	equal := true
	for i := range keys {
		if keys[i] != sorted[i] {
			equal = false
			break
		}
	}
	if equal {
		t.Fatalf("插入序与字典序相同（%v），本用例失去检出力：请更换语料字段名", keys)
	}
}
