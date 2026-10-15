package logx

import (
	"runtime"
	"testing"
)

// TestSkipCallerFiles_Registry 验证注册表的去重、合并与匹配逻辑。
// caller 归属的端到端行为见 slog_caller_ext_test.go（外部测试包，
// 避免本包测试帧被 "/observability/logx" 包名启发式吞掉）。
func TestSkipCallerFiles_Registry(t *testing.T) {
	old := callerSkipFiles.Load()
	defer callerSkipFiles.Store(old)

	callerSkipFiles.Store(nil)

	SkipCallerFiles("a.go", "b.go")
	SkipCallerFiles("b.go", "c.go") // b.go 重复，去重
	got := callerSkipFiles.Load()
	if got == nil || len(*got) != 3 {
		t.Fatalf("registry = %v, want [a.go b.go c.go]", got)
	}

	for _, f := range []string{"a.go", "b.go", "c.go"} {
		if !isSkipFile(got, f) {
			t.Errorf("isSkipFile(%q) = false, want true", f)
		}
	}
	if isSkipFile(got, "d.go") {
		t.Error(`isSkipFile("d.go") = true, want false`)
	}
	if isSkipFile(nil, "a.go") {
		t.Error("isSkipFile(nil, ...) = true, want false")
	}

	SkipCallerFiles() // 空注册是 no-op
	if got := callerSkipFiles.Load(); len(*got) != 3 {
		t.Fatalf("SkipCallerFiles() mutated registry: %v", *got)
	}

	_, selfFile, _, _ := runtime.Caller(0)
	SkipCallerFiles(selfFile)
	if got := callerSkipFiles.Load(); len(*got) != 4 {
		t.Fatalf("registry after self registration = %v, want 4 entries", *got)
	}
}
