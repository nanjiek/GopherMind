# GopherMind AI 医疗助手 AI 实现架构与优化方案
> 版本：V3.0  
设计基线日期：2026-09-17  
技术栈：Go、LangGraph、Redis、PostgreSQL、Pinecone、BM25、BGE、DeepSeek  
文档类型：供 AI 编码代理执行的系统架构、开发任务与验收规范
>

## 0. 文档用途
这份文档用于指导 AI 编码代理和开发人员逐步改造现有 Go 项目。它回答的是：

+ 系统最终应该有哪些模块。
+ Cordis 和 DeepSeek Harness 的哪些思想需要在 Go 中实现。
+ 短期记忆和长期记忆具体怎么存储、召回、压缩和恢复。
+ 多个 Agent 如何分工、通信、去重和恢复。
+ 每个阶段应该修改什么、如何测试、达到什么标准才算完成。

AI 实现时应优先遵守以下规则：

1. 先扫描现有 Go 项目、依赖和测试，不直接覆盖已有实现。
2. 每次只实现一个阶段，保持现有问答链路可运行。
3. 新旧记忆机制需要提供迁移或兼容路径。
4. 所有新增后台任务必须支持 `context.Context` 取消。
5. 所有外部写操作必须有幂等键。
6. 所有跨 Agent 状态更新必须有版本或 CAS。
7. 每个阶段必须提交单元测试、集成测试和必要的故障测试。
8. 不将模型隐藏推理保存到日志或数据库。

## 0.1 当前能力与目标能力
为了避免 AI 把设计方案误认为现有代码，本项目分为“现有能力”和“目标升级”。

| 能力 | 当前状态 | 本方案中的目标 |
| --- | --- | --- |
| Go 问诊后端 | 已有 | 保持兼容并抽象 Agent Runtime |
| LangGraph 编排 | 已接入 | 增加可恢复 Task DAG 和 Agent 私有状态 |
| Redis 短期记忆 | 已有 | 降级为热点缓存，不再是唯一事实源 |
| 滑动窗口与摘要 | 已有 | 改为 Event Log + Surface + Compaction |
| Pinecone 长期记忆 | 已有基础 | 增加候选、确认、版本、冲突和过期 |
| RAG 检索 | 已有查询重写、递归分块、Pinecone、BM25 混合检索和 BGE 重排 | 增加版本治理、Retrieval Trace、离线评估和发布回滚 |
| DeepSeek 模型调用 | 已接入 | 增加按任务风险的 Flash/Pro 路由、Token 成本和输出治理 |
| Agent Gateway | 目标升级 | 统一模型、Workflow、Skill、Tool、MCP、预算和策略 |
| Skill 与 MCP | 目标升级 | Skill Manifest、Invocation Protocol、Knowledge Profile 和 MCP Gateway |
| 多 Agent 协作 | 目标升级 | Lead、专业 Agent、Task DAG、Mailbox |
| Cordis 式 Scope | 目标升级 | Go Scope、Capability、Effect 和 Middleware |
| 全链路审计与恢复 | 目标升级 | Event Log、Projection、幂等和重放 |
| 统一评测与发布 | 目标升级 | Gold Set、SLO、版本矩阵、灰度、阻断门禁和回滚 |


### 0.1.1 项目统一口径
后续所有架构文档和面试材料统一使用以下口径：

+ 当前生成模型统一使用 DeepSeek。
+ 当前 RAG 已包含查询重写、递归分块、Pinecone Dense Retrieval、BM25 Sparse Retrieval、混合检索和 BGE 重排。
+ `400 Token` Chunk、Dense Top 20、BM25 Top 20、融合 Top 30、重排 Top 10、最终 4～6 条证据是推荐初始参数和面试解释口径；如果没有原始实验配置，不将其描述成唯一线上固定值。
+ “准确率提升 11%、召回率提升 5%”必须与原始评测记录中的基线、样本量、Recall@K 和百分点/相对提升口径一致。
+ Event Log、Surface、Compaction、Agent Scope、Task DAG、Durable Mailbox、完整沙箱和权威患者记忆库属于生产化优化设计；未落地前不表述为线上既有能力。

## 0.2 AI 实现输出要求
每个开发阶段至少应产出：

```latex
设计变更说明
接口和数据结构
数据库 migration
核心实现
单元测试
集成测试
可观测性字段
兼容与回滚说明
```

## 0.3 为什么当前学校项目采用简化记忆方案
当前采用“滑动窗口 + 摘要 + Redis”管理短期记忆，采用“主动录入 + Pinecone 向量召回”管理长期记忆，是一个适合学校项目阶段的选择。

主要考虑不是追求最完整的生产架构，而是优先验证三个核心假设：

1. 多轮对话是否能够保持基本连贯。
2. 跨会话记忆是否能够改善个性化体验。
3. RAG 和查询重写是否能够提高医学问答效果。

选择简化方案的原因：

+ 开发周期有限，需要先完成可运行的端到端系统。
+ 团队规模较小，不适合同时维护事件系统、工作流、缓存和多个数据存储。
+ Redis 读写简单、延迟低，适合保存热点会话。
+ Pinecone 已经同时用于知识检索，可以复用技术栈完成长期记忆验证。
+ 学校项目的数据规模和并发较小，复杂的一致性和恢复问题不容易出现。
+ 项目重点是证明功能和效果，而不是一次性完成医疗生产系统全部治理能力。

当前方案的主要限制：

+ Redis 中的消息和摘要容易同时承担缓存与事实存储职责。
+ 摘要生成错误后缺少可靠的原始状态重建机制。
+ Pinecone 相似结果无法表示患者事实是否已确认、过期或被撤回。
+ 多 Agent 并发写状态时缺少统一版本和冲突控制。
+ 很难完整回答一次回答使用了哪些历史事实和记忆版本。

继续推进时不建议一次性重写，而应按照以下顺序渐进改造：

```latex
第一步
保留 Redis 和现有消息结构
增加 PostgreSQL Event Log

第二步
从 Event Log 生成 Model Surface
Redis 改为 Surface 缓存

第三步
增加结构化患者记忆表
Pinecone 改为长期记忆搜索索引

第四步
增加 Memory Curator、候选确认、版本和冲突处理

第五步
增加 Agent Scope、Task DAG、Mailbox 和恢复机制
```

这样可以保持现有系统持续可用，并在每一步独立验证收益。

## 0.4 从深挖题库反向优化出的架构决策
110 道深挖题中，以下问题能够直接形成系统模块、协议或验收门禁，因此纳入 V3.0：

| 题目范围 | 暴露的问题 | V3.0 架构优化 |
| --- | --- | --- |
| 1～8 | 知识更新、过期、冲突和发布 | 版本化 Ingestion、影子索引、发布门禁和回滚 |
| 9～16、101～110 | 查询重写、幻觉和证据一致性 | Query Rewrite Protocol、双路召回和 Citation Gate |
| 17～24 | JSON 截断、断线和重启恢复 | Generation Protocol、流式 Event Seq 和恢复状态机 |
| 25～32、68～72 | 上下文压缩和跨回答记忆 | Event Log、Surface、Checkpoint 和权威长期记忆 |
| 33～45、51～60 | Workflow、动作循环和空转 | Agent Run State Machine、Action Protocol 和进展检测 |
| 46～47、66～67 | 提示词攻击、沙箱和 MCP 安全 | Policy Enforcement Point 和分层沙箱 |
| 61～67 | Skill 知识和 MCP | Skill Manifest、Invocation Protocol 和 MCP Gateway |
| 73～80 | AI 大规模生码风险 | Design Unit、Code-to-Spec 和分阶段验收 |
| 81～100 | Agent Gateway、可观测性和缓存成本 | 控制面、预算路由、SLO、缓存诊断和成本门禁 |


以下问题主要用于解释设计取舍，不单独新增模块：

+ 为什么使用 RAG、BM25、BGE 或多 Agent。
+ Chunk、TopK 和模型参数的概念解释。
+ 指标公式和面试表达方式。

这些内容仍保留在题库中，但实现时统一落到上述模块和门禁。

## 1. 项目概述
GopherMind 是一个面向健康咨询和医疗辅助场景的大语言模型应用，支持结构化问诊、多轮对话、症状分析、风险分诊、医学知识检索、药物信息及相互作用核验，并通过短期和长期记忆提供连续、个性化的用户体验。

本次升级的重点不是简单增加多个 Agent，而是建设一套：

+ 可靠的 Agent 任务执行机制。
+ 可恢复的多 Agent 协作机制。
+ 可审计的医疗决策和工具调用链路。
+ Agent 级上下文、工具和患者数据隔离机制。
+ 原始记录、工作记忆和患者长期记忆相互分离的记忆体系。
+ 可定位、可评估、可降级的 RAG 检索链路。
+ 有并发预算、资源限制和故障恢复能力的生产运行架构。

系统定位为健康咨询和临床辅助能力，不替代医生诊断。诊断、处方、剂量调整、紧急分诊等高风险输出需要根据实际产品边界配置人工审核和医疗安全策略。

## 2. 核心设计目标
### 2.1 功能目标
+ 支持结构化问诊和动态追问。
+ 支持多轮上下文连续对话。
+ 支持症状归纳、风险提示和红旗症状识别。
+ 支持医学指南、药品说明书和知识库检索。
+ 支持药物禁忌和相互作用检查。
+ 支持患者偏好、病史、过敏史和用药信息的长期记忆。
+ 支持多个专业 Agent 并行或按依赖协作。

### 2.2 工程目标
+ Agent、工具、模型或服务异常后任务可恢复。
+ 同一任务重复执行不会重复产生外部副作用。
+ 每个结论可以追溯到输入、证据、工具和 Agent。
+ 每个 Agent 只能访问职责所需的数据和工具。
+ 原始会话不会因为摘要或窗口截断而丢失。
+ 长期记忆不会把模型推断直接当作患者事实。
+ 批量任务不会无限创建 goroutine 或耗尽内存。

