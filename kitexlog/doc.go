// Package kitexlog 提供将 Kitex klog 接管到 logx 的桥接适配器。
//
// 本包把 [logx.Logger] 适配为 Kitex [klog.FullLogger]，使 Kitex 框架日志
// 统一走 logx 门面（结构化输出、级别控制、caller 归属、trace_id 注入）。
// 自 oekit/kitex（kitex.NewLogger）整体迁入，行为不变。
//
// 注意：本子包携带 github.com/cloudwego/kitex 依赖（按需引入），
// logx 根包仍保持零框架依赖。
//
// 用法:
//
//	import (
//	    "github.com/cloudwego/kitex/pkg/klog"
//	    "github.com/oekit/logx"
//	    logxkitexlog "github.com/oekit/logx/kitexlog"
//	)
//
//	klog.SetLogger(kitexlog.NewLogger(logx.NewSlogLogger("my-rpc")))
//
// 注意: 将 hlog 后端的 Logger 传入 NewLogger 会导致无限递归
// （adapter → logx → hlog → adapter），NewLogger 会在检测到 hlog 后端时
// panic 防御；此场景应使用 logx.NewSlogLogger 或 logx.UseSlog。
package kitexlog
