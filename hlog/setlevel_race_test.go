package hlog

import (
	"context"
	"sync"
	"testing"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/oekit/logx"
)

// TestHlogLogger_SetLevelConcurrentLogging（#96）：「一个 goroutine 运行期
// 调级 + 另一个 goroutine 打日志」这一生产常态，此前在 -race 下必报
// data race（hertz defaultLogger.level 是普通 int，写 SetLevel/读 logf
// 无任何同步）。包级 hlogMu 写锁（SetLevel/initHlogLevel）与全部 Ctx*
// 读锁互斥后必须干净。本用例仅在 -race 下有检出力（CI 全量 -race）。
func TestHlogLogger_SetLevelConcurrentLogging(t *testing.T) {
	var buf syncBuffer
	hlog.SetOutput(&buf)
	t.Cleanup(func() { hlog.SetOutput(nil) })

	l := NewHlogLogger("race")
	child := l.WithAttrs(logx.Attr{Key: "k", Value: "v"}) // 派生兄弟实例：包级锁必须覆盖跨实例竞态

	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := range 500 {
			lvl := logx.LevelDebug
			if i%2 == 0 {
				lvl = logx.LevelError
			}
			l.SetLevel(lvl)
		}
		close(stop)
	}()
	for range 4 {
		wg.Go(func() {
			ctx := context.Background()
			for {
				select {
				case <-stop:
					return
				default:
				}
				l.CtxInfo(ctx, "concurrent info")
				child.CtxErrorf(ctx, "concurrent error %s", "fmt")
			}
		})
	}
	wg.Wait()
}
