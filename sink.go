package logx

import (
	"os"
	"sync"
	"sync/atomic"

	"gopkg.in/natefinch/lumberjack.v2"
)

// Sink 是 logx 的输出目标抽象,业务可实现自定义 sink(文件、远程、
// Loki/HTTP/OTLP 等)。MultiSink/FileSink/RotatingSink 三个内置实现覆盖
// 常见生产场景,业务写自定义 sink 时也要实现这 3 个方法。
//
// 官方依据:https://www.cloudwego.io/zh/docs/hertz/tutorials/observability/log/
// "重定向默认 logger 的输出"段推荐 io.MultiWriter 多输出,本包把
// fanout / file rotation / 自定义扩展点统一为 Sink interface。
type Sink interface {
	// Write 实现 io.Writer 语义。失败不阻塞其他 sink(由 MultiSink 处理),
	// 单个 sink 内部应尽量保证写成功,失败时记录到 stderr 或 metrics。
	Write(p []byte) (n int, err error)
	// Sync 强制刷盘(对于 buffered writer,fsync 等)。
	// 对无缓冲的 sink(os.File) 是 no-op。
	Sync() error
	// Close 释放资源(关闭 fd,关闭网络连接)。
	// Close 之后 Write 应该返回 error。
	Close() error
}

// MultiSink 把多个 sink 合并成一个,fanout 到所有 sink。任何单个 sink
// 写入失败不影响其他 sink 的写入。
//
// 线程安全:Add/Close/Write 全部受 mu 保护,业务可并发调用。
type MultiSink struct {
	mu     sync.RWMutex
	sinks  []Sink
	closed atomic.Bool
}

// NewMultiSink 构造 multi sink,业务可传 0+ 个 sink(传 0 个返回
// nil,所有 Write 调用会被丢弃,适合 dry-run 测试场景)。
func NewMultiSink(sinks ...Sink) *MultiSink {
	return &MultiSink{sinks: append([]Sink{}, sinks...)}
}

// Add 动态添加 sink(运行时切换后端,如热加载到 Loki)。Closed 之后
// Add 返回 false。
func (m *MultiSink) Add(s Sink) bool {
	// closed 检查必须在锁内(与 Close 的 CAS+swap 互斥),否则存在
	// check-then-act 窗口:Add 通过检查后 Close 完成排空,append 到
	// 已置 nil 的 slice 并返回 true — sink 既不会被写也不会被 Close
	// (2026-09-05 复审修复)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed.Load() {
		return false
	}
	m.sinks = append(m.sinks, s)
	return true
}

// Write 实现 io.Writer:fanout 到所有 sink。任一 sink 写入失败仅 log
// 到 stderr(避免 sink 失败导致 logx panic),不返回 error(io.Writer
// 语义要求返回 n + err,但 MultiSink 把错误"吞掉"以保持其他 sink 可用)。
func (m *MultiSink) Write(p []byte) (int, error) {
	if m.closed.Load() {
		return 0, os.ErrClosed
	}
	m.mu.RLock()
	sinks := make([]Sink, len(m.sinks))
	copy(sinks, m.sinks)
	m.mu.RUnlock()
	for _, s := range sinks {
		if _, err := s.Write(p); err != nil {
			// 静默失败:用 stderr 提示,不让 sink 失败把整个 logx 拖垮
			_, _ = os.Stderr.WriteString("logx.MultiSink: sink write error: " + err.Error() + "\n")
		}
	}
	return len(p), nil
}

