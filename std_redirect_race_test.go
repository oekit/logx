package logx

import (
	"bytes"
	"log"
	"strings"
	"sync"
	"testing"
)

// #111 回归：logx std 后端与 stdlib log.Printf 并发写同一 log.SetOutput
// 重定向目标。bytes.Buffer 非线程安全——修复前 logx 行持 stdWriter.mu、
// stdlib 行持 log.std.mu，两把锁互不排斥，-race 必报 DATA RACE（issue
// 复现探针原样：8 goroutine × 50 条 logx Info + 主 goroutine 50 条
// log.Printf）。修复后 stdLogDynamicWriter 借默认 logger 的锁完成委托写，
// 双方对同一目标全互斥；本测试在 -race 下红转绿即为判据，并做行数对账
// （8*50+50 行全部落进重定向目标，一行不丢）。
func TestStdBackend_ConcurrentWithStdlibRedirect(t *testing.T) {
	logMu.Lock()
	t.Cleanup(logMu.Unlock)

	var buf bytes.Buffer
	prevW := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prevW); log.SetFlags(prevFlags) })

	lg := NewStdLogger("race-probe", LevelInfo)
	const gN = 8
	const perG = 50
	var wg sync.WaitGroup
	for range gN {
		wg.Go(func() {
			for j := range perG {
				lg.Info("hello", "k", j)
			}
		})
	}
	for j := range perG {
		log.Printf("stdlib line %d", j)
	}
	wg.Wait()

	out := buf.String()
	logxLines := strings.Count(out, "hello k=")
	stdLines := strings.Count(out, "stdlib line ")
	if logxLines != gN*perG || stdLines != perG {
		t.Fatalf("行数对账失败：logx=%d（want %d）stdlib=%d（want %d）\n%s",
			logxLines, gN*perG, stdLines, perG, out)
	}
}

// #111 契约钉：log.SetOutput 捕获形态下（只重定向 std log，不碰 stdWriter）
// ① logx std 行照常落进重定向目标（v0.8.6 拦截契约不回退）；
// ② logx 行保持 flags=0 单毫秒时间戳——借道 log.Output 的窗口钉零生效，
//
//	默认 logger 的 LstdFlags（斜杠秒级前缀 "2006/01/02 15:04:05 "）不再叠上；
//
// ③ 同一目标的 stdlib 行照常落盘（互斥不吞行）。
func TestStdBackend_RedirectCaptureSingleTimestamp(t *testing.T) {
	logMu.Lock()
	t.Cleanup(logMu.Unlock)

	var buf bytes.Buffer
	prevW := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prevW); log.SetFlags(prevFlags) })

	NewStdLogger("cap-svc", LevelInfo).Info("captured", "k", 1)
	log.Printf("stdlib captured")

	out := buf.String()
	if !strings.Contains(out, "captured k=1") {
		t.Fatalf("log.SetOutput 应能拦截 std 后端输出（v0.8.6 契约）, got: %q", out)
	}
	if !strings.Contains(out, "stdlib captured") {
		t.Fatalf("stdlib 行应照常落进同一目标, got: %q", out)
	}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if !strings.Contains(line, "[INFO] cap-svc") {
			continue
		}
		// LstdFlags 前缀形态是斜杠日期 "2026/11/08 22:53:19 "；logx 自带
		// 横杠毫秒时间戳 "2026-11-08 22:53:19.569"。logx 行内出现斜杠
		// 日期 ⇒ 默认 logger 的 flags 没钉住 ⇒ 双时间戳回归（#78 老毛病）。
		if strings.Contains(line, "/") {
			t.Fatalf("logx 行被叠了 stdlib 时间戳前缀（flags 钉零失效）: %q", line)
		}
		if !strings.Contains(line, "[INFO] cap-svc captured k=1") {
			t.Fatalf("logx 行内容残缺: %q", line)
		}
	}
}