## 3. 总体架构
```latex
用户端 / 医生端
        |
API Gateway
认证、租户、患者授权、限流、幂等
        |
Conversation Service
        |
Agent Gateway / Control Plane
模型路由、Workflow 路由、Skill/Tool/MCP Registry
Scope、Policy、Token/Cost Budget、Trace
        |
Medical Orchestrator / Execution Plane
LangGraph + Agent Runtime + Hook Pipeline
Task DAG + Durable Mailbox + Checkpoint
        |
+---------------+---------------+----------------+-------------------+
| Intake Agent  | Triage Agent  | Evidence Agent | Medication Agent  |
+---------------+---------------+----------------+-------------------+
        |                |                |                 |
        +----------------+----------------+-----------------+
                                 |
                       Final Safety Agent
                                 |
                         Response Agent
                                 |
                       用户回答 / 人工接管

Data and Capability Plane：
Clinical Event Log
Session Projection + Model Surface + Compaction
Patient Long-term Memory
RAG Ingestion + Query Rewrite + Hybrid Retrieval + BGE
Skill Registry + Knowledge Profile
Tool Provider + MCP Gateway
Observability + Evaluation + Audit
```

### 3.0.1 三层职责
| 层次 | 主要职责 | 不负责 |
| --- | --- | --- |
| Control Plane | 路由、Registry、Scope、Policy、预算、熔断和版本 | 保存完整会话事实 |
| Execution Plane | LangGraph 节点、Agent 循环、Task DAG、Hook 和恢复 | 充当患者事实权威库 |
| Data and Capability Plane | Event、Memory、RAG、Skill、Tool、MCP、评测和审计 | 自主决定医疗风险策略 |


控制面尽量无状态；执行状态由 Checkpoint、Task 和 Mailbox 保存；事实与知识由 Event Log、权威记忆库和版本化知识库保存。这样 Agent Gateway 或执行实例重启后，可以由其他实例恢复，而不会把会话绑定到单机内存。

### 3.1 当前 DeepSeek 模型路由
> 项目当前统一使用 DeepSeek。下面是当前的任务路由设计；模型版本、价格和窗口参数必须放入配置中心，不能写死在业务代码中。
>

| 模型 | 当前版本 | 上下文上限 | 最大输出 | GopherMind 中的定位 |
| --- | --- | ---: | ---: | --- |
| `deepseek-flash` | DeepSeek-V4.1-Flash | 1M Token | 384K Token | 高频、低风险、结构化和低延迟任务 |
| `deepseek-v4-pro` | DeepSeek-V4-Pro-0813 | 1M Token | 384K Token | 复杂医学综合、药物安全和高风险复核 |


两者都支持 Thinking、JSON Output 和 Tool Calls。Flash 还支持视觉输入，因此后续处理处方、检验单和报告图片时优先使用 Flash 做识别与结构化，再把结构化结果交给 Pro 复核。

模型不按 Agent 名称固定，而按任务风险和复杂度路由：

| 任务 | 推荐模型 | Thinking | 原因 |
| --- | --- | --- | --- |
| 意图分类、字段提取、查询改写 | Flash | 关闭 | 任务边界清晰，JSON Schema 可校验 |
| 对话摘要、Memory Curator 候选提取 | Flash | 关闭 | 高频后台任务，需要控制成本 |
| 普通健康知识解释 | Flash | 关闭或低档 | 低风险、低延迟 |
| 复杂病史和多证据综合 | Pro | 中档 | 需要跨证据推理 |
| 用药安全检查 | 规则引擎 + Pro | 中高档 | 先用确定性规则拦截，再让模型解释 |
| 最终高风险回答审核 | Pro | 高档 | 重点检查证据、禁忌和遗漏 |
| 红旗症状和急诊分流 | 规则引擎 + 人工 | 不依赖模型决策 | LLM 只负责解释，不作为唯一分流依据 |


风险路由：

```latex
L0 一般医学知识
  -> Flash 快路径

L1 个性化但低风险的建议
  -> Flash + RAG + Safety Check

L2 药物、孕妇、儿童、老人、慢病等高风险问题
  -> Pro 证据综合 + Pro 安全复核

L3 胸痛、呼吸困难、意识障碍等红旗信号
  -> 确定性分流规则 + 人工/急救提示
```

### 3.2 上下文窗口和项目运行预算
1M 是模型的物理上限，不是每次请求都应使用的长度。超长上下文会增加输入费用、首 Token 延迟、注意力稀释和故障重试成本。GopherMind 为不同任务设置更小的软预算：

| 场景 | 请求总预算 | 最大输入 | 输出预留 |
| --- | ---: | ---: | ---: |
| 分类、改写、抽取、摘要 | 8K | 7K | 1K |
| 普通问诊 | 32K | 28K | 4K |
| 复杂问诊和多 Agent 综合 | 64K | 56K | 8K |
| 超长病史或多份医疗文档 | 128K | 112K | 16K |


只有当 128K 仍不能覆盖任务，并且检索和分层摘要无法解决时，才提高单次预算。实际 Token 数以模型 API 返回的 usage 为准，不用字符数粗略代替。

### 3.3 DeepSeek 计费和缓存
DeepSeek 按输入缓存命中、输入缓存未命中和输出 Token 分别计费。当前高峰价格如下：

| 模型 | 缓存命中输入 / 1M | 缓存未命中输入 / 1M | 输出 / 1M |
| --- | ---: | ---: | ---: |
| Flash | $0.006 | $0.30 | $1.20 |
| Pro | $0.044 | $1.32 | $3.96 |


工作日 UTC 01:00-04:00 和 06:00-10:00 为高峰，其余时段约为半价。单次调用成本估算：

```latex
cost =
  cache_hit_tokens  / 1,000,000 * hit_price
+ cache_miss_tokens / 1,000,000 * miss_price
+ output_tokens     / 1,000,000 * output_price
```

例如高峰期一次请求包含 20K 命中输入、8K 未命中输入和 2K 输出：

```latex
Flash = 20K/1M*0.006 + 8K/1M*0.30 + 2K/1M*1.20
      = $0.00492

Pro   = 20K/1M*0.044 + 8K/1M*1.32 + 2K/1M*3.96
      = $0.01936
```

因此优化重点不是盲目删除所有历史，而是同时做到：

+ 稳定前缀尽量命中缓存。
+ 动态证据和近期消息不超过任务预算。
+ 高频简单任务走 Flash。
+ 输出使用结构化 Schema 和长度限制。
+ Pro 只处理确实需要复杂推理或安全复核的步骤。

### 3.4 Agent Gateway
API Gateway 负责外部 HTTP 接入、认证、租户、限流和协议转换；Agent Gateway 是 Agent Runtime 的逻辑控制面，负责：

```latex
模型和 Workflow 路由
Agent、Skill 和 Tool Registry
MCP Gateway
Scope、Capability 和风险策略
Token、并发和成本预算
超时、重试、熔断和降级
Trace、审计和使用量采集
```

早期作为 Go Agent Runtime 内部模块实现，避免增加不必要的网络跳数；当 Agent、模型和 MCP 服务规模扩大后，可以拆成独立服务。

网关实例尽量无状态。会话、任务和恢复状态保存到 PostgreSQL Event Log、LangGraph Checkpointer、Task/Mailbox 和 Redis。请求使用 Run ID、Session ID、幂等键和 Lease，使实例故障后能够由其他实例继续处理。

Agent Gateway 的限流对象不仅是入口请求，还包括一次请求 fan-out 后产生的 Agent Step、LLM、Skill、Tool、RAG 和 MCP 调用。权限路由使用可信的 TenantScope 和 PatientScope，模型不能通过 Prompt 修改数据访问边界。

## 4. Cordis 和 DeepSeek Harness 的学习与借鉴
这一部分明确说明方案中哪些设计来自 Cordis 和 DeepSeek Harness，以及它们如何映射到 Go 项目。

本方案不直接复制 Cordis TypeScript 框架，而是学习它的运行时约束，在 Go 中实现更小、更适合 GopherMind 的 Agent Runtime。

### 4.1 学习点一：组件声明和依赖驱动启动
#### 学习源码
```latex
../deepseek-harness/vendor/cordis/src/registry.ts
../deepseek-harness/vendor/cordis/src/fiber.ts
```

Cordis 插件可以声明依赖：

```typescript
export const inject = ['tools', 'llm']

export function apply(ctx: Context) {
  // 注册服务、工具和事件
}
```

Cordis 会将 `inject` 转换成依赖表，为插件创建 Fiber。只有依赖服务都可用时，插件才会启动。

```latex
缺少依赖
   |
PENDING
   |
依赖满足
   |
ACTIVE
   |
依赖被替换或卸载
   |
UNLOADING
```

Fiber 的 epoch 包含依赖服务对应的 Fiber 身份。当服务实现发生变化时，消费者会重新加载，避免继续使用旧服务实例。

#### GopherMind 中的借鉴
Go 中定义组件规范：

```go
type Capability string

type ComponentSpec struct {
    Name     string
    Requires []Capability
    Provides []Capability
    Start    func(scope *Scope) error
}
```

示例：

```go
var EvidenceAgentComponent = ComponentSpec{
    Name: "evidence-agent",
    Requires: []Capability{
        "medical-knowledge.search",
        "llm.generate",
        "event-log.append",
    },
    Provides: []Capability{
        "agent.evidence",
    },
    Start: startEvidenceAgent,
}
```

组件启动流程：

1. 检查依赖能力是否存在。
2. 创建组件 Scope。
3. 在 Scope 中执行初始化。
4. 初始化全部成功后发布组件。
5. 初始化失败时回滚所有已经注册的副作用。
6. 组件停止时先停止接收新任务，再回收资源。

医疗项目不一定需要实现 Cordis 完整的热更新 Fiber，但必须实现“依赖校验、启动事务、失败回滚、统一停止”。

### 4.2 学习点二：副作用自动回收
#### 学习源码
```latex
../deepseek-harness/vendor/cordis/src/fiber.ts
../deepseek-harness/vendor/cordis/src/events.ts
```

Cordis 的 `ctx.effect()` 将副作用与 Context 生命周期绑定：

```typescript
ctx.effect(() => {
  const timer = setInterval(refresh, 10000)
  const connection = connect()

  return () => {
    clearInterval(timer)
    connection.close()
  }
})
```

Cordis 具备以下特性：

+ effect 可以返回同步或异步 disposer。
+ 一个 effect 可以产生多个 disposer。
+ disposer 按注册顺序的逆序执行。
+ 重复 dispose 是幂等操作。
+ Context 失效后不能继续注册 effect。
+ `ctx.on()` 等事件监听内部也属于 effect。
+ 子插件本身也是父插件的 effect。

