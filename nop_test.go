package logx

import (
	"bytes"
	"context"
	"io"
	"log"
	"os"
	"testing"
)

// captureAllOutput 同时接管**标准 log 的输出**、**os.Stderr** 与
// **os.Stdout**，返回收尾函数；调用收尾函数拿到三处的全部输出。
//
// 为什么不能只复用 captureStderr（只重定向 log.Writer）：logx 的 std 后端走
// log.Printf ⇒ 落在 log.Writer 上；但一个「不输出」的 logger 实现也可能走
// fmt.Print* ⇒ 落在 os.Stdout/os.Stderr 上。只测前者会漏掉后者——「零输出」
// 这条承诺必须覆盖各个出口。
//
// 标准 log 的默认输出器在 init 时就绑定了当时的 os.Stderr，所以换 os.Stderr
// 变量**不会**影响它 ⇒ 两个出口必须分别接管。
//
// 已知边界：Go 内建的 println/print 直接写 fd 1/2，绕过 os.Stdout/os.Stderr
// 变量 ⇒ 捕获不到。日志实现不会走内建 print（要格式化就必然用 fmt/log），
// 故不为此引入 syscall.Dup2 级别的手段。
func captureAllOutput(t *testing.T) func() string {
	t.Helper()
	logMu.Lock()

	var buf bytes.Buffer
	prevWriter := log.Writer()
	log.SetOutput(&buf)

	pipeOut, wOut, err := os.Pipe()
	if err != nil {
		log.SetOutput(prevWriter)
		logMu.Unlock()
		t.Fatalf("pipe: %v", err)
	}
	pipeErr, wErr, err := os.Pipe()
	if err != nil {
		_ = pipeOut.Close()
		_ = wOut.Close()
		log.SetOutput(prevWriter)
		logMu.Unlock()
		t.Fatalf("pipe: %v", err)
	}
	prevStdout, prevStderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = wOut, wErr

	read := make(chan string, 2)
	go func() {
		b, _ := io.ReadAll(pipeOut)
		read <- string(b)
	}()
	go func() {
		b, _ := io.ReadAll(pipeErr)
		read <- string(b)
	}()

	var once bool
	return func() string {
		if once {
			t.Fatal("captureAllOutput 收尾函数只能调用一次")
		}
		once = true
		os.Stdout, os.Stderr = prevStdout, prevStderr
		_ = wOut.Close()
		_ = wErr.Close()
		stdout := <-read
		stderr := <-read
		_ = pipeOut.Close()
		_ = pipeErr.Close()
		log.SetOutput(prevWriter)
		logMu.Unlock()
		return buf.String() + stdout + stderr
	}
}

// TestNop_ProducesNoOutput 钉住 Nop() 的核心承诺：注入组件后测试输出零噪声。
//
// 此前想「不输出任何日志」只有两条路，都不成立：把级别设到 LevelError 仍会
// 打 error；自己实现 Logger 接口要写 17 个方法。于是下游各写一份替代品或
// 干脆绕开（#41）。
func TestNop_ProducesNoOutput(t *testing.T) {
	finish := captureAllOutput(t)
	l := Nop()

	l.Debug("d")
	l.Info("i")
	l.Warn("w")
	l.Error("e")
	l.Debugf("d%d", 1)
	l.Infof("i%d", 1)
	l.Warnf("w%d", 1)
	l.Errorf("e%d", 1)
	ctx := context.Background()
	l.CtxDebug(ctx, "cd")
	l.CtxInfo(ctx, "ci")
	l.CtxWarn(ctx, "cw")
	l.CtxError(ctx, "ce")
	l.CtxDebugf(ctx, "cd%d", 1)
	l.CtxInfof(ctx, "ci%d", 1)
	l.CtxWarnf(ctx, "cw%d", 1)
	l.CtxErrorf(ctx, "ce%d", 1)

	if out := finish(); out != "" {
		t.Fatalf("Nop() produced output: %q", out)
	}
}

// TestNop_DerivedLoggersStaySilent 钉住派生链：With* 之后仍是静默的。
func TestNop_DerivedLoggersStaySilent(t *testing.T) {
	finish := captureAllOutput(t)
	base := Nop()

	base.WithService("svc").Info("a")
	base.WithCtx(context.Background()).Info("b")
	base.WithAttrs(Attr{Key: "k", Value: "v"}).Info("c")
	base.WithGroup("g").Info("d")
	base.WithService("svc").WithAttrs(Attr{Key: "k", Value: "v"}).WithGroup("g").Error("e")

	if out := finish(); out != "" {
		t.Fatalf("derived Nop loggers produced output: %q", out)
	}
}

