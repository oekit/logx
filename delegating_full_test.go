// delegating 全方法矩阵与基础件补充测试（Round-4 覆盖率补课）。
// 用记录型 fake 作为 Default()，断言 Delegating 懒解析转发的每一条路径。
package logx_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/yi-nology/ekit-logx"
	logxhlog "github.com/yi-nology/ekit-logx/hlog"
)

// recLogger 记录型 fake：实现完整 logx.Logger 接口，记录 (level, msg) 与构建器调用。
type recLogger struct {
	mu      sync.Mutex
	entries []string
	attrs   []string
}

func (r *recLogger) record(level, msg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, level+":"+msg)
}

func (r *recLogger) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.entries...)
}

func (r *recLogger) Debug(msg string, _ ...any)                       { r.record("DEBUG", msg) }
func (r *recLogger) Info(msg string, _ ...any)                        { r.record("INFO", msg) }
func (r *recLogger) Warn(msg string, _ ...any)                        { r.record("WARN", msg) }
func (r *recLogger) Error(msg string, _ ...any)                       { r.record("ERROR", msg) }
func (r *recLogger) Debugf(f string, _ ...any)                        { r.record("DEBUG", f) }
func (r *recLogger) Infof(f string, _ ...any)                         { r.record("INFO", f) }
func (r *recLogger) Warnf(f string, _ ...any)                         { r.record("WARN", f) }
func (r *recLogger) Errorf(f string, _ ...any)                        { r.record("ERROR", f) }
func (r *recLogger) CtxDebug(_ context.Context, msg string, _ ...any) { r.record("DEBUG", msg) }
func (r *recLogger) CtxInfo(_ context.Context, msg string, _ ...any)  { r.record("INFO", msg) }
func (r *recLogger) CtxWarn(_ context.Context, msg string, _ ...any)  { r.record("WARN", msg) }
func (r *recLogger) CtxError(_ context.Context, msg string, _ ...any) { r.record("ERROR", msg) }
func (r *recLogger) CtxDebugf(_ context.Context, f string, _ ...any)  { r.record("DEBUG", f) }
func (r *recLogger) CtxInfof(_ context.Context, f string, _ ...any)   { r.record("INFO", f) }
func (r *recLogger) CtxWarnf(_ context.Context, f string, _ ...any)   { r.record("WARN", f) }
func (r *recLogger) CtxErrorf(_ context.Context, f string, _ ...any)  { r.record("ERROR", f) }
func (r *recLogger) WithCtx(_ context.Context) logx.Logger            { return r }
func (r *recLogger) WithService(name string) logx.Logger {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.attrs = append(r.attrs, "service="+name)
	return r
}
func (r *recLogger) WithAttrs(attrs ...logx.Attr) logx.Logger {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, a := range attrs {
		r.attrs = append(r.attrs, a.Key+"="+strings.ReplaceAll(fmt.Sprint(a.Value), " ", "_"))
	}
	return r
}
func (r *recLogger) WithGroup(name string) logx.Logger {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.attrs = append(r.attrs, "group="+name)
	return r
}
func (r *recLogger) SetLevel(_ logx.Level) {}

func setDefaultRestore(t *testing.T, l logx.Logger) {
	t.Helper()
	prev := logx.Default()
	logx.SetDefault(l)
	t.Cleanup(func() { logx.SetDefault(prev) })
}

func TestDelegating_ForwardsFullMethodMatrix(t *testing.T) {
	rec := &recLogger{}
	setDefaultRestore(t, rec)

	d := logx.Delegating("comp")
	d.Debug("d-kv")
	d.Info("i-kv")
	d.Warn("w-kv")
	d.Error("e-kv")
	d.Debugf("d-fmt")
	d.Infof("i-fmt")
	d.Warnf("w-fmt")
	d.Errorf("e-fmt")
	ctx := context.Background()
	d.CtxDebug(ctx, "cd")
	d.CtxInfo(ctx, "ci")
	d.CtxWarn(ctx, "cw")
	d.CtxError(ctx, "ce")
	d.CtxDebugf(ctx, "cdf")
	d.CtxInfof(ctx, "cif")
	d.CtxWarnf(ctx, "cwf")
	d.CtxErrorf(ctx, "cef")

	want := []string{"DEBUG:d-kv", "INFO:i-kv", "WARN:w-kv", "ERROR:e-kv",
		"DEBUG:d-fmt", "INFO:i-fmt", "WARN:w-fmt", "ERROR:e-fmt",
		"DEBUG:cd", "INFO:ci", "WARN:cw", "ERROR:ce",
		"DEBUG:cdf", "INFO:cif", "WARN:cwf", "ERROR:cef"}
	got := rec.snapshot()
	if len(got) != len(want) {
		t.Fatalf("entries = %v, want %d 条", got, len(want))
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("entries[%d] = %q, want %q", i, got[i], w)
		}
	}
	// component 属性经 WithAttrs 注入
	found := false
	for _, a := range rec.attrs {
		if strings.Contains(a, "comp") {
			found = true
		}
	}
	if !found {
		t.Errorf("attrs = %v, want 含 component=comp", rec.attrs)
	}
}

