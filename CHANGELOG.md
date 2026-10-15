# Changelog — oekit/logx

由 ekit v0.34.0（git.enjoye.top/enjoydream/ekit，冻结维护）拆分而来。所有 notable 变更记录如下。


## [v0.1.10] — 2026-10-16

- **fix**：slog 后端 kv/Attr 中的 error 值渲染为 `Error()` 文本——原实现直接
  `json.Marshal`，多数 error（`fmt.wrapError`、包装链等）无导出字段被渲染成
  `{}`，错误原因整条丢失（[#2](https://github.com/oekit/logx/issues/2)）；
  与 log/slog JSONHandler 的 error 处理对齐。行为变化：实现了
  `json.Marshaler` 的错误类型，输出由 marshal 结果变为错误文本

## [v0.1.8] — 2026-10-15

- **breaking**：hertzlog 删除旧适配别名 `AsHlogLogger`/`AsHlogLoggerFromXxx`，
  统一 `AsHlogLoggerXxx` 系列并改为自包含实现（四后端均直接实现 hlog.FullLogger）
## [v0.1.6] — 2026-10-15

- v0.1.6

## [v0.1.5] — 2026-10-15

- v0.1.5

## [v0.1.4] — 2026-10-15

- v0.1.4

## [v0.1.3] — 2026-10-15

- v0.1.3

## [v0.1.1] — 2026-10-15

- v0.1.1

## [v0.1.0] — 2026-10-15

- v0.1.0
