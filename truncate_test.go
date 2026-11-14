package logx

import (
	"strings"
	"testing"
)

// logx#223：TruncateForLog 单源原语 + SetMaxValueLen emit 侧截断。
func TestTruncateForLogAndMaxValueLen(t *testing.T) {
	// 单源原语：未超限原样、超限截断带尾巴、rune 边界不劈多字节
	if got := TruncateForLog("short", 10); got != "short" {
		t.Fatalf("未超限应原样: %q", got)
	}
	big := strings.Repeat("a", 100)
	got := TruncateForLog(big, 10)
	if !strings.HasPrefix(got, strings.Repeat("a", 10)) || !strings.Contains(got, "truncated") {
		t.Fatalf("超限应截到 10 字节并带尾巴: %q", got)
	}
	// 多字节字符不被劈开（"中" 3 字节,截到 10 字节边界回退）
	multi := strings.Repeat("中", 10) // 30 字节
	got2 := TruncateForLog(multi, 10)
	if !strings.HasSuffix(got2, "…[logx truncated]") {
		t.Fatalf("尾巴缺失: %q", got2)
	}
	if strings.Contains(got2[:len(got2)-len("…[logx truncated]")], "�") {
		t.Fatalf("rune 边界被劈: %q", got2)
	}

	// emit 侧：默认关闭（零行为变化），开启后 string 值截断
	if MaxValueLen() != 0 {
		t.Fatalf("默认应关闭, got %d", MaxValueLen())
	}
	SetMaxValueLen(8)
	defer SetMaxValueLen(0)
	if MaxValueLen() != 8 {
		t.Fatalf("SetMaxValueLen 未生效: %d", MaxValueLen())
	}
	v, changed := truncateValue(strings.Repeat("x", 100))
	if !changed || !strings.Contains(v.(string), "truncated") {
		t.Fatalf("truncateValue 应截断: %v", v)
	}
	v2, changed2 := truncateValue("small")
	if changed2 || v2 != "small" {
		t.Fatalf("未超限不应截断: %v", v2)
	}
	// []byte 族
	vb, changedB := truncateValue(make([]byte, 100))
	if !changedB {
		t.Fatal("[]byte 超限应截断")
	}
	if _, ok := vb.([]byte); !ok {
		t.Fatalf("[]byte 截断后应保持 []byte 类型, got %T", vb)
	}
}
