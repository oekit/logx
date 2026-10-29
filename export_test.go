package logx

// 本文件是 Go 官方的 export_test.go 模式：把**仅测试期**需要的内部符号
// 导出给同包的外部测试包（package logx_test）使用。
//
// **为什么需要**：caller 跳过注册表（callerSkipFiles）是**进程级全局**，
// 而 SkipCallerFiles 只增不删（设计上就是"注册持久化到进程结束"）。
// 外部测试包要测端到端 caller 归属，就必须能**读出旧值并恢复**——
// 否则测试自己注册的探针文件会**留在全局里**：
//
//	第一次跑：未注册 ⇒ baseline 命中探针帧 ✓
//	第二次跑（-count=2 同一进程内重复）：已注册 ⇒ 探针帧被跳过
//	          ⇒ baseline 落到 testing 框架帧 ⇒ 用例红
//
// ⇒ **对 -count>1 不幂等**。CI 用 -count=1 所以不红，但任何本地重复跑
// （-count>1 / -race 压测）都会红，且注册表会泄漏给同批次的其它用例。
//
// 有了这两个钩子，外部测试包就能在 t.Cleanup 里恢复注册表 ⇒ 幂等 +
// 不泄漏。**这两个函数只在测试构建里存在**（文件名以 _test.go 结尾），
// 不进任何发布产物。

// CallerSkipFilesForTest 返回当前 caller 跳过注册表的副本（测试用）。
func CallerSkipFilesForTest() []string {
	old := callerSkipFiles.Load()
	if old == nil {
		return nil
	}
	out := make([]string, len(*old))
	copy(out, *old)
	return out
}

// SetCallerSkipFilesForTest 整体替换 caller 跳过注册表（测试用）。
// 传 nil 表示清空。
func SetCallerSkipFilesForTest(files []string) {
	if files == nil {
		callerSkipFiles.Store(nil)
		return
	}
	cp := make([]string, len(files))
	copy(cp, files)
	callerSkipFiles.Store(&cp)
}
