# oh-my-mirrorz V0.1.0 设计规格

- 状态：已批准并完成 V0.1.0 实施；Homebrew 边界含实施期安全修订
- 日期：2026-09-03
- 项目目录：`/Users/chao/Documents/Skill Factory/oh-my-mirrorz`
- GitHub 目标：公开仓库 `chaogao512/oh-my-mirrorz`
- 许可证：MIT
- 主命令：`omm`
- 兼容命令：`oh-my-mirrorz`

## 1. 产品定义

`oh-my-mirrorz` 是一个面向 macOS 和 Linux 的统一镜像源管理器。它扫描本机已经安装且由当前版本支持的软件生态，为每个生态选择可用镜像，生成可审阅的变更计划，保存原配置，执行修改并验证结果；任一步骤失败时，按快照执行补偿性回滚。

“一键切换所有源”在 V0.1.0 中严格定义为：

> 扫描本机，在 V0.1.0 已实现的适配器中识别所有适用软件，并在一次受控操作中处理所有被选中的用户级配置；只有用户显式加入 `--system` 时才处理系统级 APT 配置。

它不表示为未安装的软件创建配置，也不表示支持 MirrorZ 索引中的全部项目。

## 2. 目标与非目标

### 2.1 V0.1.0 目标

1. 提供 `scan`、`switch`、`status`、`mirrors`、`benchmark`、`history`、`restore` 和 `doctor` 命令。
2. 完成 APT、PyPI、npm、Cargo、Homebrew 五个适配器族。
3. 支持 `auto`、`fixed`、`prefer` 三种镜像策略。
4. 支持预览、快照、变更日志、结果验证和补偿性回滚。
5. 默认不修改系统文件；系统级变更需要显式参数和最小范围提权。
6. 发布 macOS/Linux 的 amd64 与 arm64 单文件二进制。
7. 对远程数据执行严格解析，不执行远程提供的 Shell 命令或模板。

### 2.2 V0.1.0 非目标

1. 不支持 Windows。
2. 不实现 DNF、Pacman、Conda、Rustup、Docker CE 或 Kubernetes；这些进入 V0.2 候选范围。
3. 不保证跨文件操作具备数据库 ACID 原子性。
4. 不自动修复用户原本已经损坏或语法非法的配置。
5. 不替用户选择私有镜像、带认证镜像或企业内部镜像。
6. 不上传本机配置、环境变量、日志、网络地址或检测结果。
7. 不把最低延迟直接等同于最佳镜像。

## 3. 已确认的产品决策

### 3.1 名称与命令

仓库名保留 `oh-my-mirrorz`。由于 Oh My Zsh 已使用 `omz` 命令，V0.1.0 不创建 `omz`。发布包包含 `omm`，同时允许以 `oh-my-mirrorz` 为二进制别名。安装器在发现已有 `omm` 时不覆盖，只安装完整名称并给出说明。

### 3.2 技术选型

程序使用 Go。开发工具链使用 Go 1.27.1，`go.mod` 的最低语言版本设为 Go 1.26。核心程序保持纯 Go；只有平台能力确实需要时才调用系统命令，并通过可替换的执行器接口隔离，便于测试。

### 3.3 许可边界

项目自身代码采用 MIT。MirrorZ Help 与 `mirrorz-docs` 用作配置方法、协议和能力研究来源，但不复制其 CC BY-NC-SA 4.0 文档正文和模板。远程 MirrorZ 数据仅作为镜像站元数据、仓库能力、状态和 URL 选择依据。

README 必须声明：本项目是社区项目，不代表 MirrorZ、CERNET 或任何镜像站的官方客户端或背书。

## 4. 用户体验与命令契约

### 4.1 基本命令

```text
omm scan
omm switch
omm switch --dry-run
omm switch --only pip,npm,cargo
omm switch --exclude homebrew
omm switch --system
omm status
omm mirrors
omm benchmark
omm history
omm restore
omm restore <snapshot-id>
omm doctor
```

