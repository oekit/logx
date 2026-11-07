package logx

import (
	"io"
	"os"
	"strings"
	"testing"
)

// TestSlogLogger_StdoutWriteErrorReportedOnce 钉住 #79：内置 JSON 路径的
// stdout 写失败必须可观测——计数累加、降级 stderr 报告恰好一次。
// 此前 os.Stdout.Write 的返回 error 被直接丢弃，管道关闭后全部日志静默丢失。
//
// 用只读 fd（目录 fd）模拟写坏端：write 返回 EBADF，且不会像对端关闭的
// 管道那样触发 EPIPE/SIGPIPE 杀进程。
func TestSlogLogger_StdoutWriteErrorReportedOnce(t *testing.T) {
	// 报告一次的标记是进程级：保存/恢复，保证 -count>1 重跑幂等
	prevReported := stdoutFailReported.Load()
	prevFailures := stdoutWriteFailures.Load()
	stdoutFailReported.Store(false)
	t.Cleanup(func() { stdoutFailReported.Store(prevReported) })

	dir, err := os.Open(".") // 只读 fd：写必失败
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()

	oldStdout := os.Stdout
	os.Stdout = dir

	// 捕获 stderr
	sr, sw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStderr := os.Stderr
	os.Stderr = sw

	l := NewSlogLogger("svc")
	l.Info("dropped-1")
	l.Info("dropped-2")

	os.Stderr = oldStderr
	os.Stdout = oldStdout
	sw.Close()
	errOut, _ := io.ReadAll(sr)

	if got := stdoutWriteFailures.Load() - prevFailures; got != 2 {
		t.Fatalf("失败计数 = %d, want 2", got)
	}
	if n := strings.Count(string(errOut), "stdout 写失败"); n != 1 {
		t.Fatalf("stderr 报告次数 = %d, want 1（只报一次避免风暴）\nstderr: %s", n, errOut)
	}
	if !strings.Contains(string(errOut), "dropped-1") {
		t.Fatalf("报告应附被丢弃的首条日志行，便于定位\nstderr: %s", errOut)
	}
}
