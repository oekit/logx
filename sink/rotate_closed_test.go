package sink

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestRotatingSink_RotateAfterClose（#103）：Close 之后调用 Rotate 必须
// 返回 os.ErrClosed 而非 panic。此前 lumberjack 的 Rotate() 被原样提升到
// RotatingSink 上，Close 里 s.Logger 置 nil 后调用它，lumberjack 内部对
// nil 指针解引用直接崩溃，绕过「Close 后只返回 error」的 Sink 契约。
func TestRotatingSink_RotateAfterClose(t *testing.T) {
	dir := t.TempDir()
	rs, err := NewRotatingSink(filepath.Join(dir, "app.log"), 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rs.Write([]byte("hello")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	// Close 前正常透传 lumberjack.Rotate（手动轮转不 panic、不报错）
	if err := rs.Rotate(); err != nil {
		t.Fatalf("Rotate before Close: %v", err)
	}
	if err := rs.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	err = rs.Rotate()
	if !errors.Is(err, os.ErrClosed) {
		t.Fatalf("Rotate after Close = %v, want os.ErrClosed（且不得 panic）", err)
	}
	// 契约一致性：Write/Sync 同口径
	if _, err := rs.Write([]byte("x")); !errors.Is(err, os.ErrClosed) {
		t.Errorf("Write after Close = %v, want os.ErrClosed", err)
	}
	if err := rs.Sync(); !errors.Is(err, os.ErrClosed) {
		t.Errorf("Sync after Close = %v, want os.ErrClosed", err)
	}
}
