package logx

import (
	"regexp"
	"strings"
	"testing"
)

// lstdFlagsTS 匹配包级 log 默认 LstdFlags 的时间戳前缀（YYYY/MM/DD HH:MM:SS，
// 斜杠分隔、无毫秒）——std 后端自带毫秒级 ts 前缀后，行首再出现该形态
// 即双时间戳（#78）。
var lstdFlagsTS = regexp.MustCompile(`\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}`)

// ownTS 匹配 std 后端自带的毫秒级 ts（YYYY-MM-DD HH:MM:SS.mmm，连字符分隔）。
var ownTS = regexp.MustCompile(`\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\.\d{3}`)

// TestStdLogger_PrintfKeepsAttrs 钉住 #77：logf（printf 路径）必须携带
// WithAttrs 挂的持久属性——此前同一 logger 上 Info 带 request_id、Infof
// 不带，字段面按方法族分裂。
func TestStdLogger_PrintfKeepsAttrs(t *testing.T) {
	buf := captureStderr(t)
	l := NewStdLogger("svc", LevelDebug).WithAttrs(
		Attr{Key: "request_id", Value: "r-42"},
		Attr{Key: "persist", Value: "P"},
	)

	l.Infof("printf-style %d", 1)
	if out := buf.String(); !strings.Contains(out, "request_id=r-42") || !strings.Contains(out, "persist=P") {
		t.Fatalf("printf 路径丢失 WithAttrs 持久属性: %s", out)
	}
	if !strings.Contains(buf.String(), "printf-style 1") {
		t.Fatalf("printf 消息缺失: %s", buf.String())
	}

	// 对照：结构化路径原本就带（既有行为不变）
	buf.Reset()
	l.Info("kv-style", "k", 1)
	if out := buf.String(); !strings.Contains(out, "request_id=r-42") || !strings.Contains(out, "k=1") {
		t.Fatalf("结构化路径属性缺失: %s", out)
	}
}

// TestStdLogger_CtxPrintfKeepsAttrs 钉住 #77 的 Ctx*f 面：CtxInfof 同样带属性。
func TestStdLogger_CtxPrintfKeepsAttrs(t *testing.T) {
	buf := captureStderr(t)
	l := NewStdLogger("svc", LevelDebug).WithAttrs(Attr{Key: "request_id", Value: "r-42"})
	l.CtxWarnf(t.Context(), "ctx-printf %s", "msg")

	if out := buf.String(); !strings.Contains(out, "request_id=r-42") || !strings.Contains(out, "ctx-printf msg") {
		t.Fatalf("Ctx printf 路径丢失 WithAttrs 持久属性: %s", out)
	}
}

// TestStdLogger_SingleTimestamp 钉住 #78：std 后端自带毫秒级 ts 后，
// 行首不得再叠包级 log.Printf 的 LstdFlags 秒级时间戳（双时间戳）。
func TestStdLogger_SingleTimestamp(t *testing.T) {
	buf := captureStderr(t)
	NewStdLogger("svc", LevelDebug).Infof("ts-check")

	out := buf.String()
	if lstdFlagsTS.MatchString(out) {
		t.Fatalf("双时间戳（LstdFlags 前缀仍在）: %s", out)
	}
	if !ownTS.MatchString(out) {
		t.Fatalf("自带毫秒级 ts 缺失: %s", out)
	}
	// 自带 ts 恰好一次
	if n := len(ownTS.FindAllString(out, -1)); n != 1 {
		t.Fatalf("自带 ts 出现 %d 次, want 1: %s", n, out)
	}
}

// TestStdLogger_NoAttrsPrintfNoTrailingSpace 护栏：无持久属性时 printf
// 输出不产生尾部空白。
func TestStdLogger_NoAttrsPrintfNoTrailingSpace(t *testing.T) {
	buf := captureStderr(t)
	NewStdLogger("svc", LevelDebug).Infof("clean")

	out := buf.String()
	if out == "" {
		t.Fatal("无输出")
	}
	line := strings.TrimRight(strings.SplitN(out, "\n", 2)[0], "\n")
	if strings.HasSuffix(strings.TrimRight(line, "\r"), " ") {
		t.Fatalf("无属性时 printf 输出有尾部空格: %q", line)
	}
}

// TestGlobalShortcuts_RouteToDefault 全局快捷方法（logger.go 委托段）经
// std 后端落到输出器——#78 后 std 后端经私有 log.Logger（stdWriter）输出，
// 捕获需访问包内符号，故置于内部测试包。
func TestGlobalShortcuts_RouteToDefault(t *testing.T) {
	buf := captureStderr(t)
	prev := Default()
	t.Cleanup(func() { SetDefault(prev) })
	SetDefault(NewStdLogger("shortcut-svc", LevelDebug))

	WithService("svc-x").Error("shortcut")
	WithCtx(t.Context()).Warn("withctx")
	Debug("g-debug")
	Infof("g-info-fmt")
	out := buf.String()
	for _, want := range []string{"shortcut", "withctx", "g-debug", "g-info-fmt"} {
		if !strings.Contains(out, want) {
			t.Errorf("输出缺 %q: %q", want, out)
		}
	}
}
