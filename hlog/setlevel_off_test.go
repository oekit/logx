package hlog

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/oekit/logx"
)

// TestHlogLogger_SetLevelOffKeepsGlobalTight（#102）：SetLevel(LevelOff)
// 必须与构造期 initHlogLevel 同样把 hlog 全局级别收到 Error（OFF 意图＝
// 更严，绝不放宽）——此前 switch 缺 OFF 分支落 default→Info，「关日志」
// 反而打开了 hertz 进程级 Info 日志。
func TestHlogLogger_SetLevelOffKeepsGlobalTight(t *testing.T) {
	var buf syncBuffer
	hlog.SetOutput(&buf)
	t.Cleanup(func() { hlog.SetOutput(nil) })

	l := NewHlogLogger("svc-off")
	l.SetLevel(logx.LevelOff)

	// 实例侧：OFF 档实例 ShouldLog 全拦
	l.Info("instance-info-must-not-print")

	// 全局侧：hertz 框架自身（直接走 hlog 包）在 OFF 后不得打 Info
	hlog.CtxInfof(context.Background(), "GLOBAL-INFO-AFTER-OFF")
	hlog.CtxErrorf(context.Background(), "GLOBAL-ERROR-AFTER-OFF")

	out := buf.String()
	if strings.Contains(out, "GLOBAL-INFO-AFTER-OFF") {
		t.Errorf("SetLevel(OFF) 后 hlog 全局 Info 仍在打印（被放宽到 Info）: %s", out)
	}
	if !strings.Contains(out, "GLOBAL-ERROR-AFTER-OFF") {
		t.Errorf("SetLevel(OFF) 后 hlog 全局 Error 应保持可打（OFF≠断崖）: %s", out)
	}
	if strings.Contains(out, "instance-info-must-not-print") {
		t.Errorf("实例 OFF 档应拦截 Info: %s", out)
	}

	// 对照：SetLevel(Warn) 时全局 Info 被拦、Warn 可打（回归护栏）
	buf.reset()
	l.SetLevel(logx.LevelWarn)
	hlog.CtxInfof(context.Background(), "GLOBAL-INFO-AFTER-WARN")
	hlog.CtxWarnf(context.Background(), "GLOBAL-WARN-AFTER-WARN")
	out = buf.String()
	if strings.Contains(out, "GLOBAL-INFO-AFTER-WARN") {
		t.Errorf("SetLevel(WARN) 后 hlog 全局 Info 不应打印: %s", out)
	}
	if !strings.Contains(out, "GLOBAL-WARN-AFTER-WARN") {
		t.Errorf("SetLevel(WARN) 后 hlog 全局 Warn 应可打: %s", out)
	}
}

// reset 清空捕获缓冲（syncBuffer 定义于 adapter_full_test.go，本用例内复用）。
func (b *syncBuffer) reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf.Reset()
}
