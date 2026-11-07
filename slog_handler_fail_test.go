package logx

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

// failingHandler 是恒错的注入 Handler：Handle 一律返回 error，
// 模拟注入端写坏（磁盘满 / 下游阻塞 / 自定义 Handler bug）。
type failingHandler struct{ calls atomic.Int64 }

func (h *failingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *failingHandler) Handle(_ context.Context, _ slog.Record) error {
	h.calls.Add(1)
	return errors.New("disk on fire")
}
func (h *failingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *failingHandler) WithGroup(string) slog.Handler      { return h }

// TestSlogHandler_HandleErrorCountedReportedOnce（#98）：注入 Handler 路径
// 不得吞掉 slog.Handler.Handle 的 error——此前 `_ = h.Handle(...)` 使记录
// 彻底丢失且无任何失败计数，与内置路径「绝不静默丢弃」的契约分裂。
// 钉住：计数累加、stderr 一次性报告、logx.Info 正常返回（不 panic）。
func TestSlogHandler_HandleErrorCountedReportedOnce(t *testing.T) {
	// 进程级计数/一次性标记：保存/恢复，保证重跑幂等
	prevReported := handlerFailReported.Load()
	prevFailures := handlerWriteFailures.Load()
	handlerFailReported.Store(false)
	t.Cleanup(func() {
		handlerWriteFailures.Store(prevFailures)
		handlerFailReported.Store(prevReported)
	})

	// 捕获 stderr
	sr, sw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStderr := os.Stderr
	os.Stderr = sw

	h := &failingHandler{}
	l := NewSlogLogger("svc", WithHandler(h))
	l.Info("dropped-by-handler-1") // 必须正常返回，不 panic
	l.Error("dropped-by-handler-2")

	os.Stderr = oldStderr
	sw.Close()
	errOut, _ := io.ReadAll(sr)

	if got := handlerWriteFailures.Load() - prevFailures; got != 2 {
		t.Fatalf("HandlerWriteFailures 增量 = %d, want 2", got)
	}
	if HandlerWriteFailures() < 2 {
		t.Fatalf("HandlerWriteFailures() 访问器未反映累计值")
	}
	if h.calls.Load() != 2 {
		t.Fatalf("Handle 调用次数 = %d, want 2（日志仍在下发）", h.calls.Load())
	}
	if n := strings.Count(string(errOut), "Handle 失败"); n != 1 {
		t.Fatalf("stderr 报告次数 = %d, want 1（只报一次避免风暴）\nstderr: %s", n, errOut)
	}
	if !strings.Contains(string(errOut), "disk on fire") {
		t.Fatalf("报告应附失败原因\nstderr: %s", errOut)
	}
	if !strings.Contains(string(errOut), "dropped-by-handler-1") {
		t.Fatalf("报告应附被丢弃的首条日志 message，便于定位\nstderr: %s", errOut)
	}
}
