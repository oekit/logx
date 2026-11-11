package logx

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
)

// 本文件承载 slog 后端的调用方栈回溯与内部帧判定（#166 自 slog.go 按关注点
// 拆出）：内置 JSON 路径的 caller 字段、注入 Handler 路径的 slog.Record.PC、
// 以及桥接适配器（kitexlog 等）的内部帧注册表都在这里。

// callerSkipFiles 存放需跳过 caller 归属的源文件绝对路径（由 SkipCallerFiles 写入）。
// nil 表示无额外跳过项。
var callerSkipFiles atomic.Pointer[[]string]

// callerScanProbe 仅测试构建里被赋值（同包测试直接注入）：每次进入全栈
// 回溯探测 realCallerOutsideLogx 前调用，供测试断言「桥接快路径不再回溯」
// （#130——该缺陷的值语义全对，只有观测探测次数才能钉住）。生产构建恒
// nil，成本是慢路径上一次可预测分支；触发点本身在数百 ns 的回溯路径上，
// 不在桥接快路径。
var callerScanProbe func()

// SkipCallerFiles 将指定源文件标记为日志内部帧：slog 后端归属 caller 字段时
// 跳过这些文件中的调用方（重复注册同一文件自动去重，并发安全）。
//
// 用途：klog/hlog 桥接适配器位于 logx 之外，若不跳过，框架日志的 caller 会
// 错误归属到适配器文件（如 kitex/log.go）而非真实调用方——即 Kitex 官方日志
// 文档警告的"封装 klog 导致文件名行数不准确"问题。适配器应在构造函数中以
// runtime.Caller(0) 注册自身文件；其他代码不应调用本函数。
func SkipCallerFiles(files ...string) {
	if len(files) == 0 {
		return
	}
	for {
		old := callerSkipFiles.Load()
		merged := mergeSkipFiles(old, files)
		if callerSkipFiles.CompareAndSwap(old, merged) {
			return
		}
	}
}

func mergeSkipFiles(old *[]string, add []string) *[]string {
	merged := make([]string, 0, len(add)+4)
	seen := make(map[string]struct{}, len(add)+4)
	if old != nil {
		for _, f := range *old {
			if _, dup := seen[f]; !dup {
				seen[f] = struct{}{}
				merged = append(merged, f)
			}
		}
	}
	for _, f := range add {
		if _, dup := seen[f]; !dup {
			seen[f] = struct{}{}
			merged = append(merged, f)
		}
	}
	return &merged
}

// callerOutsideLogx 从调用栈中找到第一个不属于日志内部帧的调用方。
// 返回 "file:line" 格式，用于结构化日志的 caller 字段。
// 内部帧包括：logx 包自身、stdlib log/slog 桥接内部帧、SkipCallerFiles
// 注册的桥接适配器文件。
//
// 未命中（首个外部帧之前栈耗尽，或全部帧都是日志机制内部帧）时返回
// 空串、caller 字段省略。探测为单遍全栈扫描，无帧数上限（#82）：
// 深调用栈（多层中间件 + 包装 logger）不会静默丢 caller。
//
// 不缓存 skip 深度：不同调用路径的栈深不同（Logger 方法直调 vs 包级
// logx.Info vs Delegating 包装），全局缓存会让先到的路径污染后到的，
// 把 caller 错误归属到无关文件。
func callerOutsideLogx() string {
	_, caller := realCallerOutsideLogx()
	return caller
}

// realCallerOutsideLogx 返回真实调用方的 PC 与 "file:line"。
// PC 供 slog.Record 使用（Handler 的 AddSource 依赖它），字符串供 JSON 路径的
// caller 字段使用。判定规则同 callerOutsideLogx。
//
// 实现（#87）：一次 runtime.Callers + runtime.CallersFrames 单遍过滤——
// 此前 skip=2..11 串行 runtime.Caller，每次调用独立 O(depth) 重走整栈，
// 整体 O(k²)：深栈多层包装下探测成本与整条 json.Marshal 同量级
// （实测 465-1515ns/op，随栈深恶化）。单遍后无 12 帧上限（#82）：
// 栈深超过初始缓冲（64 帧）时扩容重取一次。
func realCallerOutsideLogx() (uintptr, string) {
	if callerScanProbe != nil {
		callerScanProbe() // 仅测试构建非 nil（#130 回归钉：快路径不得进入本函数）
	}
	var buf [64]uintptr
	n := runtime.Callers(3, buf[:])
	pcs := buf[:n]
	if n == len(buf) {
		// 栈比初始缓冲深（深递归/多层包装，罕见）：扩容重取，保证不因
		// 缓冲边界漏掉真实调用方帧（#82：此前上限 12 帧时静默丢 caller）。
		grown := make([]uintptr, 4*len(buf))
		if n2 := runtime.Callers(3, grown); n2 > 0 {
			pcs = grown[:n2]
		}
	}
	skipFiles := callerSkipFiles.Load()
	frames := runtime.CallersFrames(pcs)
	for {
		frame, more := frames.Next()
		if frame.PC == 0 && frame.File == "" {
			break // 帧耗尽仍未命中
		}
		if !frameInsideLogx(frame.Function, frame.File, skipFiles) {
			return frame.PC, fmt.Sprintf("%s:%d", filepath.Base(frame.File), frame.Line)
		}
		if !more {
			break
		}
	}
	return 0, ""
}

// frameInsideLogx 判定一帧是否日志机制内部帧（探测时跳过）：
//
//   - logx 包自身：按函数名里的 module 路径前缀匹配——根包、hlog/hertzlog
//     子包、以及内/外测试包。internal/ 下的测试探针（logxcallertest）
//     不属于实现，必须穿透。改 module 路径时必须同步这里。
//   - stdlib log/slog 内部帧（如 (*Logger).log，#84）：经 SlogHandler 桥接的
//     包装链（Delegating/audit 等，未走 PC 直通）回溯时，这些帧不是业务
//     调用方，不跳过会把 caller 恒错标到 log/slog/logger.go 源码行。
//   - SkipCallerFiles 注册的桥接适配器文件。
func frameInsideLogx(fnName, file string, skipFiles *[]string) bool {
	if i := strings.Index(fnName, "oekit/logx"); i >= 0 {
		rest := fnName[i+len("oekit/logx"):]
		if rest == "" || rest[0] == '.' || rest[0] == '_' ||
			strings.HasPrefix(rest, "/hlog") || strings.HasPrefix(rest, "/hertzlog") {
			return true
		}
	}
	if strings.HasPrefix(fnName, "log/slog.") {
		return true
	}
	return isSkipFile(skipFiles, file)
}

func isSkipFile(skipFiles *[]string, file string) bool {
	if skipFiles == nil {
		return false
	}
	for _, f := range *skipFiles {
		if f == file {
			return true
		}
	}
	return false
}

// pcCaller 将单个 PC 解析为 "file:line"（与 realCallerOutsideLogx 的字符串
// 口径一致）。桥接路径复用 slog 已解析好的调用点（r.PC）时使用。
func pcCaller(pc uintptr) string {
	if pc == 0 {
		return ""
	}
	frames := runtime.CallersFrames([]uintptr{pc})
	f, _ := frames.Next()
	if f.File == "" {
		return ""
	}
	return fmt.Sprintf("%s:%d", filepath.Base(f.File), f.Line)
}
