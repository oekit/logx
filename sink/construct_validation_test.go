package sink

import (
	"path/filepath"
	"testing"
)

// #177：NewRotatingSink 此前恒返回 nil error、参数零校验——basePath 为空
// 时 lumberjack 静默写 os.TempDir()/lumberjack.log，maxSizeMB<=0 时退化为
// 每写必轮转。构造期 fail-fast 是本包 Option 非法值的总则口径。
func TestNewRotatingSinkValidation(t *testing.T) {
	if rs, err := NewRotatingSink("", 10); err == nil {
		t.Errorf("basePath 为空应报错,得到 rs=%v err=%v", rs, err)
	}
	if rs, err := NewRotatingSink(filepath.Join(t.TempDir(), "a.log"), 0); err == nil {
		t.Errorf("maxSizeMB=0 应报错,得到 rs=%v err=%v", rs, err)
	}
	if rs, err := NewRotatingSink(filepath.Join(t.TempDir(), "a.log"), -1); err == nil {
		t.Errorf("maxSizeMB<0 应报错,得到 rs=%v err=%v", rs, err)
	}
	rs, err := NewRotatingSink(filepath.Join(t.TempDir(), "a.log"), 1)
	if err != nil {
		t.Fatalf("合法参数报错: %v", err)
	}
	if err := rs.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// #179：NewMultiSink 传 0 个返回非 nil 空 fanout 实例——Write 吞掉返回
// len(p), nil（godoc 与实现及本断言自此同口径）。
func TestMultiSink_EmptyWriteReturnsLen(t *testing.T) {
	ms := NewMultiSink()
	defer ms.Close()
	if ms == nil {
		t.Fatal("NewMultiSink() 返回 nil,与 godoc「空 fanout 实例」不符")
	}
	n, err := ms.Write([]byte("abcd"))
	if err != nil {
		t.Errorf("Write err = %v, want nil", err)
	}
	if n != 4 {
		t.Errorf("Write n = %d, want 4", n)
	}
}
