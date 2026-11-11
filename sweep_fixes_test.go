package logx

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// ---- #171：热路径免锁 + 并发语义 ----

// TestShouldLog_ConcurrentSetLevel 钉住 #171 的并发语义：Level 原子化后，
// 「多 goroutine 运行期调级 + 多 goroutine 热路径判级（ShouldLog/Enabled）+
// 派生快照」必须 -race 干净。修复前 ShouldLog 对 Mu 取锁、SetLevel 也取锁
// （互斥正确但争用）；原子化后若出现任何非原子路径（如整体结构体拷贝），
// 本用例在 -race 下必报。仅在 -race 下有检出力（CI 全量 -race）。
func TestShouldLog_ConcurrentSetLevel(t *testing.T) {
	sl := NewSlogLogger("race-171").(*slogLogger)
	var wg sync.WaitGroup
	const iters = 2000

	// 调级方 ×2
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			levels := [4]Level{LevelDebug, LevelInfo, LevelWarn, LevelError}
			for i := range iters {
				sl.SetLevel(levels[i%len(levels)])
			}
		}()
	}
	// 判级方 ×4（热路径形态：ShouldLog / Enabled / 结构化调用全链）
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range iters {
				_ = sl.ShouldLog(LevelDebug)
				_ = Enabled(sl, LevelInfo)
				sl.Info("concurrent emit")
			}
		}()
	}
	// 派生方 ×1（Snapshot 逐字段重建 vs 并发 SetLevel）
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range iters {
			_ = sl.WithAttrs(Attr{Key: "k", Value: "v"})
			_ = sl.WithService("sub")
		}
	}()

	wg.Wait()
}

// TestAtomicLevel_CopySemantics 钉住 AtomicLevel 的拷贝语义：值拷贝得到
// 「当下值的独立副本」，拷贝后父子互不影响（与 Snapshot/Derive 的派生语义
// 一致），且 Load/Store 各自正确。
func TestAtomicLevel_CopySemantics(t *testing.T) {
	a := newAtomicLevel(LevelWarn)
	b := a // 值拷贝（Snapshot/Derive 重建的语义等价形态）
	if b.Load() != LevelWarn {
		t.Fatalf("copy should carry current value, got %v", b.Load())
	}
	b.Store(LevelDebug)
	if a.Load() != LevelWarn {
		t.Fatalf("copy must be independent: parent=%v", a.Load())
	}
	lv := newAtomicLevel(LevelOff)
	if got := lv.String(); got != "OFF" {
		t.Fatalf("String() = %q, want OFF", got)
	}
}

// ---- #164：BaseLogger.Derive ----

// TestBaseLogger_Derive 钉住上提后的派生原语：apply 生效、逐字段重建
// （不含 Mutex）、派生级别独立。
func TestBaseLogger_Derive(t *testing.T) {
	base := NewBaseLogger("svc", LevelInfo)
	base.Attrs = []Attr{{Key: "a", Value: "1"}}
	base.Ctx = context.Background()

	got := base.Derive(func(cp *BaseLogger) {
		cp.Service = "sub"
		cp.Attrs = cp.MergeAttrs(Attr{Key: "b", Value: "2"})
	})
	if got.Service != "sub" || got.Level.Load() != LevelInfo {
		t.Fatalf("derive fields = %s/%v", got.Service, got.Level.Load())
	}
	if len(got.Attrs) != 2 {
		t.Fatalf("attrs = %v", got.Attrs)
	}
	if got.Ctx == nil {
		t.Fatal("ctx must carry over")
	}
	// 派生级别独立
	got.SetLevel(LevelError)
	if base.Level.Load() != LevelInfo {
		t.Fatalf("derived SetLevel must not affect parent: %v", base.Level.Load())
	}
	// 原字段不动
	if base.Service != "svc" || len(base.Attrs) != 1 {
		t.Fatalf("base mutated: %s %v", base.Service, base.Attrs)
	}
}

// ---- #167：std 后端函数选项形态 ----

// TestNewStdLoggerWith_LevelPriority 钉住 #167：WithStdLevel > LOG_LEVEL > Info，
// 与 NewSlogLogger 的文档口径一致；旧 variadic 形态行为不变。
func TestNewStdLoggerWith_LevelPriority(t *testing.T) {
	t.Setenv("LOG_LEVEL", "WARN")
	if got := NewStdLoggerWith("s").(*stdLogger).Level.Load(); got != LevelWarn {
		t.Fatalf("LOG_LEVEL should apply: got %v", got)
	}
	if got := NewStdLoggerWith("s", WithStdLevel(LevelDebug)).(*stdLogger).Level.Load(); got != LevelDebug {
		t.Fatalf("WithStdLevel should win over LOG_LEVEL: got %v", got)
	}
	t.Setenv("LOG_LEVEL", "")
	if got := NewStdLoggerWith("s").(*stdLogger).Level.Load(); got != LevelInfo {
		t.Fatalf("default should be Info: got %v", got)
	}
	// 旧 variadic 形态照常转发
	if got := NewStdLogger("s", LevelError).(*stdLogger).Level.Load(); got != LevelError {
		t.Fatalf("variadic form must keep working: got %v", got)
	}
}

// ---- #169：printf 路径在 FilterFunc 未配置时不做无谓格式化 ----

// lazyStr 的 String() 每被调用一次计数 +1：Sprintf 是否发生只能靠副作用观测。
type lazyStr struct{ calls *atomic.Int32 }

