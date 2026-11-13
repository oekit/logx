package logx

import (
	"encoding/json"
	"log"
	"strings"
	"testing"
	"time"
)

// #195 回归：时间戳经 AppendFormat 直写（消除 ts.Format 的中间串分配），
// 与 ts.Format(time.RFC3339Nano) 字节同构——语料钉扎，调整布局必须有意。

// TestAppendJSONTimestamp_MatchesFormat 对拍 AppendFormat 直写与 ts.Format
// 的字节等价（语料覆盖：零值/UTC/正负偏移/满纳秒/秒级整点）。
func TestAppendJSONTimestamp_MatchesFormat(t *testing.T) {
	corpus := []time.Time{
		{}, // 零值
		time.Date(2026, 11, 13, 8, 30, 0, 0, time.UTC),
		time.Date(2026, 11, 13, 8, 30, 0, 123456789, time.FixedZone("CST", 8*3600)),
		time.Date(2026, 1, 2, 3, 4, 5, 999999999, time.FixedZone("EST", -5*3600)),
		time.Unix(0, 0).UTC(),
		time.Now(), // 本地时区 + 当下满纳秒
	}
	for i, ts := range corpus {
		// appendJSONTimestamp 落盘形态 = 带引号 JSON 字符串（time 字段经
		// appendJSONValue 的最终形态）；字节同构按「引号 + ts.Format」全等钉扎
		want := `"` + ts.Format(time.RFC3339Nano) + `"`
		got := string(appendJSONTimestamp(nil, ts))
		if got != want {
			t.Errorf("语料 %d: AppendFormat=%q Format=%q（字节不同构）", i, got, want)
		}
		var s string
		if err := json.Unmarshal([]byte(got), &s); err != nil {
			t.Errorf("语料 %d: 输出不是合法 JSON 字符串: %v (%q)", i, err, got)
		}
	}
}

// TestEmitBuiltinJSON_TimeFieldRFC3339 钉住内置 JSON 行的 time 字段为
// RFC3339Nano 可互解析（JSONL 下游时序依赖）。
func TestEmitBuiltinJSON_TimeFieldRFC3339(t *testing.T) {
	before := time.Now()
	read, _ := captureStdout(t)
	NewSlogLogger("svc").Info("m", "k", 1)
	after := time.Now()

	rec := readJSONRecord(t, read())
	tv, ok := rec["time"].(string)
	if !ok {
		t.Fatalf("time 字段缺失或非字符串: %v", rec)
	}
	ts, err := time.Parse(time.RFC3339Nano, tv)
	if err != nil {
		t.Fatalf("time 字段不是 RFC3339Nano: %v (%s)", err, tv)
	}
	if ts.Before(before.Add(-time.Second)) || ts.After(after.Add(time.Second)) {
		t.Errorf("time 字段偏离发射窗口: %s (窗口 %s..%s)", tv, before, after)
	}
}

// TestStdLogger_AttrMaskerDefault 护栏：std 后端在无下沉键集时输出与历史
// 一致（#194 的 mask 通路不影响默认路径），且实现 AttrKeyMasker。
func TestStdLogger_AttrMaskerDefault(t *testing.T) {
	var buf strings.Builder
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })

	l := NewStdLogger("svc").WithAttrs(Attr{Key: "rid", Value: "r-1"})
	l.Info("m", "k", "v")

	out := buf.String()
	if !strings.Contains(out, "rid=r-1") || !strings.Contains(out, "k=v") {
		t.Errorf("std 默认路径输出被破坏: %q", out)
	}
	if _, ok := any(l).(AttrKeyMasker); !ok {
		t.Fatal("stdLogger 应实现 AttrKeyMasker")
	}
}
