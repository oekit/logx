package logx_test

import (
	"testing"

	"github.com/oekit/logx"
)

// ErrKey 是两矩阵错误字段收敛的唯一 key（oekit#161），
// 值变更即破坏 Loki/ES 查询侧兼容，锁死。
func TestErrKeyValue(t *testing.T) {
	if logx.ErrKey != "err" {
		t.Fatalf("ErrKey = %q, want %q", logx.ErrKey, "err")
	}
}