### 4.2 `scan`

`scan` 只读取环境，输出：

- 操作系统、架构、当前用户和 Shell；
- 每个适配器的 `detected`、`not-installed`、`unsupported` 或 `invalid-config` 状态；
- 配置作用域及需要提权的项目；
- 当前配置是否由本工具管理。

`scan` 不访问需要凭据的内容，不修改文件。

### 4.3 `switch`

默认行为等价于：

```text
omm switch --strategy auto
```

交互终端中必须显示计划并确认；非交互终端若未传 `--yes`，以非零状态退出。`--dry-run` 完成检测、选源和计划生成，但不创建快照、不写文件。

`--only` 与 `--exclude` 互斥。未知适配器名属于参数错误。检测到无效配置时，默认跳过该适配器并使整个计划失败；用户不能通过 `--yes` 绕过语法安全检查。

### 4.4 `restore`

无参数时恢复最近一次已提交或部分失败事务的前置快照；指定 ID 时恢复目标快照。恢复前先为当前状态创建一份恢复前快照，因此误恢复仍可撤销。

同一快照重复恢复必须得到明确的“已处于目标状态”结果，而不是重复写入。

## 5. 系统架构

```text
CLI / Presenter
      |
Source Manager
      +-- Environment Scanner
      +-- Adapter Registry
      +-- Mirror Resolver
      +-- Plan Builder
      +-- Transaction Engine
      +-- Verification Engine
      +-- State / Cache Store
```

### 5.1 CLI / Presenter

负责参数解析、交互确认、文本输出和稳定退出码。业务逻辑不得直接依赖终端颜色或交互输入。

### 5.2 Environment Scanner

通过文件、可执行程序和版本信息识别平台与工具。所有探测器均返回结构化结果，并区分“未安装”“无配置”“配置无效”“权限不足”和“命令执行失败”。

### 5.3 Adapter Registry

每个适配器实现统一接口：

```go
type Adapter interface {
    ID() string
    Detect(context.Context) Detection
    Inspect(context.Context) (CurrentConfig, error)
    Plan(context.Context, Selection) ([]Change, error)
    Apply(context.Context, []Change) error
    Verify(context.Context, Selection) Verification
    Restore(context.Context, SnapshotEntry) error
}
```

适配器只负责某个生态怎样检测、修改和验证，不自行决定镜像站优先级。

### 5.4 Mirror Resolver

Resolver 接收适配器所需的仓库能力，返回镜像选择及选择依据。选择结果至少包含：

- 提供者与站点 ID；
- 仓库规范名；
- 经过验证的 HTTPS Endpoint；
- MirrorZ 状态或来源；
- 探测时间、缓存时间和失效时间；
- 回退列表；
- 选择理由。

### 5.5 Transaction Engine

事务状态机固定为：

```text
prepared -> snapshotted -> applying -> verifying -> committed
                                  |             |
                                  +-- failed ---+
                                         |
                                     rolling-back
                                         |
                            rolled-back | degraded
```

每次状态变化先写入持久化日志再执行下一阶段。程序启动时发现未结束事务，必须阻止新事务并要求先运行 `doctor` 或 `restore`。

## 6. 镜像选择规则

### 6.1 `auto`

1. 确认具体仓库在候选站点存在。
2. 优先使用 MirrorZ/CERNET 返回的资格、状态、新鲜度和网络排序。
3. 对具体仓库路径执行 HTTPS、重定向、状态码和协议级轻量探测。
4. 只在前述条件近似相同时，以客户端观测延迟作为次要排序信号。
5. 为每个仓库独立选择，允许 PyPI、Cargo 与 Homebrew 使用不同站点。

APT 优先配置 MirrorZ 的 `mirror+https` 列表以保留多个回退站点。其他适配器使用解析后的直接 Endpoint，避免把运行时 302 行为强加给不兼容的客户端。

