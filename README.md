<p align="center">
  <img src="assets/hero.svg" alt="oh-my-mirrorz：安全、可预览、可恢复的一键换源工具" width="100%">
</p>

<h1 align="center">oh-my-mirrorz</h1>

<p align="center">
  <strong>一次扫描，统一换源；每次修改，都能恢复。</strong><br>
  面向 macOS 与 Linux 的安全镜像源管理器。
</p>

<p align="center">
  <a href="https://github.com/chaogao512/oh-my-mirrorz/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/chaogao512/oh-my-mirrorz/actions/workflows/ci.yml/badge.svg"></a>
  <a href="https://github.com/chaogao512/oh-my-mirrorz/releases/latest"><img alt="Latest Release" src="https://img.shields.io/github/v/release/chaogao512/oh-my-mirrorz?color=6f5bd3"></a>
  <a href="https://github.com/chaogao512/oh-my-mirrorz/blob/main/LICENSE"><img alt="License" src="https://img.shields.io/badge/license-MIT-23b5d3"></a>
  <img alt="Platforms" src="https://img.shields.io/badge/macOS%20%7C%20Linux-amd64%20%7C%20arm64-17294d">
</p>

<p align="center">
  简体中文 · <a href="README.en.md">English</a>
</p>

> [!NOTE]
> `oh-my-mirrorz` 是独立社区项目，不是 MirrorZ、CERNET 或任何镜像站的官方客户端，也不代表其认可或背书。

## 安装

### Homebrew（macOS 推荐）

```bash
brew install chaogao512/tap/oh-my-mirrorz
```

