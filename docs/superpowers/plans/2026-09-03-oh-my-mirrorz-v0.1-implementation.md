# oh-my-mirrorz V0.1.0 实施计划

- 依据：`docs/superpowers/specs/2026-09-03-oh-my-mirrorz-design.md`
- 状态：实施与本地验收完成，等待远端 CI 与标签发布
- 原则：先测试核心安全边界，再连接真实适配器；真实用户配置只做只读扫描

## 里程碑 1：工程与领域模型

1. 建立 Go module、CLI 入口和版本信息。
2. 定义 Detection、Selection、Change、Snapshot、Transaction 和 Adapter 接口。
3. 建立文件系统、HTTP、命令执行和时钟抽象。
4. 为退出码、选择器和过滤参数编写单元测试。

验收：项目可构建；CLI 帮助、版本和参数错误行为稳定。

## 里程碑 2：安全写入与事务

1. 实现 XDG 路径、目录权限、锁和事务日志。
2. 实现摘要校验、快照、原子写入和权限恢复。
3. 实现 prepared 到 committed/rolled-back/degraded 的状态机。
4. 注入 apply、verify 和 rollback 故障。

验收：失败后内容、权限和摘要恢复；回滚失败稳定进入 degraded。

## 里程碑 3：镜像解析

1. 实现 MirrorZ 302、APT mirrorlist 和站点/仓库元数据客户端。
2. 实现 auto、fixed、prefer 策略。
3. 实现 URL 安全、超时、重定向、缓存和轻量探测。
4. 通过本地 HTTP 服务完成确定性测试。

验收：候选不存在时不猜测 URL；离线时只允许读取状态和恢复。

## 里程碑 4：五类适配器

按风险从低到高实现：

1. PyPI：pip、uv。
2. npm：官方上游、npmmirror、自定义 fixed。
3. Cargo：稀疏索引与 replace-with 冲突保护。
4. Homebrew：zsh/bash 托管配置块。
5. APT：Ubuntu/Debian、传统格式、DEB822、security 保留、ports 区分。

验收：每个适配器均有 golden fixture、计划、应用、验证、恢复测试。

## 里程碑 5：CLI 主链

1. 串联 scan、resolve、plan、snapshot、apply、verify 和 rollback。
2. 完成 switch、status、mirrors、benchmark、history、restore、doctor。
3. 完成交互确认、dry-run、only/exclude/system 和稳定退出码。
4. 确保输出统一脱敏。

验收：临时 HOME 中完成跨适配器 switch -> verify -> restore。

## 里程碑 6：交付与本地验收

1. 编写中英文 README、MIT LICENSE、SECURITY、CONTRIBUTING、CHANGELOG。
2. 建立 GitHub Actions 测试与构建矩阵。
3. 运行 test、race、vet、格式、fuzz、交叉构建和隐私审计。
4. 在当前 macOS 上运行真实只读 scan 与隔离写入演练。
5. 独立验收规格覆盖和交付物质量。

验收：全部本地检查通过，无真实配置、凭据、快照和缓存进入 Git。

## 里程碑 7：SSH 发布

1. 验证本地 SSH 身份与目标远端。
2. 必要时通过已登录网页创建公开空仓库。
3. 推送 main，等待远端 CI。
4. CI 通过后创建并推送 `v0.1.0`。
5. 核对远端 main、tag 与本地提交一致。

验收：GitHub main 与 v0.1.0 指向已通过测试的同一发布提交。
