# Homebrew Tap 与 README 重设计规格

日期：2026-09-04  
状态：已批准并实施

## 目标

为 `oh-my-mirrorz` 提供不需要修改 Shell 配置的标准 Homebrew 安装路径，并重写主仓库与 Tap 仓库的中英文 README，使普通用户可以在首页完成“理解—安装—预览—切换—恢复”的完整决策。

## 交付边界

1. 新建公开仓库 `chaogao512/homebrew-tap`，Formula 名为 `oh-my-mirrorz`。
2. Formula 从不可变的 `v0.1.1` 标签源码构建，固定 SHA-256，安装命令为 `omm`。
3. Tap 使用 macOS GitHub Actions 验证样式、审计、源码构建、Formula 测试和冒烟测试。
4. 主仓库与 Tap 仓库均以 `README.md` 提供简体中文，以 `README.en.md` 提供英文，并在首屏互相切换。
5. 主仓库 README 将 Homebrew 作为 macOS 推荐安装方式，同时保留安装脚本和 Release 手动安装作为跨平台备选。

## 信息架构

两套 README 都采用“视觉首屏—一句话价值—主要安装入口—关键能力—安全边界—命令/维护参考—支持与许可”的顺序。视觉风格借鉴参考项目的强首屏和内容分层，但不复制其图片、文字或项目叙事。

## 验收标准

- `brew style` 与 `brew audit --strict` 通过。
- Formula 能从 `v0.1.1` 源码构建并运行 `brew test`。
- `omm version` 输出 Formula 版本，`omm mirrors` 可离线读取内置目录。
- 两仓库的中英文 README 链接、图片路径、命令、版本号和支持范围一致。
- SVG 具有可访问标题和说明，并通过实际渲染检查。
