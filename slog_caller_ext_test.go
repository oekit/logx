// 外部测试包（logx_test）：验证 SkipCallerFiles 的端到端 caller 归属。
// 真实的"外部调用方"帧由 internal/logxcallertest 提供——logx 目录内的
// 测试帧（含本文件）会被 callerOutsideLogx 的包名启发式跳过。
package logx_test

import (
	"strings"
	"testing"

	"github.com/yi-nology/ekit-logx"
	"github.com/yi-nology/ekit-logx/internal/logxcallertest"
)

func TestSkipCallerFiles_EndToEnd(t *testing.T) {
	probeFile := logxcallertest.File()

	// 基线：caller 归属探针包文件（第一个包外帧）
	base := logxcallertest.EmitCaller()
	if !strings.Contains(base, "caller_probe.go") {
		t.Fatalf("baseline caller = %q, want it to point into caller_probe.go", base)
	}

	// 注册探针文件后，探针帧按内部帧跳过，caller 归属更外层（logx 测试帧
	// 因包名启发式同样被跳过，最终落到 testing 框架帧），不再出现探针文件。
	// 注册持久化到进程结束：跳过的只有探针文件自身的帧，不影响其他测试。
	logx.SkipCallerFiles(probeFile)
	after := logxcallertest.EmitCaller()
	if strings.Contains(after, "caller_probe.go") {
		t.Fatalf("caller after skip = %q, should no longer point into caller_probe.go", after)
	}
	if after == "" {
		t.Fatal("caller after skip is empty, expected an outer frame")
	}
}