#### GopherMind 中的借鉴
Go 中实现 Scope：

```go
type Disposer func(context.Context) error

type Scope struct {
    context.Context
    cancel       context.CancelFunc
    parent       *Scope
    identity     ScopeIdentity
    capabilities map[Capability]any
    disposers    []Disposer
}

func (s *Scope) Effect(
    setup func(context.Context) (Disposer, error),
) error

func (s *Scope) Dispose(ctx context.Context) error
```

必须注册到 Scope 的资源：

+ goroutine。
+ Redis Pub/Sub 订阅。
+ 数据库连接或事务。
+ timer 和 ticker。
+ LLM 流式连接。
+ 工具注册。
+ 消息消费者。
+ 临时文件。
+ 子 Agent。

销毁顺序：

```latex
停止接收新任务
    |
取消 Request Context
    |
等待飞行中的任务结束或超时
    |
关闭子 Agent
    |
关闭消息消费者
    |
关闭连接和临时资源
    |
注销工具和能力
```

这解决多 Agent 系统中常见的 goroutine 泄漏、订阅泄漏、重复消费者和 Agent 销毁后工具仍然可见的问题。

### 4.3 学习点三：Context 隔离和 Agent Scope
#### 学习源码
```latex
../deepseek-harness/vendor/cordis/src/context.ts
../deepseek-harness/packages/core/scope/src/index.ts
../deepseek-harness/packages/core/scope/src/store.ts
../deepseek-harness/packages/core/agent-loop/src/agent.ts
```

Cordis 的 `ctx.isolate()` 可以为同名服务创建不同作用域。

DeepSeek Harness 在 Cordis 上增加 Agent Scope：

```typescript
this.scope = createScope(loopCtx, this)
this.ctx = this.scope.ctx
```

每个 Agent 使用自己的对象身份作为 Scope Key。通过 `agent.ctx` 注册的工具、提示词、监听器和限制只对该 Agent 生效。

Scoped Registry 按以下顺序合并：

```latex
全局能力
   |
父 Scope 能力
   |
Team Scope 能力
   |
当前 Agent Scope 能力
```

越靠近当前 Agent 的配置优先级越高。

#### GopherMind 中的借鉴
定义 Scope 层级：

```latex
ApplicationScope
  |
TenantScope
  |
PatientScope
  |
SessionScope
  |
TeamScope
  |
AgentScope
  |
RequestScope
```

各层职责：

| Scope | 保存内容 |
| --- | --- |
| ApplicationScope | 全局模型、日志、数据库、公共医学知识能力 |
| TenantScope | 租户配置、配额、模型路由和合规策略 |
| PatientScope | 患者身份、同意范围、数据访问边界 |
| SessionScope | 当前会话、事件日志、短期记忆和图执行 |
| TeamScope | Agent 成员、Task DAG 和 Mailbox |
| AgentScope | Agent 提示词、工具、限制和私有上下文 |
| RequestScope | 超时、取消、trace、单次调用预算 |


隔离规则：

+ Agent 不直接接收用户传入的 patient ID 作为数据权限依据。
+ patient ID 必须来自已经认证的 PatientScope。
+ Evidence Agent 不能写患者长期记忆。
+ Response Agent 不直接调用病历写工具。
+ Memory Curator 只能创建候选记忆，不能绕过确认流程。
+ Team Agent 只能看到自己的 Session Surface 和结构化任务结果。

### 4.4 学习点四：Waterfall 权限拦截
#### 学习源码
```latex
../deepseek-harness/vendor/cordis/src/events.ts
../deepseek-harness/packages/core/tools/src/index.ts
```

Cordis Waterfall 采用洋葱式执行：

```typescript
ctx.on('tools/pre-execute', async (exec, next) => {
  if (!allowed(exec)) {
    return { kind: 'deny', reason: 'permission denied' }
  }
  return next()
})
```

监听器必须调用 `next()` 才会继续执行。不调用就会截断后续操作。

DeepSeek Harness 在以下位置提供拦截：

+ `agent/pre-step`
+ `agent/request`
+ `llm/stream`
+ `tools/pre-execute`
+ `tools/execute`
+ `tools/post-execute`

#### GopherMind 中的借鉴
Go 中实现 Tool Middleware：

```go
type ToolHandler func(
    ctx context.Context,
    call ToolCall,
) (ToolResult, error)

type ToolMiddleware func(
    ctx context.Context,
    call ToolCall,
    next ToolHandler,
) (ToolResult, error)
```

工具执行链：

```latex
身份认证
  |
Tenant 校验
  |
Patient Scope 校验
  |
Agent Capability 校验
  |
Purpose 和 Consent 校验
  |
输入 Schema 校验
  |
医疗风险策略
  |
人工审批
  |
实际工具执行
  |
输出 Schema 校验
  |
PHI 脱敏
  |
审计事件
```

需要特别说明：

+ Cordis 的 `ctx.intercept()` 本身不是安全权限系统。
+ 真正的权限控制需要 Agent Scope、工具可见性、pre-execute 策略和底层 Provider 强制执行共同完成。
+ Prompt 中声明“不能调用某工具”不能替代工具注册和权限校验。

### 4.5 学习点五：Session Event Log 和 Surface
#### 学习源码
```latex
../deepseek-harness/packages/core/session/src/surface.ts
../deepseek-harness/packages/core/session/src/index.ts
../deepseek-harness/packages/core/agent-loop/src/inbox.ts
```

DeepSeek Harness 将会话分为：

```latex
Append-only Session Log
          |
Surface Projection
          |
deriveMessages()
          |
LLM Request
```

Session Log 保存全部事件，但只有以下事件进入模型可见 Surface：

+ `system/message`
+ `user/message`
+ `assistant/message`
+ `tool/result`

任务边界、错误、尝试记录、团队消息状态等事件可以用于恢复和审计，但不必全部发送给模型。

#### GopherMind 中的借鉴
GopherMind 不再把 Redis 中的消息数组作为唯一会话事实，而是增加 Clinical Event Log。

```go
type ClinicalEvent struct {
    EventID      string
    TenantID     string
    PatientID    string
    SessionID    string
    CaseID       string
    AgentID      string
    Seq          int64
    Type         string
    SchemaVersion int
    Payload      json.RawMessage
    CreatedAt    time.Time
}
```

典型事件：

```latex
conversation.started
user.message
agent.task.created
agent.task.claimed
agent.message.queued
agent.message.delivered
tool.started
tool.completed
assistant.message
memory.candidate
memory.confirmed
compaction.started
compaction.summary
compaction.completed
conversation.completed
```

### 4.6 学习点六：Compaction
#### 学习源码
```latex
../deepseek-harness/packages/compaction/compaction-basic/src/index.ts
../deepseek-harness/packages/compaction/compaction-basic/src/region.ts
../deepseek-harness/packages/compaction/compaction-basic/src/summarizer.ts
```

DeepSeek Harness 的压缩机制不会删除原始事件，而是生成一个摘要节点替换模型可见的旧 Surface 区域：

```latex
原始日志：A B C D E F G
模型视图：A Summary(B-E) F G
```

压缩使用：

```latex
compaction/start
compaction/summary
compaction/end
```

形成持久化事务，并在异步摘要结束后检查 Surface 是否已经发生变化，避免旧摘要覆盖新消息。

#### GopherMind 中的借鉴
短期记忆采用相同的“原始事实不删除、模型视图可替换”原则，详细方案见第 6 章。

### 4.7 学习点七：Durable Mailbox 和 Task DAG
#### 学习源码
```latex
../deepseek-harness/packages/experimental/agent-team/src/mailbox.ts
../deepseek-harness/packages/experimental/agent-team/src/task-board.ts
../deepseek-harness/packages/experimental/agent-team/src/projection.ts
```

DeepSeek Harness Team Mailbox：

1. 先写入 `team/message/queued`。
2. flush 后尝试投递。
3. 目标 Session 保存 message ID。
4. Lead 写入 `team/message/delivered`。
5. 服务恢复后重试 queued 减 delivered 的消息。
6. 目标通过 message ID 去重。

Task Board 使用：

+ `blockedBy` 表示任务依赖。
+ `revision` 表示任务版本。
+ `expectedRevision` 实现 CAS。
+ 旧版本写入会被拒绝。

#### GopherMind 中的借鉴
用于解决：

+ Agent 并行执行。
+ Agent 之间可靠通信。
+ 服务重启后恢复任务。
+ 消息重复投递时不重复执行。
+ 多个 Agent 同时写任务状态时防止覆盖。

## 5. 多 Agent 协作设计
### 5.1 Agent 划分
| Agent | 主要职责 | 可访问能力 |
| --- | --- | --- |
| Lead Orchestrator | 拆解任务、创建 DAG、汇总结果 | 任务管理、结果读取 |
| Intake Agent | 结构化问诊和动态追问 | 最小患者资料、问诊表 |
| Triage Agent | 红旗症状和风险分级 | 分诊规则、急诊知识 |
| Evidence Agent | 医学知识和指南检索 | RAG、医学知识库 |
| Medication Safety Agent | 禁忌、相互作用和特殊人群检查 | 药物库、规则引擎 |
| Final Safety Agent | 发布前医疗安全审核 | 所有结构化结果，只读 |
| Response Agent | 生成用户可理解的回答 | 审核后的结构化结果 |
| Memory Curator | 提取长期记忆候选 | Event Log、候选记忆写入 |


### 5.2 为什么不让一个 Agent 做全部工作
+ 问诊、检索、药物判断和回答生成关注点不同。
+ 单 Agent Prompt 过长后容易发生指令冲突。
+ 所有工具都给一个 Agent 会扩大权限范围。
+ 无法判断错误来自检索、推理、药物规则还是表达。
+ 多 Agent 可以针对高风险节点增加确定性规则和独立审核。

### 5.3 LangGraph 图
```latex
START
  |
Load Patient Context
  |
Lead Orchestrator
  |
  +------------------+
  |                  |
Intake Agent      Triage Agent
  |                  |
  +---------+--------+
            |
      Evidence Agent
            |
 Medication Safety Agent
            |
   Final Safety Agent
            |
      Response Agent
            |
     Commit Response
            |
 Memory Curator Async Task
            |
           END
```

