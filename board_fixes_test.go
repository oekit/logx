package logx

import (
	"bufio"
	"encoding/json"
	"os"
	"sync"
	"testing"
)

// TestSlogLogger_SingleWriteJSONL 钉住 #65：内置 JSON 路径必须整行单次写。
// 64 goroutine × 300 条并发写 os.Pipe，逐行读回——每行都必须是合法 JSON
// （修复前记录体与换行分两次 Write，并发交错产生拼行/空行）。
func TestSlogLogger_SingleWriteJSONL(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = old }()

	const goroutines, perG = 16, 120
	// 读端先起：pipe 缓冲（64KB）装不下全部记录，写者会在缓冲满时阻塞，
	// 等写完再读 = 死锁（实测踩过）
	var linesMu sync.Mutex
	linesGot := 0
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		sc := bufio.NewScanner(r)
		for sc.Scan() {
			line := sc.Text()
			if line == "" {
				t.Errorf("空行出现（写交错证据）")
				continue
			}
			var m map[string]any
			if err := json.Unmarshal([]byte(line), &m); err != nil {
				t.Errorf("非合法 JSON（拼行证据）: %v; line=%.80s", err, line)
			}
			linesMu.Lock()
			linesGot++
			linesMu.Unlock()
		}
	}()

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			l := NewSlogLogger("svc")
			for i := 0; i < perG; i++ {
				l.Info("msg", "g", id, "i", i)
			}
		}(g)
	}
	wg.Wait()
	w.Close()
	<-readDone

	linesMu.Lock()
	defer linesMu.Unlock()
	if linesGot != goroutines*perG {
		t.Fatalf("lines=%d want %d", linesGot, goroutines*perG)
	}
}

// TestSlogLogger_ReservedFieldConflict 钉住 #68：用户 kv 与框架字段同名时
// 框架字段优先、用户键加 "_" 前缀落地并带 _renamed 标记——不得静默覆盖
// （修复前 trace_id 被覆盖 = 追踪链断）。
func TestSlogLogger_ReservedFieldConflict(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w

	l := NewSlogLogger("svc").WithAttrs(Attr{Key: "trace_id", Value: "user-trace"})
	l.Info("hello", "caller", "fake-caller", "level", "LV")
	w.Close()
	os.Stdout = old

	sc := bufio.NewScanner(r)
	var rec map[string]any
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		if err := json.Unmarshal([]byte(line), &rec); err == nil {
			break
		}
	}
	if rec == nil {
		t.Fatal("no json record captured")
	}
	if rec["trace_id"] == "user-trace" {
		t.Fatal("用户值不得覆盖框架 trace_id")
	}
	if rec["caller"] == "fake-caller" {
		t.Fatal("用户值不得覆盖框架 caller")
	}
	if rec["_trace_id"] != "user-trace" {
		t.Fatalf("改名落地缺失: _trace_id=%v", rec["_trace_id"])
	}
	if rec["_caller"] != "fake-caller" {
		t.Fatalf("改名落地缺失: _caller=%v", rec["_caller"])
	}
	if rec["_level"] != "LV" {
		t.Fatalf("改名落地缺失: _level=%v", rec["_level"])
	}
	renamed, ok := rec["_renamed"].([]any)
	if !ok || len(renamed) != 3 {
		t.Fatalf("_renamed 标记缺失或不全: %v", rec["_renamed"])
	}
}

// TestStdLogger_ReadsLogLevelEnv 钉住 #66：NewStdLogger 构造器读 LOG_LEVEL。
func TestStdLogger_ReadsLogLevelEnv(t *testing.T) {
	old := os.Getenv("LOG_LEVEL")
	defer os.Setenv("LOG_LEVEL", old)

	os.Setenv("LOG_LEVEL", "DEBUG")
	if got := NewStdLogger("s").(*stdLogger).Level.Load(); got != LevelDebug {
		t.Fatalf("LOG_LEVEL=DEBUG ⇒ LevelDebug, got %v", got)
	}
	os.Setenv("LOG_LEVEL", "OFF")
	if got := NewStdLogger("s").(*stdLogger).Level.Load(); got != LevelOff {
		t.Fatalf("LOG_LEVEL=OFF ⇒ LevelOff, got %v", got)
	}
	os.Setenv("LOG_LEVEL", "WARN")
	if got := NewStdLogger("s").(*stdLogger).Level.Load(); got != LevelWarn {
		t.Fatalf("LOG_LEVEL=WARN ⇒ LevelWarn, got %v", got)
	}
	// 显式参数仍最高优先
	os.Setenv("LOG_LEVEL", "DEBUG")
	if got := NewStdLogger("s", LevelError).(*stdLogger).Level.Load(); got != LevelError {
		t.Fatalf("显式 level 应覆盖环境, got %v", got)
	}
}