func TestDelegating_NoComponent_UsesDefaultDirectly(t *testing.T) {
	rec := &recLogger{}
	setDefaultRestore(t, rec)

	logx.Delegating().Error("no-comp")
	if got := rec.snapshot(); len(got) != 1 || got[0] != "ERROR:no-comp" {
		t.Fatalf("entries = %v, want [ERROR:no-comp]", got)
	}
}

func TestDelegating_DerivedBuildersKeepLazyResolve(t *testing.T) {
	rec := &recLogger{}
	setDefaultRestore(t, rec)

	d := logx.Delegating("svc-x")
	sub := d.WithService("my-svc").WithGroup("grp").
		WithAttrs(logx.Attr{Key: "k", Value: "v"}).WithCtx(context.Background())
	sub.Error("via-derived")
	if got := rec.snapshot(); len(got) != 1 || got[0] != "ERROR:via-derived" {
		t.Fatalf("entries = %v", got)
	}
	if len(rec.attrs) < 3 {
		t.Errorf("attrs = %v, want service/group/attr 均被记录", rec.attrs)
	}
}

func TestSetDefault_NilPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("SetDefault(nil) 应 panic")
		}
	}()
	logx.SetDefault(nil)
}

func TestLevelString_Unknown(t *testing.T) {
	if got := logx.LevelString(logx.Level(99)); got != "UNKNOWN" {
		t.Errorf("LevelString(99) = %q, want UNKNOWN", got)
	}
	if logx.NewBaseLogger("svc", logx.LevelInfo).Service != "svc" {
		t.Error("NewBaseLogger 未保留 service")
	}
}

func TestIsHlogBacked(t *testing.T) {
	if logx.IsHlogBacked(&recLogger{}) {
		t.Error("普通 logger 不应被识别为 hlog 后端")
	}
	if !logx.IsHlogBacked(logxhlog.NewHlogLogger("svc")) {
		t.Error("hlog 后端应被 IsHlogBacked 识别")
	}
}

// —— sink.go：MultiSink / FileSink / RotatingSink ——

type errSink struct{}

func (e *errSink) Write([]byte) (int, error) { return 0, errors.New("boom") }
func (e *errSink) Sync() error               { return errors.New("sync-boom") }
func (e *errSink) Close() error              { return nil }

type memSink struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (m *memSink) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.buf.Write(p)
}
func (m *memSink) Sync() error  { return nil }
func (m *memSink) Close() error { return nil }

func TestMultiSink_WriteErrorIsolated_SyncAndClose(t *testing.T) {
	mem := &memSink{}
	sink := logx.NewMultiSink(&errSink{}, mem)
	if _, err := sink.Write([]byte("x")); err != nil {
		t.Fatalf("单 sink 失败不应阻塞整体: %v", err)
	}
	if !strings.Contains(mem.buf.String(), "x") {
		t.Errorf("健康 sink 未收到写入: %q", mem.buf.String())
	}
	if err := sink.Sync(); err == nil {
		t.Error("Sync 应上抛 sink 错误")
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := sink.Write([]byte("y")); !errors.Is(err, os.ErrClosed) {
		t.Errorf("关闭后 Write = %v, want os.ErrClosed", err)
	}
	if err := sink.Sync(); !errors.Is(err, os.ErrClosed) {
		t.Errorf("关闭后 Sync = %v, want os.ErrClosed", err)
	}
}

func TestFileSink_WriteSyncClosedCycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.log")
	sink, err := logx.NewFileSink(path)
	if err != nil {
		t.Fatalf("NewFileSink: %v", err)
	}
	if _, err := sink.Write([]byte("hello")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := sink.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := sink.Write([]byte("z")); !errors.Is(err, os.ErrClosed) {
		t.Errorf("关闭后 Write = %v, want os.ErrClosed", err)
	}
	if err := sink.Sync(); !errors.Is(err, os.ErrClosed) {
		t.Errorf("关闭后 Sync = %v, want os.ErrClosed", err)
	}
}

func TestRotatingSink_WritesAndRotates(t *testing.T) {
	base := filepath.Join(t.TempDir(), "rot.log")
	sink, err := logx.NewRotatingSink(base, 1)
	if err != nil {
		t.Fatalf("NewRotatingSink: %v", err)
	}
	for i := 0; i < 100; i++ {
		if _, err := sink.Write([]byte("rotating-sink-line-padding-0123456789\n")); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// —— 全局快捷方法（logger.go 委托段）经 std 后端落到标准库 log ——

func TestGlobalShortcuts_RouteToDefault(t *testing.T) {
	var buf strings.Builder
	prevWriter := log.Writer()
	log.SetOutput(writerFunc(func(p []byte) (int, error) { return buf.Write(p) }))
	t.Cleanup(func() { log.SetOutput(prevWriter) })
	setDefaultRestore(t, logx.NewStdLogger("shortcut-svc"))

	logx.WithService("svc-x").Error("shortcut")
	logx.WithCtx(context.Background()).Warn("withctx")
	logx.Debug("g-debug")
	logx.Infof("g-info-fmt")
	out := buf.String()
	for _, want := range []string{"shortcut", "withctx", "g-debug", "g-info-fmt"} {
		if !strings.Contains(out, want) {
			t.Errorf("输出缺 %q: %q", want, out)
		}
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }
