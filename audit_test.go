package logx

import "testing"

func TestNewAuditLogger_IgnoresLOGLevel(t *testing.T) {
	t.Setenv("LOG_LEVEL", "ERROR")
	h := newCaptureHandler()
	l := NewAuditLogger("audit", WithHandler(h))
	l.Info("audit-event", "uid", "u1") // Info 级审计不得被 LOG_LEVEL=ERROR 过滤
	if len(h.snapshot()) != 1 {
		t.Fatalf("audit Info must not be filtered by LOG_LEVEL=ERROR, got %d records", len(h.snapshot()))
	}
}

func TestNewAuditLogger_SetLevelRejectsDowngrade(t *testing.T) {
	h := newCaptureHandler()
	l := NewAuditLogger("audit", WithHandler(h))
	al := l.(auditLogger)
	al.SetLevel(LevelError) // 拒绝：会吞掉 Info 审计
	al.Info("must-keep")
	if len(h.snapshot()) != 1 {
		t.Fatalf("SetLevel(Error) should be rejected, got %d records", len(h.snapshot()))
	}
	al.SetLevel(LevelDebug) // 更细允许
	al.Debug("debug-ok")
	if len(h.snapshot()) != 2 {
		t.Fatalf("SetLevel(Debug) should be allowed, got %d records", len(h.snapshot()))
	}
}

func TestNewAuditLogger_CloneKeepsGuard(t *testing.T) {
	h := newCaptureHandler()
	l := NewAuditLogger("audit", WithHandler(h)).WithAttrs(Attr{Key: "k", Value: "v"})
	al, ok := l.(auditLogger)
	if !ok {
		t.Fatal("WithAttrs should return auditLogger wrapper")
	}
	al.SetLevel(LevelWarn) // 拒绝
	al.Info("still-logged")
	if len(h.snapshot()) != 1 {
		t.Fatalf("derived SetLevel(Warn) should be rejected, got %d records", len(h.snapshot()))
	}
}
