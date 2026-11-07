// #86 回归：printf 路径（Debugf/Infof/Warnf/Errorf 与 Ctx*f）必须携带
// WithAttrs 挂的持久属性——与 Ctx* 结构化路径（formatKV）字段面统一。
package hlog

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/oekit/logx"
)

func TestHlogLogger_PrintfKeepsAttrs(t *testing.T) {
	var buf syncBuffer
	hlog.SetOutput(&buf)
	t.Cleanup(func() { hlog.SetLogger(hlog.DefaultLogger()) })

	l := NewHlogLogger("svc").WithAttrs(
		logx.Attr{Key: "request_id", Value: "r-42"},
		logx.Attr{Key: "persist", Value: "P"},
	)

	// printf 路径（经 Ctx*f 落地）
	l.Infof("printf-style %d", 1)
	out := buf.String()
	for _, want := range []string{"printf-style 1", "request_id=r-42", "persist=P"} {
		if !strings.Contains(out, want) {
			t.Errorf("printf 路径输出缺 %q: %s", want, out)
		}
	}

	// 带 ctx 的 printf 路径
	buf.buf.Reset()
	l.CtxInfof(context.Background(), "ctx-printf %s", "msg")
	out = buf.String()
	for _, want := range []string{"ctx-printf msg", "request_id=r-42"} {
		if !strings.Contains(out, want) {
			t.Errorf("Ctx printf 路径输出缺 %q: %s", want, out)
		}
	}

	// 对照：结构化路径原本就带（既有行为不变）
	buf.buf.Reset()
	l.Info("kv-style", "k", 1)
	out = buf.String()
	if !strings.Contains(out, "request_id=r-42") || !strings.Contains(out, "k=1") {
		t.Errorf("结构化路径属性缺失: %s", out)
	}
}

func TestHlogLogger_PrintfNoAttrsNoTrailingSpace(t *testing.T) {
	var buf syncBuffer
	hlog.SetOutput(&buf)
	t.Cleanup(func() { hlog.SetLogger(hlog.DefaultLogger()) })

	NewHlogLogger("svc").Infof("clean")
	out := buf.String()
	if out == "" {
		t.Fatal("无输出")
	}
	line := strings.TrimRight(strings.SplitN(out, "\n", 2)[0], "\r\n")
	if strings.HasSuffix(line, " ") {
		t.Fatalf("无属性时 printf 输出有尾部空格: %q", line)
	}
}
