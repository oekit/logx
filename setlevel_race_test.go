package logx

import (
	"context"
	"sync"
	"testing"
)

// TestSetLevel_ConcurrentClone 回归：SetLevel 持 Mu 写 Level，但 clone/With*
// 派生路径曾无锁读 Level——并发请求路径 + 动态调级在 -race 下稳定检出
// 数据竞争。钉住：派生经 Snapshot 锁内取值。
func TestSetLevel_ConcurrentClone(t *testing.T) {
	l := NewSlogLogger("race-test")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			lvl := Level(n % 4)
			for j := 0; j < 200; j++ {
				l.SetLevel(lvl)
				_ = l.WithAttrs(Attr{Key: "k", Value: j})
				_ = l.WithCtx(context.Background())
				_ = l.WithService("sub")
			}
		}(i)
	}
	wg.Wait()
}
