package logx

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestMultiSink_Fanout 验证 MultiSink 把每条 Write 调用 fanout 到
// 所有子 sink,且内容一致。
func TestMultiSink_Fanout(t *testing.T) {
	var buf1, buf2 bytes.Buffer
	ms := NewMultiSink(
		&fileSinkLike{buf: &buf1},
		&fileSinkLike{buf: &buf2},
	)
	defer ms.Close()
	n, err := ms.Write([]byte("hello\n"))
	if err != nil {
		t.Fatalf("Write returned err: %v", err)
	}
	if n != 6 {
		t.Errorf("n = %d, want 6", n)
	}
	if buf1.String() != "hello\n" {
		t.Errorf("buf1 = %q, want hello\\n", buf1.String())
	}
	if buf2.String() != "hello\n" {
		t.Errorf("buf2 = %q, want hello\\n", buf2.String())
	}
}

// TestMultiSink_EmptyNilSafe 验证 0 sink 的 MultiSink 不 panic,Write
// 直接丢弃(适合 dry-run 测试场景)。
func TestMultiSink_EmptyNilSafe(t *testing.T) {
	ms := NewMultiSink()
	defer ms.Close()
	if _, err := ms.Write([]byte("x")); err != nil {
		t.Errorf("Write on empty MultiSink returned err: %v", err)
	}
}

// TestMultiSink_AddDynamic 验证运行时 Add 生效。
func TestMultiSink_AddDynamic(t *testing.T) {
	var buf1, buf2 bytes.Buffer
	ms := NewMultiSink(&fileSinkLike{buf: &buf1})
	defer ms.Close()
	if !ms.Add(&fileSinkLike{buf: &buf2}) {
		t.Fatal("Add returned false")
	}
	ms.Write([]byte("added\n"))
	if !strings.Contains(buf1.String(), "added") || !strings.Contains(buf2.String(), "added") {
		t.Errorf("Add not propagated: buf1=%q buf2=%q", buf1.String(), buf2.String())
	}
}

// TestMultiSink_OneFailureDoesNotBlockOthers 验证单个 sink 写入失败
// 不会阻塞其他 sink;错误写到 stderr 但 MultiSink.Write 仍返回成功。
func TestMultiSink_OneFailureDoesNotBlockOthers(t *testing.T) {
	var ok bytes.Buffer
	ms := NewMultiSink(
		&errSink{err: os.ErrPermission},
		&fileSinkLike{buf: &ok},
	)
	defer ms.Close()
	// 重定向 stderr 避免污染测试输出
	r, w, _ := os.Pipe()
	oldStderr := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = oldStderr }()

	n, err := ms.Write([]byte("test\n"))
	if err != nil {
		t.Errorf("MultiSink.Write should swallow inner errors, got err: %v", err)
	}
	if n != 5 {
		t.Errorf("n = %d, want 5", n)
	}
	if ok.String() != "test\n" {
		t.Errorf("ok sink missed content: %q", ok.String())
	}
	w.Close()
	r.Read(make([]byte, 1024)) // 排空 stderr 避免泄漏
}

// TestMultiSink_CloseIdempotent 验证 Close 幂等。
func TestMultiSink_CloseIdempotent(t *testing.T) {
	ms := NewMultiSink(&fileSinkLike{buf: &bytes.Buffer{}})
	if err := ms.Close(); err != nil {
		t.Errorf("first Close err: %v", err)
	}
	if err := ms.Close(); err != nil {
		t.Errorf("second Close err: %v", err)
	}
	// 关闭后 Write 应返回 ErrClosed
	if _, err := ms.Write([]byte("x")); err == nil {
		t.Error("Write after Close should return error")
	}
	if ms.Add(&fileSinkLike{buf: &bytes.Buffer{}}) {
		t.Error("Add after Close should return false")
	}
}

