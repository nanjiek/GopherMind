# 包说明

## 包作用
- 定义密钥读取抽象，隔离上层与具体密钥来源实现。

## 实现逻辑
1. 通过接口约束读取行为。
2. 默认实现从环境变量读取密钥。

## 关键接口
```go
type Provider interface
type EnvProvider struct
func (p *EnvProvider) Get(name string) string
```

## 协作关系
- 供鉴权与外部调用模块读取敏感参数。