紧急分支：

```latex
Triage Agent
    |
发现红旗症状
    |
停止普通问答链路
    |
生成明确行动建议
    |
人工或急诊接管
```

### 5.4 Graph State
Graph State 只保存结构化工作流状态：

```go
type ClinicalGraphState struct {
    TenantID   string
    PatientID  string
    SessionID  string
    CaseID     string
    Revision   int64

    ChiefComplaint  string
    IntakeResult    *IntakeResult
    TriageResult    *TriageResult
    EvidenceResult  *EvidenceResult
    MedicationCheck *MedicationCheck
    SafetyResult    *SafetyResult

    PendingTasks   []TaskRef
    CompletedTasks []TaskRef
    FinalAnswer    string
}
```

Graph State 不保存：

+ 所有 Agent 的完整对话历史。
+ 大量 RAG 原始文档。
+ 模型内部隐藏推理。
+ 未确认的患者长期事实。
+ 完整工具二进制结果。

### 5.5 Task DAG
```go
type AgentTask struct {
    TaskID           string
    CaseID           string
    Type             string
    Status           string
    OwnerAgentID     string
    BlockedBy        []string
    Revision         int64
    Attempt          int
    Deadline         time.Time
    IdempotencyKey   string
    InputRef         string
    OutputRef        string
    ErrorCode        string
}
```

更新方式：

```sql
UPDATE agent_task
SET status = $1,
    revision = revision + 1
WHERE task_id = $2
  AND revision = $3;
```

影响行数为零表示任务已经被其他 Agent 更新，需要重新读取，不能覆盖。

### 5.6 Durable Mailbox
```go
type AgentMessage struct {
    MessageID      string
    TeamID         string
    SenderAgentID  string
    TargetAgentID  string
    TaskID         string
    Status         string
    PayloadRef     string
    IdempotencyKey string
    CreatedAt      time.Time
    DeliveredAt    *time.Time
}
```

投递流程：

```latex
写 message.queued
    |
事务提交
    |
投递目标 Agent
    |
目标持久化 message receipt
    |
写 message.delivered
    |
目标执行任务
    |
写 task.completed 或 task.failed
```

`delivered` 只代表目标已经持久接收，不代表任务已经成功。

### 5.7 动态 Workflow、执行模式和 Skill
Lead Orchestrator 根据问题风险和复杂度选择受约束的 Workflow：

```latex
普通医学知识
  -> Evidence
  -> Response

复杂用药问题
  -> Intake
  -> Evidence
  -> Medication Safety
  -> Final Safety
  -> Response

红旗症状
  -> Triage
  -> Emergency Escalation
```

执行模式：

+ 短任务、少量工具、下一步依赖即时观察时使用有限 ReAct。
+ 多步骤、存在依赖、需要独立恢复和审核时使用 Plan-and-Execute 和 Task DAG。
+ 外层 Task DAG 管理任务边界，内部 Agent 可执行有最大步骤限制的 ReAct。

Skill 是版本化、输入输出明确、可测试的稳定能力，例如医学文档解析或药物相互作用检查；Agent 负责根据目标选择 Skill 和工具。成功 Trace 只有经过人工提炼、Schema 定义、权限审查和回归评测后才能升级为 Skill，不能把一次执行轨迹直接投入生产。

### 5.8 Agent 内部循环和动作观测
Agent 单步遵循：

```latex
State
  -> Decide
  -> Validate
  -> Act
  -> Observe
  -> Commit State
  -> Continue / Finish / Escalate
```

DeepSeek 只输出结构化动作，例如 `ask_user`、`call_skill`、`call_tool`、`delegate_task`、`return_result` 或 `escalate_human`。所有 Skill 和 Tool 必须经过 Runtime 统一入口，禁止绕过权限、幂等和审计直接调用底层实现。

动作观测至少覆盖 Run、Graph Node、Agent Step、LLM Call、Skill Invocation、Retrieval Stage、Tool/MCP Call、State Transition、Compaction 和 Output Commit。每次 Skill 触发生成 `skill_invocation_id`，并记录 `skill.selected`、`skill.started`、`skill.completed` 或 `skill.failed`。保存结构化动作、输入输出引用、Token、延迟和错误码，不保存模型隐藏推理。

空转检测根据任务状态、新增事实、工具结果和完成子任务生成 `progress_fingerprint`。连续多步无变化或重复相同 `skill_id + arguments_hash` 时拒绝重复动作，并触发策略切换、追问、降级或人工接管。

### 5.9 Skill 知识和 MCP 接入
Skill Manifest 声明知识范围、检索参数、版本和允许的 MCP Capability。稳定短规则放在 Skill 配置中，小型随版本发布的知识放在 `references/`，大规模或频繁更新知识通过受限 RAG Retriever 获取。

MCP 接入支持：

+ 将 MCP Tool 适配为内部 Tool，Skill 不感知底层协议。
+ 使用集中式 MCP Gateway 统一管理连接、权限、限流、审计和凭证。
+ 在 Skill Scope 中启动私有 MCP Client，用于专属凭证或高隔离服务。
+ 将 MCP Resource 接入文档解析和 RAG。
+ 将长耗时 MCP 调用包装为异步 Task，通过 Mailbox 恢复。

默认采用“集中式 Gateway + 内部 Tool 适配”。MCP 返回内容仍然是不可信输入，必须经过 Schema、患者范围、提示词注入、PHI 和输出大小检查。

### 5.10 Agent Run 状态机和协议
Agent Run 使用显式状态机，不通过聊天消息猜测执行状态：

```latex
created
  -> loading_context
  -> routing
  -> running
       -> waiting_tool
       -> waiting_agent
       -> waiting_user
       -> waiting_human
  -> validating
  -> completed

任意非终态
  -> retry_scheduled
  -> failed
  -> cancelled
  -> expired
```

核心数据结构：

```go
type AgentRun struct {
    RunID          string
    TenantID       string
    PatientID      string
    SessionID      string
    WorkflowID     string
    WorkflowVersion string
    Status         string
    Revision       int64
    CurrentNode    string
    StepCount      int
    MaxSteps       int
    Deadline       time.Time
    TokenBudget    int64
    CostBudget     float64
    LastCheckpoint string
    ErrorCode      string
}

type AgentAction struct {
    ActionID        string
    RunID           string
    AgentID         string
    Step            int
    Type            string
    ReasonCode      string
    TargetID        string
    Arguments       json.RawMessage
    InputSchema     string
    ExpectedOutput  string
    IdempotencyKey  string
    CreatedAt       time.Time
}

type SkillInvocation struct {
    InvocationID   string
    RunID          string
    TaskID         string
    AgentID        string
    SkillID        string
    SkillVersion   string
    TriggerType    string
    KnowledgeVersion string
    Status         string
    InputRef       string
    OutputRef      string
    Attempt        int
    ErrorCode      string
}
```

允许的 Action 类型固定为：

```latex
ask_user
call_skill
call_tool
delegate_task
return_result
escalate_human
```

DeepSeek 输出必须先通过 Action Schema、Scope、Capability、预算和风险校验，再进入执行器。`ReasonCode` 用于审计和评测，不保存隐藏 Chain of Thought。

状态更新使用 `revision + CAS`。每次 Action、Observation 和状态迁移写入 Event Log；相同幂等键的 Action 重放时直接返回已提交结果。

### 5.11 统一故障分类和恢复策略
| 故障类型 | 示例 | 默认策略 |
| --- | --- | --- |
| Transient | 网络抖动、429、临时 5xx | 有上限指数退避和 jitter |
| Timeout | LLM、RAG、Tool、MCP 超时 | 取消子任务，按风险降级或重试 |
| Validation | Action、JSON、Tool Output Schema 失败 | 定向修复一次，仍失败则终止或人工 |
| Context | Token 超限、证据过大 | 去重、裁剪、Compaction 或拆子任务 |
| Dependency | MCP、药物库、知识库不可用 | 熔断、缓存/快照降级或人工 |
| Conflict | Task revision、Memory、知识版本冲突 | 重读最新版本，不能覆盖 |
| Policy | 越权、提示词攻击、高风险违规 | 立即拒绝并审计 |
| No Progress | 重复 Action、循环委派、状态不变 | 中断循环，换策略、追问或人工 |
| Permanent | 参数错误、资源不存在 | 不自动重试 |


每个 Task 同时限制单次超时、最大尝试次数和总 Deadline。重试前必须判断副作用是否已经提交；高风险服务失败时不能静默切换为低质量结论。

## 6. 上下文和短期记忆设计
### 6.1 必须区分的六种状态
| 状态 | 作用 |
| --- | --- |
| Runtime Context | 工具、服务、连接、取消、权限和生命周期 |
| LangGraph State | 工作流节点和 checkpoint |
| Clinical Event Log | 不可变会话事实和审计记录 |
| Agent Model Surface | 当前 Agent 真正发送给 LLM 的内容 |
| Team State | Task DAG、Mailbox、Agent 成员 |
| Patient Long-term Memory | 跨会话的患者事实和偏好 |


Redis、LangGraph State、消息历史和 Pinecone 不是同一种记忆。

### 6.2 短期记忆目标
短期记忆负责：

+ 当前会话连续性。
+ 最近对话细节。
+ 当前任务状态。
+ 当前 Agent 所需的其他 Agent 结果。
+ 最近工具调用结果。
+ Token 窗口控制。

短期记忆不负责保存患者长期事实。

### 6.3 短期记忆的数据流
```latex
用户消息
   |
写入 Clinical Event Log
   |
更新 LangGraph State
   |
更新 Session Surface Projection
   |
根据 Agent Scope 构建 Agent Surface
   |
检查 Token 和字节预算
   |
必要时执行 Compaction
   |
生成 LLM Request
```

### 6.4 Agent Model Surface
```go
type ModelSurface struct {
    AgentID          string
    Generation       int64
    SystemPrompt     string
    Checkpoint       *SummaryCheckpoint
    RecentMessages   []Message
    PatientMemories  []ResolvedMemory
    Evidence         []Evidence
    TaskResults      []TaskResult
    TokenCount       int
}
```

Surface 构建顺序：