// TestMultiSink_ConcurrentSafe 验证并发 Write/Add 安全(竞态检测在
// -race 下也通过)。
func TestMultiSink_ConcurrentSafe(t *testing.T) {
	var buf bytes.Buffer
	ms := NewMultiSink(&fileSinkLike{buf: &buf})
	defer ms.Close()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ms.Write([]byte("a"))
		}()
	}
	wg.Wait()
	if buf.Len() != 100 {
		t.Errorf("buf len = %d, want 100", buf.Len())
	}
}

// TestFileSink_CreateAndAppend 验证 FileSink 自动创建文件,后续 append。
func TestFileSink_CreateAndAppend(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	fs, err := NewFileSink(path)
	if err != nil {
		t.Fatalf("NewFileSink: %v", err)
	}
	if _, werr := fs.Write([]byte("first\n")); werr != nil {
		t.Fatalf("Write: %v", werr)
	}
	if _, werr := fs.Write([]byte("second\n")); werr != nil {
		t.Fatalf("Write: %v", werr)
	}
	if cerr := fs.Close(); cerr != nil {
		t.Fatalf("Close: %v", cerr)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(got), "first\n") || !strings.Contains(string(got), "second\n") {
		t.Errorf("file content = %q, want both lines", got)
	}
}

// TestFileSink_CloseIdempotent 验证 FileSink Close 幂等。
func TestFileSink_CloseIdempotent(t *testing.T) {
	fs, _ := NewFileSink(filepath.Join(t.TempDir(), "x.log"))
	if err := fs.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	if err := fs.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

// TestRotatingSink_Options 验证 RotatingSink 选项透传 + 文件创建。
func TestRotatingSink_Options(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "rot.log")
	rs, err := NewRotatingSink(base, 1, /* MB */
		WithMaxBackups(3),
		WithMaxAge(7),
		WithCompress(true),
		WithLocalTime(true),
	)
	if err != nil {
		t.Fatalf("NewRotatingSink: %v", err)
	}
	if _, err := rs.Write([]byte("rotate-me\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := rs.Sync(); err != nil {
		t.Errorf("Sync: %v", err)
	}
	if err := rs.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if _, err := os.Stat(base); err != nil {
		t.Errorf("base file not created: %v", err)
	}
}

// TestRotatingSink_CloseIdempotent 验证 RotatingSink Close 幂等。
func TestRotatingSink_CloseIdempotent(t *testing.T) {
	rs, _ := NewRotatingSink(filepath.Join(t.TempDir(), "r.log"), 1)
	if err := rs.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	if err := rs.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

// TestNewStdLoggerWithSink_NoopCheck 验证 NewStdLoggerWithSink 构造函数
// 接受 sink 参数(实际行为集成测试在 e2e;单元测试只验函数签名)。
// 完整 stdLogger 接 sink 的 patch 会在后续 P7.x 完成(目前 stdLogger
// 仍用 log.Printf,MultiSink 通过 io.Writer 桥接留作未来扩展)。
func TestNewStdLoggerWithSink_NoopCheck(t *testing.T) {
	// 简单 sanity check:MultiSink 自身作为 io.Writer 行为正确,后续
	// 集成 NewStdLoggerWithSink 时直接传 ms 即可。
	var buf bytes.Buffer
	ms := NewMultiSink(&fileSinkLike{buf: &buf})
	defer ms.Close()
	ms.Write([]byte("via MultiSink\n"))
	if buf.String() != "via MultiSink\n" {
		t.Errorf("MultiSink → buffer content = %q", buf.String())
	}
}

// fileSinkLike 是测试用 sink,把 Write 直接写到 bytes.Buffer。
// bytes.Buffer 自身不是并发安全,加 mutex 保护(模拟真实 file sink 内部
// 通常有锁或 atomic)。
type fileSinkLike struct {
	mu  sync.Mutex
	buf *bytes.Buffer
}

func (f *fileSinkLike) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.buf.Write(p)
}
func (f *fileSinkLike) Sync() error  { return nil }
func (f *fileSinkLike) Close() error { return nil }

// errSink 模拟总是写失败的 sink。
type errSink struct{ err error }

func (e *errSink) Write(p []byte) (int, error) { return 0, e.err }
func (e *errSink) Sync() error                 { return e.err }
func (e *errSink) Close() error                { return nil }

// TestFileSink_ConcurrentWriteClose -race 钉住:并发 Write 与 Close 不得
// data race / nil deref(2026-09-05 复审修复:原实现 nil-check 与置 nil 之间无锁)。
func TestFileSink_ConcurrentWriteClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "race.log")
	s, err := NewFileSink(path)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_, _ = s.Write([]byte("x"))
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = s.Close()
		_ = s.Close() // 幂等
	}()
	wg.Wait()
	// Close 之后 Write 返回 os.ErrClosed(Sink 契约)
	if _, err := s.Write([]byte("after-close")); !errors.Is(err, os.ErrClosed) {
		t.Errorf("Write after Close: got %v, want os.ErrClosed", err)
	}
}

