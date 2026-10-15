// Package shutdown 提供优雅关闭管理器。
//
// 注册需要清理的资源，在收到 SIGINT/SIGTERM 信号时按序关闭。
//
// # 快速接入
//
// 创建管理器：
//
//	mgr := shutdown.New(10 * time.Second)
//
// 注册资源：
//
//	mgr.Add("redis", func(ctx context.Context) error { return rdb.Close() }, 3*time.Second)
//	mgr.Add("mysql", func(ctx context.Context) error { return sqlDB.Close() }, 5*time.Second)
//	mgr.AddCloser("nats", natsClient, 3*time.Second)
//
// 监听信号：
//
//	mgr.Listen(syscall.SIGINT, syscall.SIGTERM)
//
// 或手动触发：
//
//	mgr.Shutdown(context.Background())
//
// # 注册方式
//
//   - Add: 注册带 context 和 error 返回的清理函数
//   - AddCloser: 注册 io.Closer 资源
//   - AddTeardown: 注册无参数的清理函数（不接受 context，不返回 error）
//
// # 执行顺序
//
//  1. 先执行所有 AddTeardown 注册的函数
//  2. 再按注册顺序执行 Add/AddCloser 注册的任务
//  3. 每个任务有独立的超时控制
//  4. 多次调用 Shutdown 是安全的（sync.Once 保证）
//
// # 与 bootstrap 集成
//
// bootstrap 包内部使用 shutdown.Manager 管理资源关闭：
//
//	bootstrap.Run(bootstrap.Config{...}, func(app *bootstrap.App) error {
//	    app.AddResource("hertz", func(ctx context.Context) error { return h.Shutdown(ctx) })
//	    // ...
//	})
package shutdown
