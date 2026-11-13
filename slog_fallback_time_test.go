package logx

import (
	"math"
	"strings"
	"testing"
	"time"
)

// #193 回归：序列化降级记录与正常行的 schema 归一——#100 修保真时漏了
// 「已发布框架字段」里的 time（marshalFallback 入参根本没给时间戳），且
// rawKv 直接 FormatKeysAndValues 不带组前缀，同一条带组 logger 的正常行
// 与降级行同字段两种键名。

// TestSlogFallback_TimeFieldPresent 钉住降级行补发布 time 框架字段（#193）：
// JSONL 下游按 time 取事件时间，降级行缺失即退化为采集时间/整行被拒收。
func TestSlogFallback_TimeFieldPresent(t *testing.T) {
	read, _ := captureStdout(t)
	NewSlogLogger("svc").Info("m", "bad", make(chan int))

	rec := readJSONRecord(t, read())
	tv, ok := rec["time"].(string)
	if !ok || tv == "" {
		t.Fatalf("降级记录缺 time 字段: %v", rec)
	}
	if _, err := time.Parse(time.RFC3339Nano, tv); err != nil {
		t.Fatalf("降级记录 time 不是 RFC3339Nano: %v (%s)", err, tv)
	}
}

// TestSlogFallback_GroupedRawKvPrefix 钉住降级行的组前缀（#193）：rawKv 的
// kv 键施加当前组前缀（与正常路径 put 同一口径），持久属性键按挂载时刻
// 的组前缀固化（#192）。
func TestSlogFallback_GroupedRawKvPrefix(t *testing.T) {
	read, _ := captureStdout(t)
	l := NewSlogLogger("svc").
		WithGroup("db").
		WithAttrs(Attr{Key: "dsn", Value: "postgres://u:p@h/d"})
	l.Info("m", "bad", math.NaN(), "okv", "v")

	rec := readJSONRecord(t, read())
	rawKv, _ := rec["rawKv"].(string)
	if rawKv == "" {
		t.Fatalf("降级记录缺 rawKv: %v", rec)
	}
	if !strings.Contains(rawKv, "db.bad=") {
		t.Errorf("rawKv 的 kv 键未施加组前缀（缺 db.bad=）: %q", rawKv)
	}
	if !strings.Contains(rawKv, "db.okv=v") {
		t.Errorf("rawKv 的正常 kv 键未施加组前缀（缺 db.okv=v）: %q", rawKv)
	}
	// 持久属性带挂载时刻组前缀（与正常行 db.dsn 同键名）
	if rec["db.dsn"] != "postgres://u:p@h/d" {
		t.Errorf("降级行持久属性键未施加组前缀: %v", rec)
	}
	// 常规保真面不回退（#100）
	if rec["service"] != "svc" {
		t.Errorf("降级记录丢 service: %v", rec)
	}
	if rec["marshalErr"] == "" {
		t.Errorf("降级记录缺 marshalErr: %v", rec)
	}
}

// TestSlogFallback_GroupedTimeConsistency 钉住降级行与正常行 time 同一
// 渲染口径（RFC3339Nano，同一时间布局常量）。
func TestSlogFallback_GroupedTimeConsistency(t *testing.T) {
	read, _ := captureStdout(t)
	NewSlogLogger("svc").WithGroup("g").Info("m", "bad", make(chan int))

	rec := readJSONRecord(t, read())
	tv, _ := rec["time"].(string)
	ts, err := time.Parse(time.RFC3339Nano, tv)
	if err != nil {
		t.Fatalf("降级行 time 解析失败: %v (%s)", err, tv)
	}
	if math.Abs(time.Since(ts).Seconds()) > 60 {
		t.Errorf("降级行 time 偏离当前时刻过大: %s", tv)
	}
}
