# 包说明

## 包作用
- 负责令牌签发、解析和校验。
- 提供令牌摘要能力用于安全存储。

## 实现逻辑
1. 初始化时注入签名参数和有效期。
2. 签发访问令牌与刷新令牌。
3. 解析时校验签名、类型和过期状态。
4. 使用摘要函数避免明文令牌落库。

## 关键接口
```go
type Manager struct
func (m *Manager) GenerateTokenPair(userID string, role string) (TokenPair, error)
func (m *Manager) ParseAccessToken(raw string) (*Claims, error)
func HashToken(token string) string
```

## 协作关系
- 被鉴权服务和鉴权中间件共同依赖。
