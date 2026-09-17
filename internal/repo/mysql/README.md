# 包说明

## 包作用
- 提供关系型持久化实现，覆盖会话、消息、用户与消费收件箱。
- 通过仓储方法向业务层提供稳定读写接口。

## 实现逻辑
1. 初始化数据库连接与连接池参数。
2. 会话仓储负责会话创建和消息追加与查询。
3. 鉴权仓储负责用户与刷新令牌生命周期。
4. 收件箱仓储负责消费幂等状态与失败记录。

## 关键接口
```go
func NewDB(cfg config.MySQLConfig) (*gorm.DB, error)
type SessionRepository struct
type AuthRepository struct
type InboxRepository struct
```

## 协作关系
- 被业务服务层和消息消费模块共同依赖。
- 与领域模型模块进行数据映射。
