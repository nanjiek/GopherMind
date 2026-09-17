# 包说明

## 包作用
- 提供统一配置模型，承载服务运行所需全部参数。
- 封装环境变量读取与默认值回退，降低部署复杂度。

## 实现逻辑
1. 通过加载函数构建顶层配置对象。
2. 按类型解析字符串、整数、时长、布尔和列表。
3. 对缺失配置使用安全默认值，避免空值传播。

## 关键接口
```go
func Load() Config
func getEnv(key string, fallback string) string
func getInt(key string, fallback int) int
func getDuration(key string, fallback time.Duration) time.Duration
```

## 协作关系
- 被启动入口和基础设施初始化流程调用。
- 向业务层和外部客户端提供参数来源。
