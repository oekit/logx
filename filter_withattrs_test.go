package logx

import (
	"strings"
	"testing"
)

// attrsRecordingLogger 复用包内 fakeLogger，只**额外**记录 WithAttrs 挂上去
// 的属性——被测点正是「过滤必须在派生时就把敏感值遮掉」。
type attrsRecordingLogger struct {
	*fakeLogger
	attrs []Attr
}

// WithAttrs 把属性**记下来**（fakeLogger 的实现会忽略它们）。
func (l *attrsRecordingLogger) WithAttrs(attrs ...Attr) Logger {
	l.attrs = append(l.attrs, attrs...)
	return l
}

// TestFilter_WithAttrs_Masked 回归测试：WithAttrs 挂上去的属性**也必须**被
// 脱敏。
//
// 缺陷成因：filterLogger.WithAttrs 把属性**原样**交给 base logger，而过滤只
// 作用于「每次调用传进来的 kv」⇒ WithAttrs 挂的敏感属性完全绕过过滤。
// WithFilter 的 godoc 明确承诺「WithAttrs/WithGroup/WithCtx 派生的子 logger
// 保持相同过滤规则」⇒ 旧实现是谎报：敏感信息照常落盘。
func TestFilter_WithAttrs_Masked(t *testing.T) {
	base := &attrsRecordingLogger{fakeLogger: &fakeLogger{}}
	l := WithFilter(base, FilterKey("password"))

	l.WithAttrs(Attr{Key: "password", Value: "hunter2"}).Info("login")

	for _, a := range base.attrs {
		if a.Key == "password" {
			if a.Value == "hunter2" {
				t.Fatalf("WithAttrs 挂的敏感属性未脱敏：%v=%v", a.Key, a.Value)
			}
			if a.Value != "***" {
				t.Fatalf("敏感属性值应为 ***，实得 %v", a.Value)
			}
			return
		}
	}
	t.Fatal("WithAttrs 的属性没有被记录下来，用例失去意义")
}

// TestFilter_WithAttrs_NonSensitiveKept 回归护栏：非敏感属性原样保留。
func TestFilter_WithAttrs_NonSensitiveKept(t *testing.T) {
	base := &attrsRecordingLogger{fakeLogger: &fakeLogger{}}
	l := WithFilter(base, FilterKey("password"))

	l.WithAttrs(Attr{Key: "user", Value: "alice"}).Info("login")

	found := false
	for _, a := range base.attrs {
		if a.Key == "user" {
			found = true
			if a.Value != "alice" {
				t.Fatalf("非敏感属性被误脱敏：%v=%v", a.Key, a.Value)
			}
		}
	}
	if !found {
		t.Fatal("WithAttrs 的属性没有被记录下来，用例失去意义")
	}
}

// TestFilter_CallKvsAttrs_BothMasked 回归护栏：每次调用传的 kv 仍然脱敏
// （既有口径），且与 WithAttrs 路径同时生效。
func TestFilter_CallKvsAttrs_BothMasked(t *testing.T) {
	base := &attrsRecordingLogger{fakeLogger: &fakeLogger{}}
	l := WithFilter(base, FilterKey("password"))

	l.WithAttrs(Attr{Key: "password", Value: "hunter2"}).
		Info("login", "password", "s3cret", "user", "alice")

	out := base.text()
	if strings.Contains(out, "hunter2") || strings.Contains(out, "s3cret") {
		t.Fatalf("敏感值未脱敏：%q", out)
	}
	if !strings.Contains(out, "alice") {
		t.Fatalf("非敏感值被误脱敏：%q", out)
	}
}
