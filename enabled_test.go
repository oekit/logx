package logx

import (
	"context"
	"testing"
)

// plainLogger 是不实现 LevelEnabler 的 Logger 测试桩——覆盖 Enabled helper
// 的保守回退分支（未实现时恒 true）。
type plainLogger struct{}

func (plainLogger) Debug(string, ...any)                      {}
func (plainLogger) Info(string, ...any)                       {}
func (plainLogger) Warn(string, ...any)                       {}
func (plainLogger) Error(string, ...any)                      {}
func (plainLogger) Debugf(string, ...any)                     {}
func (plainLogger) Infof(string, ...any)                      {}
func (plainLogger) Warnf(string, ...any)                      {}
func (plainLogger) Errorf(string, ...any)                     {}
func (plainLogger) CtxDebug(context.Context, string, ...any)  {}
func (plainLogger) CtxInfo(context.Context, string, ...any)   {}
func (plainLogger) CtxWarn(context.Context, string, ...any)   {}
func (plainLogger) CtxError(context.Context, string, ...any)  {}
func (plainLogger) CtxDebugf(context.Context, string, ...any) {}
func (plainLogger) CtxInfof(context.Context, string, ...any)  {}
func (plainLogger) CtxWarnf(context.Context, string, ...any)  {}
func (plainLogger) CtxErrorf(context.Context, string, ...any) {}
func (plainLogger) WithCtx(context.Context) Logger            { return plainLogger{} }
func (plainLogger) WithService(string) Logger                 { return plainLogger{} }
func (plainLogger) WithAttrs(...Attr) Logger                  { return plainLogger{} }
func (plainLogger) WithGroup(string) Logger                   { return plainLogger{} }
func (plainLogger) SetLevel(Level)                            {}

func TestEnabled_StdHonorsLevel(t *testing.T) {
	l := NewStdLogger("enabled-test", LevelError)

	if Enabled(l, LevelError) != true {
		t.Error("Enabled(Error) 应为 true（level >= LevelError）")
	}
	if Enabled(l, LevelDebug) != false {
		t.Error("Enabled(Debug) 应为 false（Debug < LevelError 被过滤）")
	}

	l.SetLevel(LevelDebug)
	if Enabled(l, LevelDebug) != true {
		t.Error("SetLevel(Debug) 后 Enabled(Debug) 应为 true（运行期调级生效）")
	}
}

func TestEnabled_SlogHonorsLevel(t *testing.T) {
	l := NewSlogLogger("enabled-test", WithLevel(LevelWarn))

	if Enabled(l, LevelWarn) != true {
		t.Error("slog 后端 Enabled(Warn) 应为 true")
	}
	if Enabled(l, LevelInfo) != false {
		t.Error("slog 后端 Enabled(Info) 应为 false（Info < LevelWarn 被过滤）")
	}
}

func TestEnabled_PlainLoggerDefaultsTrue(t *testing.T) {
	var l Logger = plainLogger{}
	if Enabled(l, LevelDebug) != true {
		t.Error("未实现 LevelEnabler 的 logger 应保守返回 true（不误吞日志）")
	}
}

func TestEnabled_DelegatingPassthrough(t *testing.T) {
	old := Default()
	t.Cleanup(func() { SetDefault(old) })

	SetDefault(NewStdLogger("enabled-delegating", LevelError))
	d := Delegating("enabled-test")
	if Enabled(d, LevelDebug) != false {
		t.Error("Delegating 应透传解析目标的级别判定（Debug 被过滤）")
	}
	if Enabled(d, LevelError) != true {
		t.Error("Delegating 应透传解析目标的级别判定（Error 放行）")
	}

	// 运行期 SetDefault 后跟随新实现
	SetDefault(NewStdLogger("enabled-delegating-2", LevelDebug))
	if Enabled(d, LevelDebug) != true {
		t.Error("SetDefault 后 Delegating 应跟随新目标的级别判定")
	}
}

func TestEnabled_WithServiceSubLoggerKeepsEnabler(t *testing.T) {
	l := NewStdLogger("enabled-sub", LevelError).WithService("svc")
	if Enabled(l, LevelDebug) != false {
		t.Error("WithService 子 logger 应保留 Enabled 能力")
	}
}

// TestEnabled_FilterLoggerHonorsLevel 钉住 #76：filterLogger 必须实现
// LevelEnabler——此前未实现，logx.Enabled 恒 true（实测底层 LevelError 时
// Enabled(Debug) 仍 true），被过滤事件仍付出全部序列化/脱敏成本。
func TestEnabled_FilterLoggerHonorsLevel(t *testing.T) {
	l := WithFilter(NewStdLogger("enabled-filter", LevelError), FilterKey("password"))
	if Enabled(l, LevelDebug) != false {
		t.Error("filterLogger 应透传底层级别判定（Debug 被过滤）")
	}
	if Enabled(l, LevelError) != true {
		t.Error("filterLogger 应透传底层级别判定（Error 放行）")
	}
}

// TestEnabled_AuditLoggerHonorsLevel 钉住 #76 的审计包装器面。
func TestEnabled_AuditLoggerHonorsLevel(t *testing.T) {
	l := NewAuditLogger("enabled-audit", WithHandler(newCaptureHandler()), WithLevel(LevelWarn))
	if Enabled(l, LevelDebug) != false {
		t.Error("auditLogger 应透传内部级别判定（Debug 被过滤）")
	}
	if Enabled(l, LevelWarn) != true {
		t.Error("auditLogger 应透传内部级别判定（Warn 放行）")
	}
}