1. 加载 Agent 系统 Prompt 和安全策略。
2. 加载最近有效的摘要 checkpoint。
3. 加载 checkpoint 之后的近期消息。
4. 加载当前任务需要的结构化 Agent 结果。
5. 按查询召回患者长期记忆。
6. 按当前医学问题执行 RAG。
7. 进行去重、排序、权限过滤和 Token 裁剪。
8. 冻结本次请求使用的 Surface generation。

#### 6.4.1 普通问诊的 32K 短期上下文划分
| 内容 | 预算 | 说明 |
| --- | ---: | --- |
| 稳定 System Prompt 和医疗安全策略 | 3K | Agent 职责、边界、输出规则 |
| Tool Schema | 2K | 只注入当前 Agent 有权限使用的工具 |
| 结构化历史摘要 | 4K | 较早对话压缩后的医疗状态 |
| 最近完整对话 | 8K | 保留完整 turn、纠正和未完成问题 |
| 患者长期记忆 | 3K | 仅召回与当前问题有关且状态有效的事实 |
| RAG 医学证据 | 6K | 重排后的高质量片段和来源信息 |
| 其他 Agent 结构化结果 | 2K | 只共享结论、证据 ID 和风险，不共享完整思考过程 |
| 输出预留 | 4K | 防止输入占满窗口导致回答被截断 |
| **合计** | **32K** | 普通问诊默认软上限 |


复杂问诊使用 64K：

| 内容 | 预算 |
| --- | ---: |
| 稳定 Prompt 和 Tool Schema | 6K |
| 结构化摘要 | 8K |
| 最近完整对话 | 14K |
| 患者长期记忆 | 6K |
| RAG 医学证据 | 16K |
| 其他 Agent 结构化结果 | 6K |
| 输出预留 | 8K |
| **合计** | **64K** |


预算不是平均分配。Surface Builder 按医疗重要性裁剪：

```latex
安全策略、过敏、当前用药、红旗信号
  > 当前用户问题和最近纠正
  > 高质量 RAG 证据
  > 未完成问诊状态
  > 一般历史摘要
  > 低相关旧对话和冗长工具原文
```

#### 6.4.2 缓存友好的 Surface 排列
DeepSeek 上下文缓存按共同前缀生效，因此请求内容按“稳定在前、动态在后”排列：

```latex
稳定前缀：
1. System Prompt
2. 医疗安全策略
3. 排序固定的 Tool Schema
4. 当前稳定的 Summary Checkpoint

动态后缀：
5. 最近对话
6. 患者长期记忆
7. RAG 证据
8. Agent Task Result
9. 当前用户任务
```

工程规则：

+ 相同版本的 Prompt 必须保持字节级一致，并记录 `prompt_version`。
+ Tool Schema 使用固定顺序，不把随机 ID、当前时间和 trace ID 放在前缀。
+ 新消息追加到后部，不在每一轮重写前面的历史。
+ 不要每轮都 Compaction；摘要变化会使该位置之后的缓存失效。
+ 从响应 usage 采集 `prompt_cache_hit_tokens`、`prompt_cache_miss_tokens` 和输出 Token。
+ Prompt 或工具升级时允许缓存自然失效，不为命中率保留过期安全规则。

#### 6.4.3 基于 Token 压力和价格的动态调整
触发 Compaction 不只看消息条数，而是同时看窗口和成本：

```latex
projected_total_tokens > operational_limit * 70%
OR recent_message_tokens > recent_message_budget
OR rag_or_tool_bytes > evidence_byte_budget
OR estimated_cache_miss_cost > step_cost_budget
```

调整顺序：

1. 删除无权限、重复和低相关内容。
2. 工具原文落对象存储，只在 Surface 保留结构化结果和引用。
3. 降低 RAG `top_k`，保留重排后证据。
4. 压缩较早完整 turn，保留最近 tail。
5. 将低风险步骤从 Pro 路由到 Flash。
6. 仍超预算时拆成多个子任务，而不是硬塞进 1M 窗口。

后台摘要、embedding 重建、离线评估和 Memory Curator 批处理可优先安排在非高峰时段。按中国标准时间，官方工作日高峰约为 09:00-12:00 和 14:00-18:00；在线问诊不能为了半价等待，只有非实时任务才允许错峰。

不能只为了省钱牺牲医疗安全信息。过敏、当前用药、禁忌、红旗症状、用户纠正和证据出处属于不可自动丢弃区。

#### 6.4.4 用 Hook 强制执行缓存和记忆策略
这里的 Hook 不是 React Hook，而是 Agent Runtime 的生命周期拦截点。DeepSeek Harness 基于 Cordis Waterfall 暴露了以下关键 Hook：

| DeepSeek Harness Hook | 作用 | GopherMind 中的借鉴 |
| --- | --- | --- |
| `system-prompt/assemble` | 组装系统提示词、上下文段和工具 | 构建稳定前缀、固定 Tool Schema 顺序 |
| `agent/pre-step` | 模型步骤开始前修改或拒绝消息 | 注入摘要、近期消息、患者记忆和 RAG，并执行 Token 裁剪 |
| `agent/request` | LLM 请求提交前修改调用配置 | 模型路由、Thinking、输出上限和成本预算 |
| `llm/stream` | 包裹真实模型流式调用 | 超时、trace、usage 采集和 Provider 适配 |
| `agent/request-error` | 请求失败后的决策 | 判断重试、降级模型、缩小上下文或终止 |
| `tools/pre-execute` | 工具调用前检查 | 权限、患者范围、幂等和风险拦截 |
| `tools/post-execute` | 工具调用后处理 | PHI 脱敏、结果截断、spill 和审计 |


DeepSeek Harness 的 Waterfall Hook 采用洋葱模型：

```latex
Hook A before
  -> Hook B before
       -> Runtime default action
     Hook B after
Hook A after
```

Hook 不调用 `next()` 就能拒绝后续链路。Hook 注册本身属于 Cordis Fiber 的 Effect，Agent 或插件销毁时自动注销，避免旧 Hook 重复执行或跨 Agent 污染。

GopherMind 在 Go 中定义统一 Hook Pipeline：

```go
type ModelHook interface {
    Name() string
    Order() int
    BeforeModel(ctx context.Context, req *ModelRequest) error
    AfterModel(ctx context.Context, req *ModelRequest, resp *ModelResponse) error
}

type CompactionHook interface {
    BeforeCompaction(ctx context.Context, plan *CompactionPlan) error
    AfterCompaction(ctx context.Context, result *CompactionResult) error
}
```

推荐执行顺序：

```latex
BeforeModel
1. ScopeAuthorizationHook
2. StablePrefixHook
3. SurfaceProjectionHook
4. TokenBudgetHook
5. CacheCostHook
6. RequestFreezeHook

AfterModel
1. UsageCaptureHook
2. CacheMetricHook
3. MedicalOutputValidationHook
4. EventLogHook
```

各 Hook 的职责：

+ `StablePrefixHook`：固定 Prompt 版本、规范换行、按名称排序工具并生成 `prefix_hash`。
+ `SurfaceProjectionHook`：根据 Agent Scope 注入摘要、近期消息、患者记忆和证据。
+ `TokenBudgetHook`：检查 8K、32K、64K 或 128K 软预算，超限时触发裁剪或 Compaction。
+ `CacheCostHook`：根据模型价格、历史命中率和预计 miss Token 估算本步骤成本。
+ `RequestFreezeHook`：冻结最终消息和工具列表，避免发出前被其他 goroutine 修改。
+ `UsageCaptureHook`：读取模型返回的输入、输出、推理和缓存命中 Token。
+ `CacheMetricHook`：按 `agent + model + prompt_version + toolset_version` 聚合命中率。
+ `MedicalOutputValidationHook`：检查引用、药物字段、风险提示和输出 Schema。
+ `EventLogHook`：把模型版本、Surface generation、prefix hash、usage 和审核结果写入事件日志。

缓存命中率：

```latex
cache_hit_rate =
  cache_read_tokens
  / (cache_read_tokens + cache_miss_input_tokens)
```

Hook 发现命中率下降时不直接修改生产 Prompt，而是记录原因标签：

```latex
prompt_version_changed
toolset_changed
summary_generation_changed
message_order_changed
dynamic_prefix_detected
provider_cache_cold
```

这样可以区分正常冷启动和真正的 Prompt 抖动。所有修改请求内容的 Hook 必须发生在 `RequestFreezeHook` 之前；Freeze 之后只能采集和审计，不能再改消息顺序。

### 6.5 滑动窗口
滑动窗口保留：

+ 最近 N 个完整 turn。
+ 当前未完成 turn。
+ 当前任务直接相关的工具结果。
+ 最近用户纠正的信息。
+ 最近风险和安全结果。

不能简单按照消息数量截断，需要保持：

+ Tool call 和 tool result 成对。
+ 用户问题和对应回答尽量成对。
+ 否定信息不能只保留一半。
+ 药物、剂量、频次和时间不能拆散。

### 6.6 摘要内容
医疗摘要采用结构化格式：

```yaml
chief_complaint:
symptoms:
  positive: []
  negative: []
onset_and_duration:
severity:
medical_history:
allergies:
current_medications:
special_population:
triage_risk:
evidence_used:
user_corrections:
pending_questions:
unresolved_conflicts:
```

摘要必须特别保留：

+ 用户明确否认的症状。
+ 过敏和不良反应。
+ 药品、剂量、频次和停药时间。
+ 症状开始和变化时间。
+ 用户对历史信息的纠正。
+ 红旗风险和未解决冲突。

### 6.7 Compaction 事务
```go
type CompactionCheckpoint struct {
    CompactionID  string
    SessionID     string
    AgentID       string
    StartSeq      int64
    EndSeq        int64
    BaseGeneration int64
    Summary       StructuredSummary
    Status        string
    CreatedAt     time.Time
}
```

处理流程：

1. 读取 Surface generation。
2. 计算待压缩区间。
3. 保留 system prompt 和近期 tail。
4. 保证工具调用结果不被拆开。
5. 写入 `compaction.started`。
6. 调用摘要模型生成结构化摘要。
7. 校验摘要 Schema 和关键事实。
8. 再次检查 Surface generation。
9. generation 未变化时提交 replacement。
10. 写入 `compaction.completed`。
11. generation 已变化时放弃摘要并稍后重试。