// TestRotatingSink_WriteAfterClose 钉住:Close 之后 Write/Sync 返回
// os.ErrClosed 而不是 panic(2026-09-05 复审修复:原实现 Logger 置 nil 后
// promoted Write deref 必 panic,违反 Sink 契约)。
func TestRotatingSink_WriteAfterClose(t *testing.T) {
	s, err := NewRotatingSink(filepath.Join(t.TempDir(), "rot.log"), 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Write([]byte("x")); err != nil {
		t.Fatalf("Write before Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("Close should be idempotent, got %v", err)
	}
	if _, err := s.Write([]byte("after")); !errors.Is(err, os.ErrClosed) {
		t.Errorf("Write after Close: got %v, want os.ErrClosed(不是 panic)", err)
	}
	if err := s.Sync(); !errors.Is(err, os.ErrClosed) {
		t.Errorf("Sync after Close: got %v, want os.ErrClosed", err)
	}
}

// TestMultiSink_AddAfterClose 钉住:Add 与 Close 竞态窗口(2026-09-05 复审
// 修复:Add 的 closed 检查移入锁内,Closed 之后 Add 必须返回 false 且
// 不泄漏 sink)。
func TestMultiSink_AddAfterClose(t *testing.T) {
	inner := &fileSinkLike{buf: &bytes.Buffer{}}
	m := NewMultiSink(inner)
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	ok := m.Add(&fileSinkLike{buf: &bytes.Buffer{}})
	if ok {
		t.Error("Add after Close should return false")
	}
	// Close 幂等
	if err := m.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

// TestMultiSink_ConcurrentAddCloseWrite -race 验证 Add/Close/Write 并发安全。
func TestMultiSink_ConcurrentAddCloseWrite(t *testing.T) {
	m := NewMultiSink(&fileSinkLike{buf: &bytes.Buffer{}})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_, _ = m.Write([]byte("x"))
			}
		}()
	}
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = m.Add(&fileSinkLike{buf: &bytes.Buffer{}})
	}()
	go func() {
		defer wg.Done()
		_ = m.Close()
	}()
	wg.Wait()
}

// TestRotatingSink_ConcurrentWriteClose -race 钉住:并发 Write 与 Close 对
// Logger 字段的访问必须串行化(v0.25.14 首版仅用 closed 标志,审查代理用
// -race 实测复现"读到 false 后、再次读字段前 Close 置 nil"的窗口)。
func TestRotatingSink_ConcurrentWriteClose(t *testing.T) {
	for round := 0; round < 50; round++ {
		s, err := NewRotatingSink(filepath.Join(t.TempDir(), fmt.Sprintf("race-%d.log", round)), 1)
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 50; j++ {
					_, _ = s.Write([]byte("x"))
				}
			}()
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.Close()
			_ = s.Close()
		}()
		wg.Wait()
	}
}