安装后可直接运行 `omm`，不需要修改 `.zshrc`，后续使用 `brew upgrade oh-my-mirrorz` 即可升级。Formula 由独立的 [`chaogao512/homebrew-tap`](https://github.com/chaogao512/homebrew-tap) 维护。

### 一键安装脚本（macOS / Linux）

```bash
curl -fsSL https://raw.githubusercontent.com/chaogao512/oh-my-mirrorz/main/install.sh | sh
```

脚本会识别系统与架构、下载对应 Release，并在安装前校验 SHA-256。默认安装到 `~/.local/bin`；如果已存在第三方 `omm`，则改用 `oh-my-mirrorz`，不会静默覆盖。

也可以从 [GitHub Releases](https://github.com/chaogao512/oh-my-mirrorz/releases/latest) 手动下载 macOS/Linux 的 amd64 或 arm64 压缩包。

## 30 秒开始使用

先扫描本机，不修改任何文件：

```bash
omm scan
```

预览即将发生的全部用户级变更：

```bash
omm switch --dry-run
```

确认计划后执行：

```bash
omm switch
```

需要恢复时：

```bash
omm history
omm restore
```

无参数 `restore` 会恢复最近一次可恢复事务；也可以使用 `omm restore <snapshot-id>` 选择历史快照。

## 为什么做这个工具

单独修改 pip、npm、Cargo、Homebrew、APT 和 Conda 并不难，难的是知道哪些配置真正生效、哪些字段不该碰，以及失败后如何可靠回到原状。

`oh-my-mirrorz` 把换源变成一个可审查的事务：扫描当前环境，为每个软件生态选择合法入口，展示写入计划，保存原始快照，再应用并验证。任何适配器失败，已执行的变更都会按逆序回滚。

<p align="center">
  <img src="assets/workflow.svg" alt="扫描、选择、预览、应用、验证与恢复工作流" width="100%">
</p>

## 当前支持

| 生态 | 配置范围 | 自动策略 | 保护边界 |
| --- | --- | --- | --- |
| pip / uv | 用户级 | MirrorZ / CERNET | 保留无关字段，不修改项目配置 |
| npm | 用户级 | npmmirror | 保留 scope registry、令牌和证书字段 |
| Cargo | 用户级 | MirrorZ / CERNET sparse index | 不覆盖项目级或已有自定义 `replace-with` |
| Homebrew | 用户级 `brew.env` | MirrorZ / CERNET API、bottles、构建用 PyPI | 不修改 Brew/Core Git remote |
| APT | Debian / Ubuntu 系统级 | MirrorZ APT mirrorlist | 需显式 `--system`；默认保留 security 与第三方源 |
| Conda / Mamba / Micromamba | 用户级 `~/.condarc` | MirrorZ / CERNET Anaconda | 保留频道、顺序、优先级、私有源与无关字段 |

当前版本支持 macOS 与 Linux 的 amd64、arm64。Windows、DNF、Pacman、Docker CE、Rustup 与 Kubernetes 尚未支持。

## 安全不是附加项

- **先预览。** `--dry-run` 不创建快照，也不写入文件。
- **先快照。** 每次写入前保存原始文件与事务清单，目录权限为 `0700`，内容权限为 `0600`。
- **防止误覆盖。** 应用前再次核对 SHA-256；若文件在预览后被其他程序修改，操作会停止。
- **原子写入。** 用户文件通过同目录临时文件和原子重命名更新；系统文件只通过受限参数调用 `sudo install`。
- **限制目标。** 默认只接受无凭据 HTTPS 地址，并拒绝显式私有、回环和链路本地端点。
- **失败回滚。** 配置验证或网络验证失败后，已应用的文件按逆序恢复。
- **写入前预检。** 每种生态都使用真实协议入口验证本次选择，目标不可达时不会修改配置。
- **保留安全更新。** Ubuntu/Debian security 源默认不切换，减少镜像同步延迟带来的风险。

事务记录位于 `$XDG_STATE_HOME/oh-my-mirrorz`；未设置时使用 `~/.local/state/oh-my-mirrorz`。

## 镜像选择策略

| 策略 | 行为 | 示例 |
| --- | --- | --- |
| `auto` | 使用该生态内置、经过约束的默认入口 | `omm switch` |
| `fixed` | 只使用指定站点；不支持该生态时直接失败 | `omm switch --strategy fixed --mirror tuna` |
| `prefer` | 优先指定站点，不可用时回退到 `auto` | `omm switch --prefer ustc` |

用 `omm mirrors` 查看内置站点。`auto` 默认保留 MirrorZ 的动态择优能力；它综合仓库能力、站点状态、同步新鲜度和网络信息，但不等同于本机实时带宽测速。

## 透明测速，不自动改源

`benchmark` 使用所有适配器共用的测速引擎，比较每个仓库能力下的 `auto` 与固定候选源：

```bash
omm benchmark
omm benchmark --adapter pypi
omm benchmark --adapter conda --runs 3
```

结果会显示候选源、MirrorZ 当前实际落点、成功次数、响应延迟中位数，以及该次探测中的最低延迟源。这里的 `fastest (sample)` 只表示本轮低成本请求的响应延迟最低，不承诺完整包下载速度，也不会自动修改配置。

如果希望固定某个结果，可显式执行：

```bash
omm switch --only conda --strategy fixed --mirror ustc
```

APT 使用有序 mirrorlist 保留多站点回退，因此只报告健康状态，不把一次测速结果标记为固定“最快源”。

## 常用命令

| 命令 | 作用 |
| --- | --- |
| `omm scan` | 只读扫描已安装生态与配置位置 |
| `omm switch --dry-run` | 展示完整换源计划，不写文件 |
| `omm switch` | 交互确认后应用用户级配置 |
| `omm switch --only pip,npm,cargo` | 只处理指定适配器 |
| `omm switch --only conda` | 只切换 Conda/Mamba/Micromamba 的公开频道镜像 |
| `omm switch --exclude homebrew` | 排除指定适配器 |
| `omm mirrors --adapter cargo` | 查看指定生态的内置镜像 |
| `omm benchmark [--adapter NAME] [--runs N]` | 比较同一仓库能力的全部候选源 |
| `omm history` | 查看本机事务历史 |
| `omm restore [snapshot-id]` | 恢复最近或指定快照 |
| `omm doctor` | 检查无效配置和未完成事务 |

### Debian / Ubuntu 系统源

APT 不会默认进入换源计划。需要时先预览，再显式启用系统级操作：

```bash
omm scan --system
omm switch --system --dry-run
omm switch --system
```

只有真正写入系统文件时才会请求 `sudo`。如确有需要，可追加 `--include-security` 切换 security 源；这一选项必须与 `--system` 同时使用。

### Conda、Mamba 与 Micromamba

三者统一归一化为 `conda` 适配器。工具只管理 `~/.condarc` 中正在使用的公开镜像字段：

- 保留现有 `channels` 内容和顺序；
- 保留 `channel_priority`、代理、SSL、缓存目录等无关配置；
- 保留未知或私有 `custom_channels`，不输出凭据；
- 不在 `defaults`、`conda-forge` 与 `nodefaults` 之间擅自转换；
- 发现环境变量或另一份 Conda/Mamba 配置会覆盖频道时停止操作并指出冲突。

切换前和写入后都会读取当前系统对应的 `repodata.json`，例如 Apple Silicon 使用 `osx-arm64`，不会下载软件包或清理 Conda 缓存。

## 从源码构建

需要 Go 1.26 或更新版本：

```bash
go test ./...
go build -trimpath -o omm ./cmd/omm
```

## 文档与参与

| 内容 | 入口 |
| --- | --- |
| 设计、安全边界与恢复模型 | [`docs/superpowers/specs/2026-09-03-oh-my-mirrorz-design.md`](docs/superpowers/specs/2026-09-03-oh-my-mirrorz-design.md) |
| Conda 与统一测速设计 | [`docs/superpowers/specs/2026-09-04-conda-and-unified-benchmark-design.md`](docs/superpowers/specs/2026-09-04-conda-and-unified-benchmark-design.md) |
| 一键安装脚本 | [`install.sh`](install.sh) |
| 发布记录 | [`CHANGELOG.md`](CHANGELOG.md) |
| 参与贡献 | [`CONTRIBUTING.md`](CONTRIBUTING.md) |
| 安全问题报告 | [`SECURITY.md`](SECURITY.md) |

欢迎提交问题、适配器建议和可复现的镜像兼容性报告。新增生态必须同时说明配置优先级、凭据边界、验证方式和恢复语义。

## 许可证

[MIT License](LICENSE) © 2026 Gaochao