### 6.2 `fixed`

用户指定站点后，Resolver 必须确认该站点声明并实际提供对应仓库。若不存在则失败或按适配器明确标记跳过，不得根据站点主页拼接猜测 URL。

### 6.3 `prefer`

先按 `fixed` 规则验证优先站点；不满足时回退到 `auto`，并在计划中明确显示发生了回退。

### 6.4 网络与缓存

- 默认只接受 HTTPS。
- 默认拒绝带用户信息的 URL、回环地址、链路本地地址和私有地址。
- 校园内部镜像必须通过显式 `--allow-private` 使用。
- 缓存只用于减少发现请求；执行新切换前仍须验证目标 Endpoint。
- 离线时允许 `scan`、`status`、`history` 和 `restore`，但不允许基于未验证缓存发起新切换。

## 7. 适配器规格

### 7.1 APT

支持 Debian 与 Ubuntu：

- 传统 One-Line-Style；
- Ubuntu 24.04 及之后的 DEB822；
- `deb` 与用户已有的 `deb-src` 状态；
- 架构与发行版代号校验；
- Ubuntu ports 与主仓库区分。

默认保留官方 security 源。只有 `--include-security` 才允许替换，且计划中显示同步延迟风险警告。

APT 是系统级适配器，只有 `--system` 时进入计划。程序从普通用户启动，只对经过摘要校验的目标文件写入操作请求提权；不得以 root 身份重新扫描用户 HOME。

### 7.2 PyPI

逻辑适配器包含 pip 与 uv 两个子适配器：

- pip 修改用户级 INI 配置的 `global.index-url`；
- uv 修改用户级 `uv.toml`；
- 保留所有无关配置；
- 不修改项目级 Poetry/PDM 配置；
- 验证使用 Simple Repository API 的轻量请求。

### 7.3 npm

npm 使用独立的 Registry Provider 接口。V0.1.0 内置：

- 官方上游 `https://registry.npmjs.org/`，用于识别和恢复；
- npmmirror `https://registry.npmmirror.com/`，作为经过明确登记的镜像候选；
- 用户通过配置文件声明的自定义 Registry，默认不参与 `auto`，只能显式 `fixed` 使用。

npm 适配器只修改用户级 `.npmrc` 的 `registry`，保留 scope registry、认证令牌和其他字段。日志、Diff 与快照清单不得输出认证值。

### 7.4 Cargo

使用 `$CARGO_HOME/config.toml`，不存在时才回退识别旧 `config`。通过 `[source.crates-io] replace-with` 和独立镜像 source 配置稀疏索引，要求 `sparse+https://` 且末尾 `/` 存在。不得修改 `credentials.toml` 或令牌。

若现有 `replace-with` 指向用户自定义 source，默认报冲突并跳过，不覆盖用户意图。

### 7.5 Homebrew

Homebrew 作为一个逻辑适配器管理：

- `HOMEBREW_BREW_GIT_REMOTE`；
- `HOMEBREW_API_DOMAIN`；
- `HOMEBREW_BOTTLE_DOMAIN`；
- `HOMEBREW_PIP_INDEX_URL`；
- 仅在确有 Core Git Tap 时处理 `HOMEBREW_CORE_GIT_REMOTE`。

持久化采用带稳定起止标记的托管配置块：

```text
# >>> oh-my-mirrorz managed block >>>
...
# <<< oh-my-mirrorz managed block <<<
```

实施采用 Homebrew 原生 `brew.env`，不改写 `.zprofile`、`.bash_profile` 或其他 Shell 启动文件，因此可独立于 zsh、bash、fish 工作。恢复时只移除或复原托管块，不删除用户其他环境变量。

#### V0.1 实施期安全修订