// Sync 刷新所有 sink 的缓冲区。
func (m *MultiSink) Sync() error {
	if m.closed.Load() {
		return os.ErrClosed
	}
	m.mu.RLock()
	sinks := make([]Sink, len(m.sinks))
	copy(sinks, m.sinks)
	m.mu.RUnlock()
	var firstErr error
	for _, s := range sinks {
		if err := s.Sync(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Close 关闭所有 sink(幂等,重复调用无副作用)。Close 之后 Write/Sync
// 全部返回 os.ErrClosed。
func (m *MultiSink) Close() error {
	if !m.closed.CompareAndSwap(false, true) {
		return nil
	}
	m.mu.Lock()
	sinks := m.sinks
	m.sinks = nil
	m.mu.Unlock()
	var firstErr error
	for _, s := range sinks {
		if err := s.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// FileSink 把日志写入单一文件(append 模式)。Close 后 Write 返回 error。
// 文件不存在会自动创建,父目录业务自行 mkdir -p。
//
// Write/Sync/Close 全部受 mu 保护:并发 Write 安全,Close 与 Write 并发
// 不会出现 nil deref 窗口(2026-09-05 复审修复)。
type FileSink struct {
	mu sync.Mutex
	f  *os.File
}

// NewFileSink 打开/创建 path 指向的文件,O_APPEND|O_CREATE|O_WRONLY 模式。
// 失败时返回 error(例如目录不存在 / 权限不足)。
func NewFileSink(path string) (*FileSink, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	return &FileSink{f: f}, nil
}

// Write 实现 Sink/IO.Writer。
func (s *FileSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return 0, os.ErrClosed
	}
	return s.f.Write(p)
}

// Sync 调 fdatasync 刷盘(os.File 自带)。
func (s *FileSink) Sync() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return os.ErrClosed
	}
	return s.f.Sync()
}

// Close 关闭文件,幂等。
func (s *FileSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return nil
	}
	err := s.f.Close()
	s.f = nil
	return err
}

// RotatingSink 按大小轮转日志文件,基于 natefinch/lumberjack (apache 2.0)。
// 文件命名:`<base>`(当前)+ `<base>.1` ~ `<base>.<maxBackups>`(历史)。
//
// 官方依据:natefinch/lumberjack 是 Go 生态最成熟的日志轮转库,
// 0 业务假设(只依赖 sync/atomic)。
//
// 用法:
//
//	rs, _ := NewRotatingSink("./logs/app.log", 100 /* MB */,
//	    WithMaxBackups(7), WithMaxAge(30), WithCompress(true))
//	defer rs.Close()
type RotatingSink struct {
	*lumberjack.Logger
	closed atomic.Bool
	mu     sync.Mutex
}

// RotatingOption 配置 RotatingSink。
type RotatingOption func(*lumberjack.Logger)

// WithMaxBackups 设置保留历史日志文件数(默认 0 = 全部保留)。
func WithMaxBackups(n int) RotatingOption {
	return func(l *lumberjack.Logger) { l.MaxBackups = n }
}

// WithMaxAge 设置历史日志保留天数(默认 0 = 不过期)。
func WithMaxAge(days int) RotatingOption {
	return func(l *lumberjack.Logger) { l.MaxAge = days }
}

// WithCompress 启用 gzip 压缩历史日志(默认 false)。
func WithCompress(b bool) RotatingOption {
	return func(l *lumberjack.Logger) { l.Compress = b }
}

// WithLocalTime 用本地时间戳文件名(默认 UTC)。
func WithLocalTime(b bool) RotatingOption {
	return func(l *lumberjack.Logger) { l.LocalTime = b }
}

// NewRotatingSink 构造轮转 sink,basePath 是当前文件路径,maxSizeMB 是
// 单文件最大字节(MB 单位,lumberjack 内部用 byte 存,这里转 MB 让业务
// 不用自己乘 1024*1024)。
func NewRotatingSink(basePath string, maxSizeMB int, opts ...RotatingOption) (*RotatingSink, error) {
	l := &lumberjack.Logger{
		Filename: basePath,
		MaxSize:  maxSizeMB, // lumberjack.MaxSize 单位是 MB
	}
	for _, opt := range opts {
		opt(l)
	}
	return &RotatingSink{Logger: l}, nil
}

// Write 覆盖 lumberjack 提升方法:Close 之后返回 os.ErrClosed,遵守 Sink
// interface 契约("Close 之后 Write 应该返回 error",见本包 Sink 文档)。
// 2026-09-05 复审修复:原实现对 nil Logger 直接 deref,Close 后 Write 必 panic;
// v0.25.14 复审二次修正:仅靠 closed 标志挡不住"读到 false 后、再次读
// Logger 字段前 Close 完成置 nil"的窗口(字段读写无同步,data race +
// nil-deref),Write/Sync/Close 与 FileSink 同款全走 mu 串行化。
func (s *RotatingSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed.Load() || s.Logger == nil {
		return 0, os.ErrClosed
	}
	return s.Logger.Write(p)
}

// Sync 触发 lumberjack rotate 检查(lumberjack rotate-on-write,无显式
// fsync)。Closed 之后返回 os.ErrClosed。
func (s *RotatingSink) Sync() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed.Load() || s.Logger == nil {
		return os.ErrClosed
	}
	return nil
}

// Close 关闭 lumberjack logger(幂等)。Close 之后 Write/Sync 返回 os.ErrClosed。
func (s *RotatingSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed.Load() || s.Logger == nil {
		return nil
	}
	s.closed.Store(true)
	err := s.Logger.Close()
	s.Logger = nil
	return err
}
