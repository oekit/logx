package logx

import (
	"strings"
	"testing"
)

// TestFilterKey_CaseInsensitive 钉住 #85：FilterKey 命中判定大小写不敏感——
// HTTP 头的规范键名是 Authorization/X-Api-Key（大写），此前配置
// FilterKey("authorization") 时 "Authorization" 键的值原样落盘。
func TestFilterKey_CaseInsensitive(t *testing.T) {
	base := &fakeLogger{}
	l := WithFilter(base, FilterKey("authorization"))

	l.Info("http", "Authorization", "Bearer secret-token", "plain", "ok")

	out := base.text()
	if strings.Contains(out, "Bearer secret-token") {
		t.Fatalf("大小写变体绕过脱敏: %s", out)
	}
	if !strings.Contains(out, "Authorization=***") {
		t.Fatalf("脱敏标记缺失（键名保持原样，仅值替换）: %s", out)
	}
	if !strings.Contains(out, "plain=ok") {
		t.Fatalf("非敏感 kv 被误脱敏: %s", out)
	}
}

// TestFilterKey_AttrsCaseInsensitive 钉住 #85 的 WithAttrs 面：
// 持久属性键的大小写变体同样脱敏。
func TestFilterKey_AttrsCaseInsensitive(t *testing.T) {
	base := &attrsRecordingLogger{fakeLogger: &fakeLogger{}}
	l := WithFilter(base, FilterKey("password"))

	l.WithAttrs(Attr{Key: "PASSWORD", Value: "hunter2"}).Info("login")

	for _, a := range base.attrs {
		if strings.EqualFold(a.Key, "password") && a.Value == "hunter2" {
			t.Fatalf("WithAttrs 大小写变体绕过脱敏: %+v", base.attrs)
		}
	}
}

// TestFilterKey_NonStringKeyNormalized 钉住 #85：非 string 键（int 等）先按
// %v 归一成键名再比对——emit 侧本来就把键 fmt.Sprintf("%v") 稳定成键，
// 此前这类键完全不可脱敏。
func TestFilterKey_NonStringKeyNormalized(t *testing.T) {
	base := &fakeLogger{}
	l := WithFilter(base, FilterKey("42"))

	l.Info("code", 42, "secret-value")

	out := base.text()
	if strings.Contains(out, "secret-value") {
		t.Fatalf("非 string 键绕过脱敏: %s", out)
	}
}

// TestFilterKey_ExactStillWorks 护栏：精确匹配语义（含点路径字面量）不变。
func TestFilterKey_ExactStillWorks(t *testing.T) {
	base := &fakeLogger{}
	l := WithFilter(base, FilterKey("user.token"))

	l.Info("api", "user.token", "t-xyz", "user.tokenx", "safe")

	out := base.text()
	if strings.Contains(out, "t-xyz") {
		t.Fatalf("点路径键未脱敏: %s", out)
	}
	if !strings.Contains(out, "user.tokenx=safe") {
		t.Fatalf("非命中键被误脱敏: %s", out)
	}
}
