package hertzlog

import (
	"bytes"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	hertzcontriblogrus "github.com/hertz-contrib/logger/logrus"
	"github.com/sirupsen/logrus"
)

// TestLogrusLogger_RoutesToHlog 验证 NewLogrusLogger + hlog.SetLogger 端到端。
func TestLogrusLogger_RoutesToHlog(t *testing.T) {
	var buf bytes.Buffer
	l := logrus.New()
	l.SetOutput(&buf)
	l.SetLevel(logrus.InfoLevel)
	hertzcontriblogrus.NewLogger(hertzcontriblogrus.WithLogger(l))
	// hlog.SetLogger(hertzcontriblogrus.NewLogger(...)) 是 hertz 的接法;
	// 我们的 AsHlogLoggerFromLogrus 提供 ekit 风格的 1 行包装。
	logger := hertzcontriblogrus.NewLogger(hertzcontriblogrus.WithLogger(l))
	hlog.SetLogger(AsHlogLoggerLogrus(logger))
	defer hlog.SetLogger(hlog.DefaultLogger())

	hlog.Info("ekit-hertzlog-test")
	if !strings.Contains(buf.String(), "ekit-hertzlog-test") {
		t.Errorf("buf = %q, want contains ekit-hertzlog-test", buf.String())
	}
}

// TestLogrusOption_TypeAlias 验证 type alias 编译期兼容。
func TestLogrusOption_TypeAlias(t *testing.T) {
	var opt = hertzcontriblogrus.WithLogger(logrus.New())
	_ = opt
}

// TestLogrusLogger_TypeAlias 验证 type alias 编译期兼容。
func TestLogrusLogger_TypeAlias(t *testing.T) {
	var l = NewLogrusLogger()
	if l == nil {
		t.Fatal("NewLogrusLogger returned nil")
	}
}
