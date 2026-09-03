# oh-my-mirrorz

一个安全、可预览、可恢复的 macOS/Linux 一键换源工具。

`oh-my-mirrorz` 会扫描本机已安装的软件生态，为每个生态选择合适的镜像，在写入前展示计划并保存快照；配置或网络验证失败时，会自动恢复原状。

> 本项目是独立社区项目，不是 MirrorZ、CERNET 或任何镜像站的官方客户端，也不代表其认可或背书。

[English](README.en.md)

## 当前支持

| 生态 | 配置范围 | 默认策略 | 重要保护 |
| --- | --- | --- | --- |
| pip / uv | 用户级 | MirrorZ/CERNET | 保留其他字段，不改项目配置 |
| npm | 用户级 | npmmirror | 不改 scope registry 和认证字段 |
| Cargo | 用户级 | MirrorZ/CERNET sparse index | 不覆盖已有自定义 `replace-with` |
| Homebrew API / bottles / 构建用 PyPI | 用户级 `brew.env` | MirrorZ/CERNET | 只维护带标记的配置块，不改 Git remote |
| APT（Debian/Ubuntu） | 系统级，需 `--system` | MirrorZ APT mirrorlist | 保留第三方源，默认保留 security 源 |

V0.1.0 支持 macOS 与 Linux 的 amd64、arm64。Windows、DNF、Pacman、Conda、Docker CE、Rustup 和 Kubernetes 暂不支持。

## 安装

从 GitHub Release 下载对应系统的压缩包，校验 `checksums.txt` 后，将 `omm` 放入 `PATH`。

也可以使用安装脚本（安装到 `~/.local/bin`，下载后仍会校验 SHA-256）：

```bash
curl -fsSL https://raw.githubusercontent.com/chaogao512/oh-my-mirrorz/main/install.sh | sh
```

安装器不会覆盖已有的第三方 `omm` 命令；发生重名时只安装 `oh-my-mirrorz`。

## 使用

先只读扫描：

```bash
omm scan
```

预览全部用户级变更：

```bash
omm switch --dry-run
```

确认后执行：

```bash
omm switch
```

常用选项：

```bash
omm switch --only pip,npm,cargo
omm switch --exclude homebrew
omm switch --strategy fixed --mirror tuna
omm switch --prefer ustc
omm switch --system
omm switch --system --include-security
```

系统级 APT 只有显式加入 `--system` 才会进入计划；真正写入目标文件时才调用 `sudo`，扫描过程仍以当前用户运行。默认不替换 Ubuntu/Debian 的 security 源，因为镜像同步延迟可能影响安全更新及时性。

检查、测速与恢复：

```bash
omm mirrors
omm benchmark
omm history
omm restore
omm restore <snapshot-id>
omm doctor
```

无参数 `restore` 恢复最近一次可恢复事务。恢复前会保存当前状态，因此恢复操作本身也可以撤销；若目标已经处于快照状态，则明确报告无需改动。

## 安全模型

- `--dry-run` 不创建快照、不写文件。
- 每次写入前再次核对原内容 SHA-256，避免覆盖计划生成后的外部修改。
- 用户级文件使用同目录临时文件、同步落盘和原子重命名。
- 系统级文件使用受限参数调用 `sudo install` 与原子重命名，不执行拼接的 Shell 命令。
- 快照目录权限为 `0700`，快照与事务清单权限为 `0600`。
- 默认只接受无凭据 HTTPS URL，并拒绝显式私有、回环和链路本地地址。
- APT 仅改写识别出的 Debian/Ubuntu 官方仓库；PPA、Docker 等第三方条目保持原样。
- V0.1 不设置 Homebrew 的 Brew/Core Git remote，避免用户运行 `brew update` 后出现无法由普通配置快照完整恢复的隐藏状态变化。
- 任一适配器在配置或网络验证中失败，已应用的变更会按逆序回滚。

事务记录位于 `$XDG_STATE_HOME/oh-my-mirrorz`，未设置时使用 `~/.local/state/oh-my-mirrorz`。

## 镜像策略

- `auto`：使用内置、按仓库验证的 MirrorZ/CERNET 或明确登记的生态镜像入口；APT 使用 `mirror+https` 保留客户端回退能力。
- `fixed`：使用指定的内置站点，如 `tuna`、`ustc` 或 `npmmirror`；站点不提供相应生态时直接失败，不猜测 URL。
- `prefer`：优先指定站点；该生态不可用时回退到 `auto`，并记录选择理由。

用 `omm mirrors` 查看每个适配器当前内置的站点。

## 从源码构建

需要 Go 1.26 或更新版本：

```bash
go test ./...
go build -trimpath -o omm ./cmd/omm
```

完整设计与安全边界见 [`docs/superpowers/specs/2026-09-03-oh-my-mirrorz-design.md`](docs/superpowers/specs/2026-09-03-oh-my-mirrorz-design.md)。

## 许可证

[MIT](LICENSE)