独立复核确认：`HOMEBREW_BREW_GIT_REMOTE` 与 `HOMEBREW_CORE_GIT_REMOTE` 可能在后续 `brew update` 时改变 Homebrew 仓库的真实 Git origin，仅恢复 `brew.env` 不能保证恢复该隐藏状态。为维持 V0.1“全部已应用变更均可由文件快照完整恢复”的不变量，正式 CLI 只管理 `HOMEBREW_API_DOMAIN`、`HOMEBREW_BOTTLE_DOMAIN` 与 `HOMEBREW_PIP_INDEX_URL`，暂不设置两个 Git remote 变量。适配器保留显式内部开关供后续实现 adapter-aware remote 快照与恢复后启用。

## 8. 文件与状态存储

遵循 XDG，缺省路径为：

```text
$XDG_CONFIG_HOME/oh-my-mirrorz/config.toml
$XDG_STATE_HOME/oh-my-mirrorz/state.json
$XDG_STATE_HOME/oh-my-mirrorz/transactions/<id>/
$XDG_CACHE_HOME/oh-my-mirrorz/mirrorz/
```

未设置 XDG 变量时分别回退到：

```text
~/.config/oh-my-mirrorz
~/.local/state/oh-my-mirrorz
~/.cache/oh-my-mirrorz
```

状态目录权限为 `0700`，快照文件权限上限为 `0600`。快照记录原始内容、权限、所有者、目标路径和 SHA-256。符号链接不直接跟随写入：先解析并校验目标是否仍位于允许的配置范围，否则拒绝修改。

## 9. 写入与提权安全

1. 配置解析失败时禁止写回。
2. 写入使用同目录临时文件、权限复制、`fsync` 和原子重命名。
3. 计划中的每项变更包含旧内容摘要和目标内容摘要；执行前重新检查旧摘要，防止计划生成后文件被外部修改。
4. 系统级写入只接受当前事务生成的规范化路径、摘要和临时文件，不接受任意命令字符串。
5. 远程 JSON/YAML/重定向结果只解析为受限数据结构。
6. HTTP 客户端限制响应体、重定向次数、总超时和并发量。
7. 输出层统一执行凭据脱敏。

## 10. 验证与失败语义

验证分为两层：

- 配置验证：重新读取并解析写入后的配置，确认目标字段和无关字段；
- 网络验证：对该生态的真实协议入口执行低成本读取，不下载完整软件包。

任一进入计划的适配器验证失败，整个事务进入回滚。V0.1.0 不提供“可选适配器”或隐式“尽力而为”模式；需要跳过的项目必须在计划生成前通过 `--exclude` 排除。

回滚后逐项验证原内容、权限和摘要。全部一致时状态为 `rolled-back`；任一不一致时状态为 `degraded`，退出码非零并列出准确路径、快照位置和手工恢复命令。

## 11. 退出码

```text
0  成功或无需变更
2  参数或用法错误
3  检测/配置解析失败
4  镜像解析或网络验证失败
5  权限被拒绝
6  应用失败且已完整回滚
7  回滚不完整，系统处于 degraded 状态
8  存在未结束事务或锁冲突
```

## 12. 测试设计

### 12.1 单元测试

- URL、域名和私有地址校验；
- MirrorZ 排序结果解析与策略回退；
- INI、TOML、npmrc、Shell 托管块和 APT 格式合并；
- 快照、权限、摘要、事务状态机；
- 凭据脱敏与错误映射；
- `only`、`exclude`、`system` 参数组合。

### 12.2 Golden Fixtures

至少覆盖：

- Ubuntu 22.04 `sources.list`；
- Ubuntu 24.04 DEB822；
- Debian 12/13；
- x86 Ubuntu 与 ubuntu-ports；
- pip 含 `extra-index-url`；
- uv TOML 含无关字段；
- npmrc 含 scope registry 和认证令牌；
- Cargo 旧 `config`、新 `config.toml` 和自定义 replace-with；
- Homebrew 的 zsh/bash 配置及重复托管块。

### 12.3 隔离集成测试

