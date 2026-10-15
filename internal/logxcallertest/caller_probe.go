// Package logxcallertest 为 logx 的 caller 归属测试提供真实的包外调用帧。
//
// logx 目录内的测试文件（含 _test 外部测试包）的符号名都含
// "/observability/logx"，会被 callerOutsideLogx 的包名启发式当作内部帧
// 跳过，无法构造"外部调用方"场景；本包位于 logx 之外，帧可被正常归属。
// 仅供 logx 测试使用。
package logxcallertest

import (
	"encoding/json"
	"io"
	"os"
	"runtime"
	"strings"

	"github.com/openekit/logx"
)

// File 返回本文件路径，供测试注册为 caller 跳过项。
func File() string {
	_, file, _, _ := runtime.Caller(0)
	return file
}

// EmitCaller 输出一条经 slog 后端的日志，返回记录到的 caller 字段。
// 调用方帧（本函数）即 callerOutsideLogx 应归属的第一帧。
func EmitCaller() string {
	r, w, err := os.Pipe()
	if err != nil {
		return "pipe error: " + err.Error()
	}
	old := os.Stdout
	os.Stdout = w
	logx.NewSlogLogger("svc").Info("caller probe")
	os.Stdout = old
	w.Close()
	out, _ := io.ReadAll(r)

	var rec map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &rec); err != nil {
		return "parse error: " + err.Error()
	}
	c, _ := rec["caller"].(string)
	return c
}
