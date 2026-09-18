# 包说明

## 包作用
- 提供 PostgreSQL 持久化实现，覆盖现有业务表及 P1 Event Log 基础表。
- 通过仓储方法向业务层提供稳定读写接口。
- 使用 `migrations/*.up.sql` 管理 schema；应用启动只校验版本，不自动修改表结构。

## 实现逻辑
1. `go run ./cmd/migrate` 在空 schema 上按版本执行 migration；未知非空 schema 会被拒绝。
2. `NewDB` 初始化连接池并校验 schema 版本。
3. 会话仓储负责会话创建和消息追加与查询。
4. 鉴权仓储负责用户与刷新令牌生命周期。
5. 收件箱仓储负责消费幂等状态与失败记录。

## 关键接口
```go
func OpenDB(cfg config.PostgresConfig) (*gorm.DB, error)
func NewDB(cfg config.PostgresConfig) (*gorm.DB, error)
func ApplyMigrations(ctx context.Context, db *gorm.DB) error
type SessionRepository struct
type AuthRepository struct
type InboxRepository struct
```

## 协作关系
- 被业务服务层和消息消费模块共同依赖。
- 与领域模型模块进行数据映射。