所有写入测试使用临时 HOME、XDG 目录、假命令执行器和本地 HTTP 测试服务。测试正常切换、无变化、重定向、超时、TLS 失败、恶意响应、外部并发修改和恢复。

### 12.4 系统级集成测试

使用 Ubuntu 与 Debian 容器测试 APT。macOS 本机只执行只读探测和临时目录内的 Homebrew 配置流程，不修改真实用户配置。

### 12.5 故障注入

在每一个事务阶段注入失败，包括：

- 第 N 个适配器写入失败；
- 验证失败；
- 用户拒绝提权；
- 进程中断后重新启动；
- 快照缺失或摘要不符；
- 回滚阶段权限改变。

### 12.6 构建与质量检查

- `go test ./...`；
- `go test -race ./...`；
- `go vet ./...`；
- 格式检查；
- 关键解析器的限时 fuzz 测试；
- macOS/Linux 的 amd64、arm64 交叉编译；
- 安装包完整性和命令冲突检查。

## 13. V0.1.0 验收标准

必须同时满足：

1. `scan` 对未安装软件和无效配置给出正确分类。
2. `switch --dry-run` 显示准确路径、作用域和脱敏后的字段级差异，且不产生配置和快照变更。
3. 五个适配器族均有单元测试与隔离集成测试。
4. Ubuntu 传统格式与 DEB822 均通过应用、验证、恢复测试。
5. 任一写入或验证故障后，原配置内容、权限和摘要全部恢复一致。
6. 回滚故障能够稳定进入 `degraded`，不得报告成功。
7. `restore` 幂等，并能撤销一次恢复操作。
8. 日志、终端输出和事务清单不泄露 npm、代理或 URL 凭据。
9. 本地测试全部通过后才允许创建发布提交和标签。
10. SSH 推送完成后，GitHub CI 在目标矩阵全部通过。

## 14. 仓库与发布流程

仓库为公开 MIT 项目。实施阶段完成后：

1. 完成 README（中文默认、英文补充）、LICENSE、SECURITY、CHANGELOG 和贡献指南。
2. 执行全部本地测试并保存简洁测试摘要。
3. 检查无真实用户配置、快照、密钥、令牌或构建缓存进入 Git。
4. 通过 SSH 远端 `git@github.com:chaogao512/oh-my-mirrorz.git` 推送，不使用 `gh auth login`。
5. 若远端尚不存在，使用已登录的 GitHub 网页创建公开空仓库，再继续 SSH 推送。
6. 等待 GitHub CI 完成；全部通过后创建并推送 `v0.1.0` 标签。

## 15. 实施顺序

1. 建立 CLI、领域模型、文件系统和命令执行抽象。
2. 实现事务、快照、锁、日志与故障注入测试。
3. 实现 MirrorZ Resolver、缓存与本地测试服务。
4. 依次实现 PyPI、npm、Cargo、Homebrew、APT 适配器。
5. 完成跨适配器事务集成测试。
6. 完成文档、安装器、交叉构建和隐私审计。
7. 本地验收通过后执行 GitHub SSH 发布和远端 CI 验证。

## 16. 参考依据

- MirrorZ Help：<https://help.mirrors.cernet.edu.cn/>
- MirrorZ 数据格式：<https://github.com/mirrorz-org/mirrorz>
- MirrorZ 302：<https://github.com/mirrorz-org/mirrorz-302>
- MirrorZ Docs：<https://github.com/mirrorz-org/mirrorz-docs>
- Oh My Tuna：<https://github.com/tuna/oh-my-tuna>
- Oh My Zsh CLI：<https://github.com/ohmyzsh/ohmyzsh/blob/master/lib/cli.zsh>
- Cargo Source Replacement：<https://doc.rust-lang.org/cargo/reference/source-replacement.html>
- Homebrew 环境变量：<https://docs.brew.sh/Manpage>
- npmmirror：<https://npmmirror.com/>
- Go Release History：<https://go.dev/doc/devel/release>