func (l lazyStr) String() string { l.calls.Add(1); return "lazy" }

// TestWithFilter_PrintfSkipsFormatWithoutFilterFunc 钉住 #169：只配 FilterKey
// （无 FilterFunc）时，Debugf/Infof/Ctx*f 不得执行 fmt.Sprintf（修复前每次
// 白付一次完整格式化，随后 base 再格式化第二次）；配置 FilterFunc 时丢弃
// 判定照常收到格式化结果。
func TestWithFilter_PrintfSkipsFormatWithoutFilterFunc(t *testing.T) {
	// 无 FilterFunc：只付 base 自己的一次格式化（#169 修复前是两次——
	// filter 的 dropped(fmt.Sprintf(...), nil) 实参先于 drop != nil 短路
	// 无条件求值，与 base 的格式化构成双重格式化）。
	var calls atomic.Int32
	base := NewSlogLogger("svc", WithHandler(newCaptureHandler()))
	f := WithFilter(base, FilterKey("password"))
	f.Infof("no-filter %s", lazyStr{&calls})
	if n := calls.Load(); n != 1 {
		t.Fatalf("FilterFunc 未配置时 printf 只应由 base 格式化一次，Sprintf 次数 = %d", n)
	}
	f.CtxErrorf(context.Background(), "ctx %s", lazyStr{&calls})
	if n := calls.Load(); n != 2 {
		t.Fatalf("Ctx*f 路径同样只付一次格式化，累计 = %d", n)
	}

	// 配 FilterFunc：判定收到格式化结果
	var dropped int32
	f2 := WithFilter(NewSlogLogger("svc", WithHandler(newCaptureHandler())),
		FilterFunc(func(msg string, kvs []any) bool {
			if strings.Contains(msg, "lazy") {
				atomic.AddInt32(&dropped, 1)
			}
			return false
		}))
	f2.Infof("with-filter %s", lazyStr{&calls})
	if n := calls.Load(); n != 4 { // filter 判定 1 次 + base 1 次（累计含前段 2 次）
		t.Fatalf("FilterFunc 配置时判定与 base 各格式化一次，累计 = %d", n)
	}
	if atomic.LoadInt32(&dropped) != 1 {
		t.Fatalf("FilterFunc 应收到格式化后的消息, 命中 %d 次", dropped)
	}
}

// ---- #172：NewAuditLogger 不得写穿调用方的选项切片 ----

// TestNewAuditLogger_DoesNotMutateCallerOpts 钉住 #172：调用方以 len<cap 的
// 共享视图传入 opts 时，哨兵 WithLevel 不得写进后备数组空洞——视图之外
// 的既有元素（shared[1]）必须原样保留。
func TestNewAuditLogger_DoesNotMutateCallerOpts(t *testing.T) {
	shared := make([]SlogOption, 2, 4)
	shared[0] = WithHandler(newCaptureHandler())
	shared[1] = WithLevel(LevelDebug) // 共享视图之外的元素

	_ = NewAuditLogger("svc", shared[:1]...) // 修复前把 WithLevel(Info) 写进 shared[1]

	probe := &slogLogger{}
	shared[1](probe) // 重新应用：shared[1] 必须仍是 WithLevel(LevelDebug)
	if probe.Level.Load() != LevelDebug {
		t.Fatalf("调用方选项被写穿：shared[1] 生效级别 = %v, want LevelDebug", probe.Level.Load())
	}
}

// ---- #170：内置 JSON 编码与 encoding/json 字节同构 ----

// TestAppendJSONString_MatchesEncodingJSON 用含转义边角的字符串语料钉住
// 手写编码器的字节兼容性（HTML 转义、控制字符、非法 UTF-8、U+2028/2029）。
func TestAppendJSONString_MatchesEncodingJSON(t *testing.T) {
	corpus := []string{
		"", "plain", `quote"inside`, `back\slash`,
		"html<b>&tag</b>", "ctrl\x01\x02", "tab\tnl\r\n",
		"unicode: 中文 ✓", "invalid utf8: \xff\xfe", "line sep \u2028 para \u2029",
		"del\x7f", strings.Repeat("x", 4096),
	}
	for _, s := range corpus {
		want, err := json.Marshal(s)
		if err != nil {
			t.Fatalf("corpus marshal: %v", err)
		}
		got := string(appendJSONString(nil, s))
		if got != string(want) {
			t.Fatalf("appendJSONString(%q) = %s, want %s", s, got, want)
		}
	}
}

// TestAppendJSONValue_ScalarByteParity 钉住标量快路径与 json.Marshal 的
// 字节同构（含 NaN 报错触发降级的口径）。
func TestAppendJSONValue_ScalarByteParity(t *testing.T) {
	values := []any{
		nil, true, false,
		int(500), int8(-8), int16(16), int32(-32), int64(1 << 40),
		uint(1), uint8(2), uint16(3), uint32(4), uint64(1 << 50),
		"string", LevelInfo, // 非 fast-path 的命名类型走 json.Marshal 回退
	}
	for _, v := range values {
		want, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal %v: %v", v, err)
		}
		got, err := appendJSONValue(nil, v)
		if err != nil {
			t.Fatalf("appendJSONValue(%v): %v", v, err)
		}
		if string(got) != string(want) {
			t.Fatalf("appendJSONValue(%#v) = %s, want %s", v, got, want)
		}
	}
	if _, err := appendJSONValue(nil, math.NaN()); err == nil {
		t.Fatal("NaN 必须报错以触发降级路径")
	}
}
