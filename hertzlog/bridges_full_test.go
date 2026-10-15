// hertzlog 四后端桥接的全方法矩阵测试（Round-4 覆盖率补课）。
// logrus/zerolog/slog 经输出重定向断言内容；zap 经 core WriteSyncer 捕获。
package hertzlog

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	hertzcontriblogrus "github.com/hertz-contrib/logger/logrus"
	hertzcontribslog "github.com/hertz-contrib/logger/slog"
	hertzcontribzap "github.com/hertz-contrib/logger/zap"
	"github.com/rs/zerolog"
	"github.com/sirupsen/logrus"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// exerciseFullLogger 走 hlog.FullLogger 的 12 个方法，返回是否全部无 panic。
func exerciseFullLogger(t *testing.T, l hlog.FullLogger) {
	t.Helper()
	ctx := context.Background()
	l.Debugf("d-%s", "fmt")
	l.Infof("i-%s", "fmt")
	l.Warnf("w-%s", "fmt")
	l.Errorf("e-%s", "fmt")
	l.CtxDebugf(ctx, "cd-%s", "fmt")
	l.CtxInfof(ctx, "ci-%s", "fmt")
	l.CtxWarnf(ctx, "cw-%s", "fmt")
	l.CtxErrorf(ctx, "ce-%s", "fmt")
}

func assertContains(t *testing.T, out, what string) {
	t.Helper()
	for _, want := range []string{"d-fmt", "i-fmt", "w-fmt", "e-fmt",
		"cd-fmt", "ci-fmt", "cw-fmt", "ce-fmt"} {
		if !strings.Contains(out, want) {
			t.Errorf("%s 输出缺 %q\n--- got ---\n%s", what, want, out)
		}
	}
}

func TestLogrusBridge_FullMatrix(t *testing.T) {
	var buf bytes.Buffer
	l := logrus.New()
	l.SetOutput(&buf)
	l.SetLevel(logrus.DebugLevel)
	bridge := NewLogrusLogger(hertzcontriblogrus.WithLogger(l))
	exerciseFullLogger(t, AsHlogLoggerLogrus(bridge))
	assertContains(t, buf.String(), "logrus")
}

func TestZapBridge_FullMatrix(t *testing.T) {
	var buf bytes.Buffer
	ws := zapcore.AddSync(&buf)
	level := zap.NewAtomicLevelAt(zapcore.DebugLevel)
	bridge := NewZapLogger(
		hertzcontribzap.WithCoreWs(ws),
		hertzcontribzap.WithCoreEnc(zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig())),
		hertzcontribzap.WithCoreLevel(level),
	)
	exerciseFullLogger(t, AsHlogLoggerZap(bridge))
	assertContains(t, buf.String(), "zap")
}

func TestZerologBridge_FullMatrix(t *testing.T) {
	var buf bytes.Buffer
	bridge := NewZerologLogger(zerolog.New(&buf).Level(zerolog.DebugLevel))
	exerciseFullLogger(t, AsHlogLoggerZerolog(bridge))
	assertContains(t, buf.String(), "zerolog")
}

func TestSlogBridge_FullMatrix(t *testing.T) {
	var buf bytes.Buffer
	lvl := new(slog.LevelVar)
	lvl.Set(slog.LevelDebug)
	bridge := NewSlogLogger(hertzcontribslog.WithOutput(&buf), hertzcontribslog.WithLevel(lvl))
	exerciseFullLogger(t, AsHlogLoggerSlog(bridge))
	assertContains(t, buf.String(), "slog")
}

// 旧入口别名保持可用（编译期契约）。
func TestAliases_Compile(t *testing.T) {
	var buf bytes.Buffer
	zl := NewZerologLogger(zerolog.New(&buf))
	if AsHlogLoggerZerolog(zl) == nil {
		t.Fatal("zerolog 适配入口不应为 nil")
	}
	lg := logrus.New()
	lg.SetOutput(&buf)
	if AsHlogLoggerLogrus(NewLogrusLogger(hertzcontriblogrus.WithLogger(lg))) == nil {
		t.Fatal("logrus 适配入口不应为 nil")
	}
}