// TestNop_SetLevelIsNoOp 钉住 SetLevel 对静默 logger 无影响（调级别不应
// 让它开始输出）。
func TestNop_SetLevelIsNoOp(t *testing.T) {
	finish := captureAllOutput(t)
	l := Nop()
	l.SetLevel(LevelDebug)
	l.Info("still silent")
	if out := finish(); out != "" {
		t.Fatalf("Nop() produced output after SetLevel: %q", out)
	}
}

// TestNop_EnabledAlwaysFalse 钉住 Nop 实现 LevelEnabler：热路径守卫
// logx.Enabled 会**跳过昂贵参数求值**，而不只是不打印。
func TestNop_EnabledAlwaysFalse(t *testing.T) {
	l := Nop()
	le, ok := l.(LevelEnabler)
	if !ok {
		t.Fatal("Nop() must implement LevelEnabler")
	}
	for _, lvl := range []Level{LevelDebug, LevelInfo, LevelWarn, LevelError, LevelOff} {
		if le.Enabled(lvl) {
			t.Errorf("Enabled(%v) = true, want false", lvl)
		}
	}
	if Enabled(l, LevelError) {
		t.Error("logx.Enabled(Nop(), LevelError) = true, want false")
	}
}

// TestLevelOff_Silent 钉住「保留结构化日志但本进程不落盘」那一档：
// LevelOff 比 LevelError 更高 ⇒ 任何级别都不输出。
func TestLevelOff_Silent(t *testing.T) {
	finish := captureAllOutput(t)
	l := NewStdLogger("svc", LevelOff)

	l.Debug("d")
	l.Info("i")
	l.Warn("w")
	l.Error("e")

	if out := finish(); out != "" {
		t.Fatalf("LevelOff produced output: %q", out)
	}
}

// TestLevelOff_ExistingLevelsUnchanged 钉住「既有四档逐字不变」：
// LevelOff 是**新增**的一档，不能把既有四档的取值或行为挪动。
func TestLevelOff_ExistingLevelsUnchanged(t *testing.T) {
	if LevelDebug != 0 || LevelInfo != 1 || LevelWarn != 2 || LevelError != 3 {
		t.Fatalf("既有四档取值被挪动了: debug=%d info=%d warn=%d error=%d",
			LevelDebug, LevelInfo, LevelWarn, LevelError)
	}
	if LevelOff != 4 {
		t.Fatalf("LevelOff = %d, want 4", LevelOff)
	}
	if got := LevelString(LevelOff); got != "OFF" {
		t.Errorf("LevelString(LevelOff) = %q, want %q", got, "OFF")
	}
	// 既有四档名称不变。
	for lvl, want := range map[Level]string{
		LevelDebug: "DEBUG", LevelInfo: "INFO", LevelWarn: "WARN", LevelError: "ERROR",
	} {
		if got := LevelString(lvl); got != want {
			t.Errorf("LevelString(%v) = %q, want %q", lvl, got, want)
		}
	}
	// LevelError 仍然会打 error（LevelOff 是新档，不是替换它）。
	buf := captureStderr(t)
	NewStdLogger("svc", LevelError).Error("boom")
	if out := buf.String(); out == "" {
		t.Fatal("LevelError should still output")
	}
}

// TestParseLogLevel_Off 钉住 OFF 可解析：此前 LOG_LEVEL=OFF 只能报「未知
// 级别」再退回默认 ⇒ 「想彻底关掉日志」没有可达路径。
func TestParseLogLevel_Off(t *testing.T) {
	lvl, err := ParseLogLevel("OFF")
	if err != nil || lvl != LevelOff {
		t.Fatalf("ParseLogLevel(\"OFF\") = %v, %v; want %v, nil", lvl, err, LevelOff)
	}
	if got := ParseLogLevelOr("off", LevelInfo); got != LevelOff {
		t.Fatalf("ParseLogLevelOr(\"off\", LevelInfo) = %v, want %v", got, LevelOff)
	}
	// 既有解析口径不变。
	if lvl, err := ParseLogLevel("error"); err != nil || lvl != LevelError {
		t.Fatalf("ParseLogLevel(\"error\") = %v, %v; want %v, nil", lvl, err, LevelError)
	}
	if _, err := ParseLogLevel("verbose"); err == nil {
		t.Fatal("unknown level should still error")
	}
}
