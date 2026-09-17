# 包说明

## 包作用
- 统一管理模型提供方选择与回退逻辑。
- 向业务层提供单一模型调用入口。

## 实现逻辑
1. 启动时注入多个模型提供方并建立映射。
2. 按模型类型返回对应实现。
3. 主实现失败时执行回退策略，降低不可用风险。

## 关键接口
```go
type ModelFactory struct
func (f *ModelFactory) Get(modelType string) (service.ModelProvider, error)
func (f *ModelFactory) GenerateWithFallback(ctx context.Context, modelType string, prompt string) (string, model.Usage, error)
```

## 协作关系
- 依赖模型提供方模块。
- 被业务服务层调用完成路由。
