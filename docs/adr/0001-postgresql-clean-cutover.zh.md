# ADR-0001：PostgreSQL 空库切换

状态：接受
日期：2026-09-17

## 背景

当前代码通过 GORM/MySQL 保存用户、会话、消息、消费 Inbox、文档、评测、MCP Job 和记忆目录，并在进程启动时执行 AutoMigrate。V3 目标使用 PostgreSQL 保存事实、执行状态和审计信息。

用户明确决定：原有数据全部作废，不迁移 MySQL 数据，直接使用 PostgreSQL。

## 决策

- 新架构只支持 PostgreSQL，配置统一为 `POSTGRES_DSN`。
- PostgreSQL 从空库开始，使用版本化 SQL migration 建表；生产启动过程不运行 GORM AutoMigrate。
- 不实现 MySQL/PostgreSQL 双写、历史回填、CDC、增量追平或记录级对账。
- 首次发布前确认目标数据库为空或属于本应用的新 schema，执行 migration 后再启动应用。
- 旧 MySQL 容器、驱动、配置和 repository 在 PostgreSQL repository 覆盖全部接口且测试通过后删除。
- Event Log、Projection、Task、Mailbox、权威记忆及现有业务表共享 PostgreSQL，但保持独立表和明确事务边界。

## 切换与回滚

切换顺序为：创建空库 → 执行版本化 migration → 校验 schema → 部署 PostgreSQL-only 应用 → 冒烟验证 → 开放写入。任何旧 MySQL 数据都不进入该流程。

切换前可以回滚到旧应用和旧 MySQL，二者仍构成完整旧系统。新系统开放写入后，旧 MySQL 不再是可接受的回滚数据源；回滚必须使用兼容 PostgreSQL schema 的前一应用版本或恢复 PostgreSQL 备份。测试环境可以清空 PostgreSQL 后重新初始化。

## 验收

- 空 PostgreSQL 上从零执行全部 migration 成功，重复检查不会重复创建对象。
- 两个 Go 入口只依赖 PostgreSQL，代码和 compose 不再需要 MySQL。
- repository 集成测试覆盖事务、唯一约束、幂等、CAS 和租约接管。
- migration 失败时应用不启动；schema 版本与应用不兼容时健康检查失败。
- 文档明确旧数据废弃，任何脚本都不会自动连接旧 MySQL 搬运数据。

## 影响

实现更直接，但旧账号、会话、文档状态和记忆不可在新版本中恢复。首次使用新系统需要重新注册或重新建立测试身份，重新导入文档并创建记忆。这是已接受的产品行为。