原始事件一直保留：

```latex
Event Log：
A B C D E F G

Model Surface：
A Summary(B-E) F G
```

### 6.8 Redis 的职责
Redis 保存：

+ 热点 Surface 缓存。
+ 最近摘要 checkpoint 缓存。
+ Agent 运行状态。
+ 有界 Inbox 缓存。
+ 分布式租约。
+ 限流计数器。
+ 幂等结果缓存。

Redis 不作为：

+ 原始会话唯一事实源。
+ 患者长期记忆权威库。
+ 医疗审计唯一存储。
+ 不可恢复任务的唯一队列。

### 6.9 短期记忆恢复
服务重启时：

```latex
加载 Clinical Event Log
    |
重建 Session Projection
    |
加载最近有效 checkpoint
    |
追加 checkpoint 后的事件
    |
恢复 LangGraph checkpoint
    |
恢复 queued 未 delivered 消息
    |
恢复未完成任务
```

### 6.10 JSON 截断、流式输出和历史恢复
模型输出在完整解析、Schema 校验和医疗安全校验前不能产生业务副作用。

JSON 被截断时：

```latex
保存原始片段和 finish reason
  |
标记 output_incomplete
  |
定位最后一个完整字段或数组项
  |
携带 generation_id、已完成字段和待补字段定向续写
  |
合并、去重并重新解析
  |
Schema 和安全校验
```

不使用模糊的“继续生成”，也不让模型重新生成全部内容。续写请求只携带必要摘要、Evidence ID、上一段尾部和待完成字段。

流式输出保存单调递增的 `event_seq`。客户端重连时携带 `generation_id + last_event_seq`，服务端先重放已确认事件，再继续返回。服务重启后从 Event Log、LangGraph checkpoint、任务表、Mailbox 和幂等结果恢复；未完整 Token 片段不作为已提交结果。

## 7. 长期记忆设计
### 7.1 长期记忆目标
长期记忆负责跨会话保存：

+ 患者基础信息。
+ 已确认疾病和病史。
+ 过敏和不良反应。
+ 当前或历史用药。
+ 特殊人群信息。
+ 用户交流偏好。
+ 用户主动要求记住的事项。

长期记忆不应直接保存：

+ 模型猜测出的疾病。
+ 未确认的症状归因。
+ 低置信度的情绪或性格判断。
+ Agent 隐藏推理。
+ 没有来源的药物和病史。

### 7.2 长期记忆状态
```latex
candidate
   |
规则校验
   |
pending_confirmation
   |
用户或权威数据确认
   |
confirmed
   |
后续可能进入
   +--> contradicted
   +--> expired
   +--> revoked
   +--> deleted
```

### 7.3 长期记忆数据模型
```go
type PatientMemory struct {
    MemoryID       string
    TenantID       string
    PatientID      string
    Type           string
    Value          json.RawMessage

    SourceEventID  string
    SourceType     string
    Provenance     string
    Confidence     float64

    ConsentScope   string
    Status         string
    Version        int64

    ValidFrom      time.Time
    ValidUntil     *time.Time
    ConfirmedAt    *time.Time
    RevokedAt      *time.Time

    CreatedAt      time.Time
    UpdatedAt      time.Time
}
```

### 7.4 Memory Curator
Memory Curator 在回答提交后异步执行：

```latex
读取本次新事件
   |
提取候选事实
   |
分类和 Schema 校验
   |
与现有事实对比
   |
判断新增、更新、重复或冲突
   |
写 memory.candidate
   |
必要时询问用户确认
   |
确认后写权威库
   |
异步更新向量索引
```

Memory Curator 只能创建候选，不能直接把模型输出变成已确认病史。

### 7.5 主动录入
用户可以明确要求：

```latex
请记住我对青霉素过敏。
我已经停用某药。
以后请使用中文回答。
删除我的历史用药记录。
```

主动录入流程：

1. 识别用户记忆意图。
2. 生成结构化候选。
3. 对高风险事实进行二次确认。
4. 保存来源消息。
5. 写入权威关系库。
6. 更新向量索引。
7. 返回明确的记忆结果。

### 7.6 权威关系库和 Pinecone
采用双层存储：

```latex
PostgreSQL
保存事实、版本、状态、来源、同意和有效期

Pinecone
保存 embedding、memory_id 和检索元数据
```

Pinecone 只负责找到候选记忆，不能负责判断事实当前是否有效。

召回流程：

```latex
当前问题
   |
查询改写
   |
Pinecone 召回 memory_id
   |
根据 memory_id 回查 PostgreSQL
   |
校验 tenant_id 和 patient_id
   |
校验 confirmed 状态
   |
校验有效期和 consent scope
   |
冲突消解和去重
   |
注入 Agent Surface
```

### 7.7 Namespace
```latex
tenant/{tenantId}/patient/{patientId}/demographic
tenant/{tenantId}/patient/{patientId}/condition
tenant/{tenantId}/patient/{patientId}/allergy
tenant/{tenantId}/patient/{patientId}/medication
tenant/{tenantId}/patient/{patientId}/preference
```

不能只依赖 namespace 字符串做权限隔离，回表时仍需验证 TenantScope 和 PatientScope。

### 7.8 冲突处理
示例：

```latex
旧记忆：正在服用药物 A
新消息：我已经停用药物 A 两个月
```

不能简单覆盖，需要：

1. 创建新的 memory version。
2. 将旧记录标记为 expired 或 superseded。
3. 保存新旧事实的时间关系。
4. 记录来源消息。
5. 更新向量索引。
6. 后续召回优先返回当前有效版本。

冲突无法自动解决时：

```latex
旧记录显示您正在服用药物 A，
但您刚才表示已经停药。
请确认目前是否仍在服用。
```

### 7.9 删除和撤回
删除流程：

1. 在权威库标记 revoked 或 deleted。
2. 立即停止该记忆参与召回。
3. 写审计事件。
4. 异步删除 Pinecone 向量。
5. 清理 Redis 缓存。
6. 验证后续查询不再返回该事实。

必须先使权威记录失效，再异步删除向量，避免向量库延迟导致旧事实继续生效。

## 8. RAG 设计
### 8.1 数据摄取
```latex
数据源登记
  |
文档解析
  |
章节识别
  |
递归分块
  |
医学实体和元数据抽取
  |
Embedding
  |
稀疏索引
  |
向量索引
  |
版本发布
```

分块优先保持以下医学边界：

+ 标题和章节。
+ 适应证。
+ 禁忌证。
+ 剂量和用法。
+ 特殊人群。
+ 不良反应。
+ 相互作用。
+ 参考文献。

Chunk 元数据：

```latex
document_id
document_version
section_path
source_type
published_at
valid_until
jurisdiction
population
source_grade
embedding_model
index_version
```

### 8.2 在线检索
```latex
原始问题
  |
意图和实体识别
  |
查询重写
  |
+-------------------+
| Dense Retrieval   |
| Sparse Retrieval  |
+-------------------+
  |
合并去重
  |
BGE Reranker
  |
来源、时效、地区、权限过滤
  |
Evidence Package
  |
回答生成
  |
引用正确性校验
```

#### 8.2.1 查询重写（已实现）
查询重写是当前 RAG 的独立模块，目标是将依赖对话、口语化或信息不完整的问题转换成可检索的 Standalone Query，同时保护医学语义。

输入只包含当前问题、必要的最近对话、结构化问诊结果和相关患者条件，不携带全部会话历史。使用 DeepSeek Flash 或同级低延迟模型关闭 Thinking，并要求输出：

```go
type RewrittenQuery struct {
    OriginalQuery      string
    StandaloneQuery    string
    Intent             string
    MedicalEntities    []MedicalEntity
    ProtectedTerms     []string
    Filters            QueryFilters
    SparseKeywords     []string
    SubQueries         []string
    Confidence         float64
}
```

处理步骤：

```latex
指代消解
  -> 药品、疾病和检查指标标准化
  -> 保留否定词、剂量、单位、时间和特殊人群
  -> 补充检索意图
  -> 必要时拆成 1～3 个子查询
  -> JSON Schema 和实体保持校验
  -> 原始 Query 与重写 Query 双路检索
```

基础路径以一个 Standalone Query 加 Sparse Keywords 为主；只有复杂复合问题才生成少量子查询，避免查询膨胀。当前项目不把未实现的 HyDE 描述为既有能力。

重写失败时保留原始 Query，并使用规则抽取的医学实体执行 BM25/Dense 兜底。需要记录：

```latex
raw_query
standalone_query
sub_queries
entity_before
entity_after
negation_preserved
dose_time_preserved
rewrite_confidence
rewrite_latency_ms
rewrite_fallback
recall_delta
```

评测不仅看文本是否流畅，而是观察实体保持率、否定词保持率、剂量时间保持率、Rewrite Success Rate，以及重写前后 Recall@K、MRR、延迟和 Token 的变化。

### 8.3 检索不到时如何定位
按照检索链路逐层定位：

| 层次 | 排查问题 | 需要记录的证据 |
| --- | --- | --- |
| 数据源 | 目标文档是否进入系统 | document ID、版本、发布状态 |
| 解析 | 目标内容是否解析成功 | 原文和解析文本对比 |
| 分块 | 目标信息是否被错误切碎 | chunk 内容和 section path |
| Embedding | 入库和查询模型是否一致 | embedding model、维度 |
| 索引 | 是否写入正确 namespace | index、namespace、version |
| Filter | metadata 是否误过滤 | 过滤前后候选数量 |
| Query Rewrite | 是否丢失实体、否定、剂量 | raw query、rewritten query |
| Dense Recall | 语义召回是否命中 | candidate ID 和 score |
| Sparse Recall | 关键词召回是否命中 | BM25 排名 |
| Rerank | 正确候选是否被降权 | 重排前后排名 |
| Context | 命中内容是否被 Token 裁剪 | selected evidence ID |
| Generation | 模型是否忽略证据 | Prompt 和引用结果 |


关键日志字段：

```latex
trace_id
session_id
agent_id
raw_query
rewritten_query
medical_entities
filters
knowledge_version
dense_candidates
sparse_candidates
merged_candidates
reranked_candidates
selected_evidence_ids
retrieval_latency_ms
rerank_latency_ms
context_tokens
no_answer_reason
```

