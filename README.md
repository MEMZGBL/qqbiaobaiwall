# qq表白墙

一个给 QQ 群用的「表白墙/投稿墙」服务。



项目基于 Go，数据存 SQLite，机器人侧使用 ZeroBot（对接 NapCat WebSocket），QQ 空间接口由 `qzone-go` 提供。


## 主要能力

- 投稿来源
  - qq群命令投稿
  - 网页投稿页投稿
- 审核流程
  - `pending -> approved -> published`
  - 失败会落到 `failed`，并记录失败原因
- 发布方式
  - 发布前将投稿渲染成一张截图（文字+图片）
  - 再把截图作为图片发到 QQ 空间
- Cookie 管理
  - 启动后异步尝试 `GetCookies`（优先）
  - 失败再回退到扫码登录
  - 会话过期时自动触发刷新回调
- 安全与数据
  - SQLite 持久化（WAL）
  - Web 管理后台账号+会话
  - 可配置敏感词过滤

## 项目结构

```text
qzonewall-go/
├─ cmd/wall/main.go                # 程序入口
├─ internal/config/                # 配置加载与默认值
├─ internal/source/qq_bot.go       # QQ Bot 命令与事件处理
├─ internal/task/worker.go         # 审核后自动发布 Worker
├─ internal/task/keepalive.go      # Cookie 校验/刷新/扫码逻辑
├─ internal/web/server.go          # Web 后台与投稿页
├─ internal/render/screenshot.go   # 投稿截图渲染
├─ internal/store/sqlite.go        # SQLite 存储
├─ config.yaml                     # 配置文件
├─ run.bat / run.sh                # 启动脚本
├─ Dockerfile                      # Docker 构建文件
├─ .dockerignore                   # Docker 忽略文件
└─ winres/                         # Windows 资源文件
```

## 环境要求

- Go `1.24+`
- 一个可用的 NapCat + ZeroBot WebSocket 连接
