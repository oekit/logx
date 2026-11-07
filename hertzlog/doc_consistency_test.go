package hertzlog

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestGodocReferencesOnlyLiveAPIs（#105）：包内 godoc/注释不得引用已删除的
// API 名（旧名 AsHlogLogger/AsHlogLoggerFromXxx 已于 v0.1.7 删除，现行导出
// 为 AsHlogLoggerZap/Logrus/Slog/Zerolog）。此前 zap.go/zerolog.go 的导出
// 注释示例仍教用户调用已删除的名字——pkg.go.dev 直接可见，照抄即编译
// 失败，且与 aliases.go 的自述矛盾。
func TestGodocReferencesOnlyLiveAPIs(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(thisFile)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	// aliases.go 自述历史名（「旧名已删除」的声明本身合法），不参与扫描
	forbidden := []string{"AsHlogLogger(", "AsHlogLoggerFrom"}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "aliases.go" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		for _, pat := range forbidden {
			if strings.Contains(string(raw), pat) {
				t.Errorf("%s 引用已删除的 API（模式 %q）——照抄即编译失败", name, pat)
			}
		}
	}

	// 现行 API 必须存在（编译期之外再钉一次名字，防未来误删）
	var _ = AsHlogLoggerZap
	var _ = AsHlogLoggerLogrus
	var _ = AsHlogLoggerSlog
	var _ = AsHlogLoggerZerolog
}
