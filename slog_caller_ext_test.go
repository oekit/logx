// 外部测试包（logx_test）：验证 SkipCallerFiles 的端到端 caller 归属。
// 真实的"外部调用方"帧由 internal/logxcallertest 提供——logx 目录内的
// 测试帧（含本文件）会被 callerOutsideLogx 的包名启发式跳过。
package logx_test

import (
	"strings"
	"testing"

	"github.com/oekit/logx"
	"github.com/oekit/logx/internal/logxcallertest"
)

func TestSkipCallerFiles_EndToEnd(t *testing.T) {
	probeFile := logxcallertest.File()

	// **必须先保存并在结束时恢复注册表**：caller 跳过注册表是**进程级全局**，
	// 而 SkipCallerFiles 只增不删（设计上就是"注册持久化到进程结束"）。
	// 不恢复的话，本用例自己注册的探针文件会留在全局里 ⇒
	// 第二次跑（-count=2 在同一进程内重复）时探针帧已被跳过 ⇒
	// baseline 落到 testing 框架帧 ⇒ 用例红（实测 -count=1 绿、-count=2 红）。
	// 恢复后本用例对 -count>1 幂等，且不向同批次其它用例泄漏注册表。
	old := logx.CallerSkipFilesForTest()
	t.Cleanup(func() { logx.SetCallerSkipFilesForTest(old) })

	// 基线：caller 归属探针包文件（第一个包外帧）
	base := logxcallertest.EmitCaller()
	if !strings.Contains(base, "caller_probe.go") {
		t.Fatalf("baseline caller = %q, want it to point into caller_probe.go", base)
	}

	// 注册探针文件后，探针帧按内部帧跳过，caller 归属更外层（logx 测试帧
	// 因包名启发式同样被跳过，最终落到 testing 框架帧），不再出现探针文件。
	logx.SkipCallerFiles(probeFile)
	after := logxcallertest.EmitCaller()
	if strings.Contains(after, "caller_probe.go") {
		t.Fatalf("caller after skip = %q, should no longer point into caller_probe.go", after)
	}
	if after == "" {
		t.Fatal("caller after skip is empty, expected an outer frame")
	}
}
