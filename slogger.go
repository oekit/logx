package logx

import (
	"context"
	"log/slog"
	"time"
)

// SlogHandler 把 logx.Logger 暴露为 slog.Handler——与 slog.go 中
// slog 后端（slog.Handler → logx 输出）方向相反的桥：让期望 *slog.Logger
// 的三方库直接消费 logx 生态（含 Delegating/过滤/子 logger 能力）。
//
// 记录经 logx 的 Ctx* 方法下发（保留 ctx 以便 trace_id 注入）；后端是 slog
// 后端时经 slogBridgeSink 直通（复用 r.PC/r.Time，不栈回溯，#84）；级别
// 映射按阈值制（见 logxLevel）；WithGroup 实现为键前缀（"group.key"），且
// 只限定其后的属性（#192：WithAttrs 在挂载时刻固化当时的组前缀，先于
// WithGroup 的属性留在组外，与 stdlib slog.Handler 契约对拍一致）。
func SlogHandler(l Logger) slog.Handler {
	if l == nil {
		panic("logx: SlogHandler called with nil logger")
	}
	return &slogxHandler{l: l}
}

// slogBridgeSink 由支持接收 slog 侧已解析调用点/时间戳的后端实现（#84）。
// 桥接路径优先走它：直接用 r.PC / r.Time 构造下游输出，不再栈回溯——
// 回溯的第一个非 logx 帧是 stdlib log/slog.(*Logger).log，caller 会恒错标
// 到 log/slog/logger.go 源码行。目前仅 slog 后端（*slogLogger）实现；
// 其余后端走 Handle 的通用 kv 拍平回退（caller 由后端自行解析或省略）。
type slogBridgeSink interface {
	logxBridge(ctx context.Context, level Level, pc uintptr, ts time.Time, msg string, kv ...any)
}

type slogxHandler struct {
	l     Logger
	attrs []slog.Attr // WithAttrs 累积的属性——键已按**挂载时刻**的组前缀固化（#192）
	group string      // WithGroup 累积的键前缀（不含尾部点），只限定其后的 Record attrs
}

func (h *slogxHandler) Enabled(_ context.Context, level slog.Level) bool {
	return Enabled(h.l, logxLevel(level))
}

func (h *slogxHandler) Handle(ctx context.Context, r slog.Record) error {
	kvs := make([]any, 0, (len(h.attrs)+r.NumAttrs())*2)
	// WithAttrs 累积属性：前缀已在挂载时刻固化（#192），不再叠加当前组——
	// 此前输出时刻统一回填最终前缀，WithAttrs 先于 WithGroup 的属性被错误
	// 卷进组，违反 slog「subsequent attributes」契约。
	for _, a := range h.attrs {
		kvs = append(kvs, a.Key, a.Value.Any())
	}
	// Record attrs：Handle 时刻组即当前组，按契约施加前缀。
	prefix := ""
	if h.group != "" {
		prefix = h.group + "."
	}
	r.Attrs(func(a slog.Attr) bool {
		if a.Equal(slog.Attr{}) {
			return true
		}
		kvs = append(kvs, prefix+a.Key, a.Value.Any())
		return true
	})

	// 快路径（#84）：后端支持直通时复用 r.PC / r.Time，不再栈回溯。
	if sink, ok := h.l.(slogBridgeSink); ok {
		sink.logxBridge(ctx, logxLevel(r.Level), r.PC, r.Time, r.Message, kvs...)
		return nil
	}

	// 阈值制级别映射（#84）：比 ERROR 更重的自定义级别（如 slog.LevelError+2）
	// 必须落 Error——此前四档名称精确匹配 + default 落 Info，此类记录被降为
	// INFO，logx 设 WARN 档时整条吞掉（致命日志消失且标签错标）。
	switch logxLevel(r.Level) {
	case LevelDebug:
		h.l.CtxDebug(ctx, r.Message, kvs...)
	case LevelWarn:
		h.l.CtxWarn(ctx, r.Message, kvs...)
	case LevelError:
		h.l.CtxError(ctx, r.Message, kvs...)
	default:
		h.l.CtxInfo(ctx, r.Message, kvs...)
	}
	return nil
}

func (h *slogxHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	// #192：stdlib 口径——WithAttrs 落盘时即用**当时**的组前缀预格式化
	// （对齐 GOROOT log/slog commonHandler.withAttrs 的 state.prefix 固化），
	// 顺序信息自此保留在键里，后续 WithGroup 不再回溯改写先挂属性。
	next := &slogxHandler{l: h.l, group: h.group}
	prefix := ""
	if h.group != "" {
		prefix = h.group + "."
	}
	next.attrs = make([]slog.Attr, 0, len(h.attrs)+len(attrs))
	next.attrs = append(next.attrs, h.attrs...)
	for _, a := range attrs {
		if a.Equal(slog.Attr{}) {
			continue // 空属性丢弃（stdlib withAttrs 同口径）
		}
		a.Key = prefix + a.Key
		next.attrs = append(next.attrs, a)
	}
	return next
}

func (h *slogxHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	next := &slogxHandler{l: h.l, attrs: h.attrs}
	if h.group != "" {
		next.group = h.group + "." + name
	} else {
		next.group = name
	}
	return next
}

// logxLevel 将 slog.Level 映射为 logx.Level（阈值制，#84）。
//
// slog 支持自定义级别（slog.LevelError+2 语义为比 ERROR 更严重），按四档
// 名称精确匹配会让任何非四档级别降为 Info。阈值制保证严重性单调不减：
//
//	>= ERROR → Error；>= WARN → Warn；>= INFO → Info；更低（含 Debug-N）→ Debug
func logxLevel(level slog.Level) Level {
	switch {
	case level >= slog.LevelError:
		return LevelError
	case level >= slog.LevelWarn:
		return LevelWarn
	case level >= slog.LevelInfo:
		return LevelInfo
	default:
		return LevelDebug
	}
}

// Slogger 返回以 l 为后端的 *slog.Logger。
func Slogger(l Logger) *slog.Logger {
	return slog.New(SlogHandler(l))
}