离线指标：

+ Recall@K。
+ MRR。
+ nDCG。
+ Citation Coverage。
+ Citation Correctness。
+ No Answer Precision。
+ Freshness Pass Rate。
+ Safety Violation Rate。

### 8.4 知识库更新、过期和冲突
知识库采用版本化增量更新：

```latex
数据源变更
  |
文档和 Chunk Hash 对比
  |
只重新处理新增或变化的 Chunk
  |
写入影子索引
  |
完整性检查和离线回归
  |
原子切换 active index version
  |
保留上一版本用于回滚
```

每个 Chunk 保存：

```latex
document_id
document_version
content_hash
index_version
published_at
valid_from
valid_until
status
superseded_by
```

过期知识先在权威元数据中标记失效，使其立即停止参与召回，再异步清理 Pinecone 和 BM25 索引。冲突按照版本、地区、适用人群、来源等级和发布时间处理；同级权威来源无法自动消解时，保留冲突证据并进入 Final Safety 或人工审核。

同一次回答冻结 `knowledge_version`，避免一次任务同时使用两个知识库版本。

### 8.5 分块和混合检索统一参数
文档采用“结构边界优先、Token 长度兜底”的递归分块：

```latex
标题和章节
  -> 医学语义字段
  -> 段落
  -> 列表项
  -> 句子
  -> Token 长度兜底
```

推荐初始参数：

| 参数 | 初始值 |
| --- | ---: |
| 目标 Chunk | 400 Token 左右 |
| 最小 Chunk | 150 Token |
| 普通最大 Chunk | 600 Token |
| 语义完整时允许上限 | 800 Token |
| Overlap | 40～80 Token，约 10%～15% |
| Dense Retrieval | Top 20 |
| BM25 Retrieval | Top 20 |
| RRF 合并去重 | Top 30 |
| BGE Rerank | Top 10 |
| 最终注入 Surface | 4～6 条证据，并受 Token 预算约束 |


这些参数用于启动评测，不是适用于所有文档的固定最优值。最终需要在开发集上比较不同 Chunk、Overlap 和各阶段 TopK，寻找 Recall、排序质量、回答准确率、延迟和 Token 成本的拐点，并在独立测试集上报告结果。

### 8.6 生成控制和幻觉治理
幻觉治理采用多层防线：

1. 检索层只提供版本有效、权限允许的证据。
2. Prompt 要求基于 Evidence Package 回答，证据不足时追问或拒答。
3. 输出按事实、建议、风险和引用进行结构化。
4. 每个关键事实关联 Evidence ID，并检查 Citation Correctness。
5. 药物、红旗症状和高风险人群增加规则引擎或独立审核。
6. 记录 Unsupported Claim Rate、No Answer Precision/Recall 和 Safety Violation Rate。

检索不到、证据冲突或知识过期时，不允许 DeepSeek 仅依靠参数知识生成确定性诊断、处方或剂量建议。

## 9. 权限和医疗安全
### 9.1 Agent Capability
示例：

```latex
patient.basic.read
patient.history.read
clinical.form.write
medical.knowledge.search
medication.database.read
medication.interaction.check
memory.candidate.write
memory.confirmed.write
response.publish
human.review.request
```

不同 Agent 分配不同 Capability，未注册的工具不出现在该 Agent 的 LLM Tool Schema 中。

### 9.2 风险等级
| 等级 | 场景 | 策略 |
| --- | --- | --- |
| L0 | 医学术语和一般知识 | 自动回答并提供来源 |
| L1 | 个性化健康建议 | 证据支持和风险说明 |
| L2 | 药物、剂量、孕妇、儿童和慢病 | 独立安全审核或人工审核 |
| L3 | 胸痛、呼吸困难、意识异常等 | 中断普通流程并紧急分诊 |


### 9.3 输出发布屏障
```latex
Response Draft
   |
Evidence Citation Check
   |
Medication Safety Check
   |
Triage Consistency Check
   |
PHI and Policy Check
   |
Final Safety Decision
   |
Publish / Modify / Escalate
```

### 9.4 提示词攻击和 Agent 沙箱
用户输入、网页、附件和 RAG 文档全部视为不可信数据。防护不只依赖 System Prompt，而是同时执行：

+ 检索内容与系统指令分区，文档中的指令不能覆盖系统策略。
+ Agent Scope 只暴露职责所需的 Tool Schema。
+ 工具执行前校验 Capability、患者范围、Purpose、Consent、输入 Schema 和风险级别。
+ 高风险写操作需要审批、幂等键、事务和审计。
+ 数据访问使用只读视图、字段脱敏和行级权限。
+ 网络访问使用域名/IP 白名单，禁止访问内网元数据地址。
+ 文件操作限制在临时目录或只读挂载，阻止路径穿越。
+ 代码或进程执行放入容器或子进程沙箱，并限制 CPU、内存、时间和输出大小。

医疗 Agent 默认不能任意执行 Shell、访问公网或写患者权威事实。即使模型受到提示词注入，Runtime Policy 也必须阻止越权副作用。

## 10. 批量任务的 CPU 和内存治理
多 Agent 处理批量任务时可能发生 CPU 和内存打满。

常见原因：

+ 一个任务 fan-out 出过多 Agent。
+ 每个文档或患者启动一个 goroutine。
+ 大量 embedding 或 rerank 在本地执行。
+ 上下文和 RAG 候选集过大。
+ 重复 JSON marshal 和大对象复制。
+ 工具返回结果全部留在内存。
+ 重试没有上限，形成重试风暴。
+ 取消信号没有传播，用户退出后任务仍运行。
+ Agent Scope 未回收导致 goroutine、timer 或连接泄漏。

### 10.1 多层并发预算
| 层级 | 限制 |
| --- | --- |
| 全局 | 最大运行图、LLM、embedding、rerank 并发 |
| Tenant | QPS、并发会话、Token、成本 |
| Patient | 同一患者写任务串行化 |
| Session | 最大活跃 Agent 和 pending task |
| Agent | 最大步骤、工具调用、上下文和执行时间 |
| Tool | 并发模式、超时、输入输出大小和重试次数 |


### 10.2 Go 实现
+ 有界 worker pool。
+ 加权 semaphore。
+ 有界 channel。
+ `errgroup` 配合并发限制。
+ context cancellation 全链路传播。
+ 大结果写对象存储，只保留 locator 和摘要。
+ 设置 `GOMEMLIMIT` 并通过压测校准 `GOGC`。
+ 队列满时背压或拒绝，不继续创建 goroutine。

### 10.3 定位方法
1. 降低入口并发并暂停低优先级批量任务。
2. 检查 CPU、Heap、RSS、goroutine 和 GC。
3. 采集 Go pprof：
    - cpu
    - heap
    - allocs
    - goroutine
    - mutex
    - block
4. 检查每个 trace 的 Agent fan-out、步骤数和重试数。
5. 检查上下文 Token、RAG 候选数和工具结果字节数。
6. 检查队列到达率、消费率和最老任务等待时间。
7. 检查取消后仍在运行的 goroutine。
8. 修复后重新进行容量和故障压测。

## 11. 可观测性
### 11.1 Trace
```latex
conversation.request
  graph.run
    agent.intake
    agent.triage
    agent.evidence
      rag.rewrite
      rag.retrieve
      rag.rerank
    agent.medication
    agent.final_safety
    agent.response
  memory.curate
```

### 11.2 核心指标
业务指标：

+ 会话成功率。
+ 人工接管率。
+ 红旗症状识别率。
+ 引用覆盖率。
+ 用户纠正率。

Agent 指标：

+ 任务成功率。
+ 路由准确率。
+ 工具选择准确率。
+ 平均 Agent fan-out。
+ 平均步骤数。
+ 无进展步骤率和空转率。
+ 重试率。
+ 取消率。
+ 恢复成功率。
+ 重复副作用率。
+ Task DAG 完成时间。

LLM 和缓存指标：

+ 输入、输出和 Thinking Token。
+ `prompt_cache_hit_tokens`。
+ `prompt_cache_miss_tokens`。
+ Token 加权缓存命中率。
+ `prefix_hash`、`prompt_version` 和 `toolset_version` 的变化原因。
+ Time to First Token。
+ 单次请求和单个成功任务成本。

Agent Gateway 指标：

+ 活跃 Run、Session 和 Agent Step。
+ 模型、Skill、Tool 和 MCP 路由次数。
+ 各层限流和背压次数。
+ 队列等待和 Lease 超时。
+ Provider、MCP 和 Tool 熔断状态。
+ 租户 Token 和成本预算使用率。

记忆指标：

+ 候选记忆数量。
+ 确认率。
+ 拒绝率。
+ 冲突率。
+ 过期记忆召回率。
+ 删除后残留召回率。

系统指标：

+ CPU。
+ Heap 和 RSS。
+ GC CPU 和暂停。
+ goroutine 数量。
+ 队列深度。
+ 最老任务等待时间。
+ LLM 和工具 P95/P99。

安全指标：

+ 提示词攻击成功率。
+ 越权工具调用率。
+ 患者数据跨 Scope 泄漏率。
+ 高风险回答漏审率。
+ Unsupported Claim Rate。

### 11.3 SLO 和告警
SLO 按风险等级和 Workflow 分层配置，不能只使用一个全局平均值：

| SLO 维度 | 主要指标 | 告警原则 |
| --- | --- | --- |
| 正确性 | 任务成功率、路由准确率、引用正确率 | 相比当前基线显著下降 |
| 医疗安全 | 红旗召回、高风险漏审、安全违规 | 任一严重违规立即阻断发布 |
| 可靠性 | 恢复成功率、重复副作用率、消息积压 | 出现重复外部副作用立即告警 |
| 延迟 | TTFT、端到端 P95/P99、关键路径 | 按 L0～L3 和在线/离线分别设置 |
| 成本 | 单任务 Token、缓存命中率、租户成本 | 超过 Run 或 Tenant 预算 |
| 资源 | CPU、Heap、goroutine、队列年龄 | 持续超过容量水位 |
| 依赖 | DeepSeek、Pinecone、MCP、Tool 可用率 | 熔断开启或错误率持续升高 |


