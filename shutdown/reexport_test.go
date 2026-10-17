package shutdown_test

import (
	"context"
	"testing"
	"time"

	"github.com/oekit/logx/shutdown"
)

// TestReexport_Smoke 验证兼容层能构造 Manager 并完成一次 Shutdown。
func TestReexport_Smoke(t *testing.T) {
	mgr := shutdown.New(time.Second)
	var closed bool
	mgr.Add("res", func(context.Context) error {
		closed = true
		return nil
	}, 0)
	mgr.Shutdown(context.Background())
	if !closed {
		t.Fatal("closer should have run via re-exported Manager")
	}
}
