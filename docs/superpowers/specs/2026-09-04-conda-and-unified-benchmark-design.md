# oh-my-mirrorz v0.2.0：Conda 与统一测速设计

## 目标

v0.2.0 将 Conda、Mamba、Micromamba 纳入现有事务模型，并让所有适配器共用同一种镜像探测与 benchmark 语义。版本升级不得改变用户的依赖来源策略，也不得把一次低成本延迟探测描述成长期下载速度。

## 统一不变量

每个适配器均执行：

```text
detect -> resolve -> protocol preflight -> plan -> snapshot
       -> atomic apply -> config/network verify -> commit or rollback
```

- 镜像选择只有 `auto`、`fixed`、`prefer` 三种策略。
- Adapter 只声明配置变更和真实协议探测入口；Resolver 独占镜像目录与优先级；Benchmark Engine 独占重复探测、排序和输出语义。
- `prefer` 的命名镜像缺失或预检失败时回退 `auto`，并记录回退理由。
- `benchmark` 只读，不自动写入胜出候选。
- `fastest (sample)` 只表示同一仓库能力在本轮探测中的最低响应延迟中位数。

## Conda 适配器

适配器 ID 为 `conda`，CLI 别名为 `conda`、`mamba`、`micromamba`。

首版唯一写目标为用户级 `~/.condarc`。适配器：

- 保留 `channels`、频道顺序、`channel_priority`、私有频道和无关配置；
- 仅在 `defaults` 当前生效时更新 `default_channels`；
- 仅更新当前使用或已经配置的公开 `conda-forge`、`pytorch` 的 `custom_channels` 地址；
- 不增加或删除 `defaults`、`conda-forge`、`nodefaults`；
- 拒绝覆盖带凭据或变量展开的公开频道 URL；
- 发现 `CONDARC`、频道环境变量、`~/.conda/.condarc` 或 `~/.mambarc` 会覆盖频道时停止；
- 使用 YAML AST 保留未知字段和注释，并依赖普通事务快照恢复完整原文件。

验证使用平台特定的 `repodata.json`，包括 `osx-arm64`、`osx-64`、`linux-64`、`linux-aarch64`；不执行安装、求解或缓存清理。

## 统一 Benchmark Engine

命令：

```text
omm benchmark [--adapter NAME] [--runs 1..10]
```

引擎从同一 Resolver Catalog 枚举 `auto` 与固定候选，对 Adapter 提供的每个 `ProbeTarget` 重复探测，输出：

- adapter；
- repository capability；
- candidate；
- redirect 后的 final target；
- 成功次数；
- 成功样本的延迟中位数；
- `dynamic`、`healthy`、`degraded`、`unreachable` 或 `fastest (sample)`。

排名限定在同一 adapter 与 capability 内。必须全部样本成功才可成为本轮胜出候选。APT probe 被标记为不可排名，继续保留 MirrorZ mirrorlist 的客户端多站点回退。

## 验收

- 六个适配器全部实现同一个 ProbeTarget 接口。
- PyPI 与 Conda 均能比较 `auto`、TUNA、USTC，并显示 MirrorZ 实际落点。
- Conda 的 defaults 与社区频道分开排名。
- 配置只含私有 Conda 频道时保持逐字节不变。
- 切换前预检失败时零写入；`prefer` 可回退；写入后验证失败仍由事务引擎完整回滚。
- 单元测试、race、vet、四目标交叉编译及真实只读端点烟雾测试全部通过。