具体阈值必须通过现有系统基线和压测确定，不在架构文档中伪造线上数字。高风险安全指标使用零容忍或人工批准策略，不能用整体准确率抵消。

### 11.4 统一评测和发布门禁
评测分为三层：

```latex
确定性评测：
Schema、路由、Tool 参数、Recall@K、MRR、幂等和状态机

机器评测：
Faithfulness、完整性、表达质量和引用蕴含

人工评测：
医学正确性、高风险安全、冲突和边界案例
```

数据集标签至少包括：

```latex
intent
risk_level
required_agents
expected_route
expected_tools
gold_evidence
key_facts
forbidden_claims
should_abstain
human_escalation
```

发布时冻结并记录：

```latex
model_version
prompt_version
workflow_version
skill_version
toolset_version
knowledge_version
embedding_version
reranker_version
evaluation_dataset_version
```

发布流程：

```latex
开发集调参
  -> 独立测试集回归
  -> 高风险人工复核
  -> 影子流量
  -> 小流量灰度
  -> SLO 和成本检查
  -> 全量发布
  -> 可回滚版本保留
```

任何高风险安全回归、权限越权、重复副作用或恢复失败都应阻断发布，即使平均准确率有所提升。

## 12. 存储职责
| 存储 | 职责 |
| --- | --- |
| PostgreSQL | Event Log、Task、Mailbox、患者权威记忆、同意、审计 |
| Redis | Surface 缓存、租约、限流、运行态、热点 Inbox |
| Pinecone | 医学知识和患者记忆候选召回 |
| 对象存储 | 原始文档、附件、大型工具结果、评估数据 |
| LangGraph Checkpointer | 当前图执行状态和节点 checkpoint |


## 13. 推荐代码结构
```latex
cmd/gophermind-api

internal/gateway/agent

internal/runtime/component
internal/runtime/scope
internal/runtime/capability
internal/runtime/policy
internal/runtime/hooks
internal/runtime/skill
internal/runtime/run
internal/runtime/action
internal/runtime/invocation

internal/agent/orchestrator
internal/agent/intake
internal/agent/triage
internal/agent/evidence
internal/agent/medication
internal/agent/safety
internal/agent/response

internal/workflow/langgraph
internal/llm/deepseek

internal/session/eventlog
internal/session/surface
internal/session/compaction
internal/session/projection

internal/team/taskboard
internal/team/mailbox

internal/memory/curator
internal/memory/store
internal/memory/retrieval

internal/rag/ingestion
internal/rag/rewrite
internal/rag/retrieval
internal/rag/rerank
internal/rag/evaluation

internal/evaluation/agent
internal/evaluation/rag
internal/evaluation/safety
internal/evaluation/release

internal/security
internal/observability
internal/integration/mcp
```

## 14. 分阶段实施
### 14.0 AI 生成代码治理
AI 编码代理不得一次性无边界生成和提交数千行代码。所有实现必须从 Spec 提取需求清单，并拆成可以独立验证的设计单元：

```latex
需求 ID
  -> 架构决策
  -> Coding Plan
  -> 接口和数据结构
  -> 单模块实现
  -> 单元/集成/故障测试
  -> Code-to-Spec 核对
  -> 分阶段提交和回滚
```

重点审查：

+ 需求分支和异常路径是否完整。
+ 权限、事务、幂等、超时和恢复是否被绕过。
+ 是否编造不存在的 API 或依赖。
+ 数据库兼容和回滚是否成立。
+ 测试是否只验证 AI 自己的实现假设。
+ 是否发生跨模块架构漂移或覆盖已有修改。

即使编译和测试通过，也必须建立“需求 ID -> 代码位置 -> 测试用例”的双向追踪。AI 提高实现速度，但架构边界、风险判断和最终验收由工程负责人负责。

### 阶段一：上下文和事实源
+ 建立 Clinical Event Log。
+ 实现 Session Projection。
+ Redis 从唯一消息存储改为热点缓存。
+ 实现 Agent Model Surface。
+ 保留现有滑动窗口和摘要能力。

验收：

+ 原始对话可以完整回放。
+ Redis 数据丢失后可以从 Event Log 恢复。
+ 不同 Agent 看到不同 Surface。

### 阶段二：Agent Runtime 核心协议
+ 实现 ComponentSpec、Scope 和 LIFO disposer。
+ 实现 Agent Run 状态机。
+ 实现结构化 Agent Action 和 Observation 协议。
+ 实现 revision、CAS、幂等结果和统一错误码。
+ 实现统一 Hook Pipeline。
+ 实现进展指纹和空转检测。

验收：

+ Agent 创建失败不会留下半初始化资源。
+ Agent 销毁后无残留 goroutine、订阅和工具。
+ 每一步 Action、Observation 和状态迁移都可追踪。
+ 相同幂等键重放不会重复产生副作用。
+ 连续无进展步骤可以被检测并终止。

### 阶段三：Agent Gateway、Skill、Tool 和 MCP
+ 实现 Agent Gateway 逻辑控制面。
+ 实现 DeepSeek 模型、Workflow 和成本路由。
+ 实现 Capability Registry 和 Tool Middleware。
+ 实现 Skill Registry、Skill Manifest 和 Skill Executor。
+ 实现 Skill Knowledge Profile 和受限 Retriever。
+ 实现 MCP Gateway、Tool 适配和 Server 健康检查。
+ 实现全局、租户、会话、Run 和 Agent Step 预算。

验收：

+ 未授权 Agent 看不到高风险 Skill 和 Tool。
+ 每次 Skill 触发可关联 Agent Step、RAG、Tool/MCP、Token、延迟和结果。
+ MCP 服务异常受超时、熔断、幂等和降级策略控制。
+ 网关实例重启后不会丢失持久任务。
+ 超过预算的 fan-out 会被背压或拒绝。

### 阶段四：多 Agent 可靠协作
+ 实现 Lead、Intake、Triage、Evidence、Medication 和 Final Safety Agent。
+ 实现静态 Workflow、动态路由和最大执行边界。
+ 实现 Task DAG。
+ 实现 Durable Mailbox。
+ 实现消息去重、任务幂等和依赖超时。
+ 实现 ReAct 内循环最大步骤限制。

验收：

+ 服务重启后可以继续未完成任务。
+ 消息重复投递不会重复执行外部副作用。
+ 并行 Agent 不会覆盖彼此状态。
+ 循环委派和依赖死锁可以被检测。
+ 简单问题不会无必要启动全部 Agent。

### 阶段五：短期上下文和输出恢复
+ 实现 Surface generation。
+ 实现 Compaction 事务和结构化医疗摘要。
+ 实现 Token、字节和输出预留预算。
+ 实现工具结果 spill。
+ 实现 JSON 定向续写和 Schema 校验。
+ 实现流式 `generation_id + event_seq` 重连恢复。

验收：

+ 长会话 Token 可控。
+ 摘要不会删除原始事件。
+ 并发新消息不会被旧摘要覆盖。
+ JSON 截断不会产生未校验业务副作用。
+ 客户端断线和服务重启后能够恢复已提交输出。

### 阶段六：长期记忆和 RAG 治理
+ 实现 Memory Curator、candidate、confirmed 和用户确认。
+ 实现记忆版本、冲突、过期、撤回和删除。
+ 实现 PostgreSQL 权威状态与 Pinecone 向量索引。
+ 治理现有 Query Rewrite、混合检索和 BGE 参数。
+ 实现知识库增量更新、影子索引、发布和回滚。
+ 建立 Retrieval Trace 和固定离线评测集。

验收：

+ 模型推断不能直接成为患者病史。
+ 撤回或过期记忆不会继续参与回答。
+ 查询重写不丢失否定、剂量、时间和特殊人群。
+ 知识更新失败不会破坏当前线上版本。
+ 检索问题可以定位到解析、分块、召回、重排、Context 或生成阶段。

### 阶段七：安全、可观测性和发布治理
+ 实现提示词攻击检测、Tool 权限测试和分层沙箱。
+ 实现 Run、Agent、LLM、Skill、RAG、MCP、Memory 和 Cost 指标。
+ 定义分风险等级 SLO 和告警。
+ 建立确定性、机器和人工组合评测。
+ 实现影子流量、灰度、版本矩阵和自动回滚。
+ 增加 pprof、容量压测和故障演练。

验收：

+ 高风险安全回归、越权和重复副作用会阻断发布。
+ 模型、Prompt、Workflow、Skill、知识库和 Reranker 变更可以独立对比。
+ 批量任务不会无限占用 CPU、内存和 goroutine。
+ 缓存命中下降可以定位到 Prompt、Toolset、摘要、序列化或模型路由。
+ 发布后可以在明确版本范围内快速回滚。

## 15. 项目最终优势
GopherMind 的优势不是“调用了多个 Agent”，而是构建了一个医疗场景下的可靠 Agent Runtime：

+ 使用 Agent Gateway 统一模型、Workflow、Skill、Tool、MCP、预算和策略。
+ 使用显式 Agent Run 状态机、Action Protocol 和 Invocation Event 管理执行。
+ 从 Cordis 学习组件声明、依赖管理、副作用回收和 Context 隔离。
+ 从 DeepSeek Harness 学习 Event Log、Surface、Compaction、Durable Inbox 和 Task DAG。
+ 使用 Go 实现有界并发、资源治理、可靠消息和生产可观测性。
+ 使用 LangGraph 编排医疗任务，而不是让 LangGraph State 承担全部数据职责。
+ 使用 Event Log 保存事实，使用 Surface 控制模型注意力。
+ 使用结构化权威库保存患者事实，使用 Pinecone 做候选召回。
+ 使用受控查询重写、Dense/BM25、BGE 和 Citation Gate 管理证据链。
+ 使用 Capability 和 Tool Middleware 控制医疗数据和高风险工具。
+ 使用统一评测、SLO、灰度和版本矩阵阻断安全回归。
+ 支持任务恢复、消息重试、幂等执行和全过程审计。

项目可以概括为：

> 基于 Go、LangGraph 和 DeepSeek 构建事件驱动的医疗 Agent 系统，通过 Agent Gateway、显式 Run/Action 协议、分层上下文、版本化 RAG、Skill/MCP 能力治理、可靠消息和统一评测发布门禁，实现可恢复、可审计、权限可控、成本可治理的智能问诊平台。
>
