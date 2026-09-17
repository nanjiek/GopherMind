# GopherMind V3.0 升级实施计划

日期：2026-09-17  
状态：规划，尚未开始升级实现  
设计输入：[V3.0 架构规划原文](ai-architecture-v3.zh.md)

## 1. 总体判断

方案的主要价值是把会话事实、模型上下文、患者记忆和执行状态分开，并为重试、恢复、权限和评测建立统一协议。建议保留这一方向，按可验证的小阶段推进。不要先搭齐所有 Agent，也不要一次搬迁所有目录和数据库。

原文完整保留，属于目标设计输入，不代表代码现状或已经验证的外部产品能力。本文基于当前工作区静态检查制定；没有运行编译、测试、线上调用或迁移。工作区已有大量修改和未跟踪文件，实施前必须记录基线，保留已有工作。

## 2. 当前代码与规划的差异

| 项目 | 代码证据 | 对计划的影响 |
| --- | --- | --- |
| 消息事实存储 | `internal/repo/mysql/session_repo.go` 已事务保存会话和消息；`session_service.go` 缓存 miss 后读取历史回填 | 不应描述为 Redis 唯一事实源；升级重点是增加事件语义、顺序、版本和重放 |
| 短期摘要 | `session_service.go` 窗口取最近 6 条，摘要拼接最近最多 8 条 | 这是截取与拼接，不能等同结构化模型摘要；需单独实现 Compaction |
| 模型接入 | `internal/model/factory/factory.go`、`providers/openai.go` 包含 OpenAI 兼容、Kimi、Qwen 等适配 | DeepSeek 统一路由属于待实施目标；不能把现有配置直接改名当作接入完成 |
| LangGraph | 已检查的 Go/Python 依赖和链路未发现 LangGraph；存在 LangChain 相关代码 | LangChain 不等于 LangGraph，需先明确 Go Runtime 与 Python 图执行的协议边界 |
| RAG | `internal/rag/langchain/engine.go`、`internal/vector/pinecone/client.go` 和 Python RAG 服务并存 | 先核验实际启动路径、混合召回实现与降级行为，避免重复维护两套主链路 |
| 查询重写 | Go RAG 的 `Rewrite` 当前为去首尾空白和合并空格 | 不能视为完成医学语义重写；需盘点其他调用路径，再补协议和否定词等测试 |
| 长期记忆 | `memory_service.go` 已主动录入、目录存储和向量读写；创建先写向量再写目录 | 优先补权威状态校验、事务 Outbox 和失败补偿，防止两边状态不一致 |
| MCP | `internal/transport/mcp/server.go` 已有 stdio Server 和工具 | 复用现有服务；新增的重点是出站 MCP Gateway、权限、预算和治理 |
| 消息去重 | `internal/repo/mysql/inbox_repo.go` 已有消费状态、去重和过期接管 | 可复用思路，但不代表已实现 Agent Durable Mailbox 或可靠 DAG |
| 可观测与评测 | 已有 `internal/obs`、`eval_service.go`、`test/e2e`、`test/perf` | 扩展现有设施，不从零另建重复系统 |

原文中的 DeepSeek 型号、上下文、视觉能力、价格与高峰时段尚未核验；本计划不采用这些数字作为实现依据。接入前按官方文档及实际账户能力验证并记录日期。11%/5% 等收益只有找回原始评测数据后才能作为成果引用。

## 3. 实施原则与架构决策

1. 沿用 `cmd/server`、`internal/core/service` 和现有 transport/repo 分层，通过接口逐步接入新模块；不为匹配原文目录而重排全部项目。
2. PostgreSQL 是目标存储。先补适配接口和迁移脚本，再迁移会话域及新增 Event/Task/Memory；认证等旧业务可以暂留 MySQL，明确每类数据唯一写入主库。
3. 不使用未经协调的 MySQL/PostgreSQL 同步双写。迁移期采用源库事务 Outbox、幂等投递、快照加增量追平，切换前验证数量、顺序、归属和内容校验。
4. Go 负责入口、授权、工具执行、预算和事实写入；LangGraph 初步按独立 Python 执行服务评估。先做最小 checkpoint 恢复验证，再冻结协议；图节点与 Task DAG 不能各自成为同一任务的调度权威。
5. 基础权限、审计、幂等、超时、输出校验从首个阶段开始。最后阶段补齐系统化验证与发布治理，不能最后才加入安全控制。
6. 保留现有同步和流式 API，通过配置开关切换新旧执行路径。发生数据切换后，回滚必须处理新增数据，不能只切回旧读路径。

## 4. 分阶段执行顺序

### P0：基线与关键决策（第一批工作）

- 盘点启动入口、数据表、API、Provider、RAG 实际路径、队列和现有测试，输出能力清单与依赖图。
- 运行 Go 编译/测试、前端构建及可用的 Python 测试，记录环境依赖、原有失败和复现命令。Go RAG 引用了 `github.com/tmc/langchaingo/textsplitter`，当前 `go.mod` 未列出该模块，应作为编译核验项。
- 固化问答、流式、缓存恢复、RAG、记忆和 MCP 冒烟用例；采集延迟和 Token 基线，外部服务使用明确标识的测试环境或替身。
- 编写三份 ADR：数据库渐进迁移、LangGraph/Go 执行边界、RAG 主路径及 DeepSeek 适配方案。
- 建立版本化小型评测集，覆盖无证据、否定、时间、剂量、特殊人群、权限隔离与重复请求。

验收：能明确“目前哪些链路可以运行、哪些失败早已存在”；关键边界有书面决策，后续阶段不依赖未验证的 SDK 或模型参数。

### P1：事件事实源与最小 Surface（对应原文阶段一）

- 新增 `internal/session/eventlog`、`projection`、`surface`，定义事件类型、schema version、会话序号、归属范围、请求幂等键和 source message ID。
- 添加 PostgreSQL 适配与版本化 migration，支持追加事件、按序读取、唯一约束和 Projection checkpoint。
- 先完成历史回填、增量追平和只读影子对比，再切换写入主库；同一会话写入需要确定的顺序及重复键行为。
- QueryService/StreamService 通过接口生成基础 Surface，复用现有窗口逻辑；Redis 使用含范围和版本的缓存键。

验收：历史回填重复执行不重复入库；Redis 丢失可重建；事件重放与原始历史一致；跨用户/患者读取被拒绝；旧 API 契约兼容。

### P2：最小 Agent Runtime（对应阶段二）

- 实现 Scope、依赖校验、启动失败回收、LIFO disposer、取消传播与有界并发。
- 实现 Run/Action/Observation 状态机、revision/CAS、错误分类、步骤限制、进展检测和 Hook Pipeline。
- 先将现有单 Agent 问答包装为 Runtime 执行，不同时扩展全部专业 Agent。
- 写操作使用稳定 operation ID；外部系统不支持幂等时记录结果不确定状态并进行对账，不承诺跨系统 exactly-once。

验收：初始化失败和取消后资源可回收；并发状态写入不覆盖；重复 Action 不重复副作用；无进展循环有明确终止原因。

### P3：Gateway 与能力治理（对应阶段三）

- 在进程内实现模型/Workflow 路由、Capability、输入输出 Schema、预算、超时、熔断和审计。
- 基于已核验能力增加 DeepSeek Provider、usage 与价格配置；其他 Provider 通过兼容开关保留迁移路径。
- 先落地一个受限 Skill 和一个只读 MCP 调用，贯通 Manifest、Knowledge Profile、工具中间件与调用追踪，再扩展注册表。
- 所有工具执行都再次校验可信范围与权限，不能只隐藏 Tool Schema。

验收：越权实际调用被拒绝；预算超限不继续 fan-out；MCP 超时不阻塞会话；每次调用可关联 Run/Step、成本与结果。

### P4：可恢复 Workflow 与多 Agent（对应阶段四）

- 先实现固定图：Intake → 风险路由 → Evidence → Safety → Response；Medication 按需进入，之后再引入 Lead 动态委派。
- 接入 P0 验证过的 LangGraph checkpoint，明确唯一任务状态机与图节点执行协议。
- 实现 Task DAG、Mailbox、依赖超时、去重、租约及 fencing token，阻止过期 worker 提交。
- 复用现有队列经验，但以持久任务状态判断完成，不能以消息已发送判断成功。

验收：进程重启能续跑；重复消息、旧 worker 和依赖循环不会破坏状态；简单问题走短路径；专业 Agent 状态与可见数据隔离。

### P5：Compaction 与输出恢复（对应阶段五）

- 在 P1 Surface 基础上加入真实 Token 预算、输出预留、结构化摘要、版本提交和大型工具结果外置。
- Compaction 按事件水位构建，并通过 CAS 发布；压缩期间的新消息保留在摘要之后。
- 增加 generation ID、event seq、已提交输出持久化和前端重连协议；重连重放已提交片段，不直接重新执行工具。
- JSON 输出通过完整 Schema 校验后才能触发业务写入；续写有次数与预算限制。

验收：摘要并发不丢消息；超长结果不撑满内存；断线/重启重放无重复；截断 JSON 无未校验副作用。高风险输出在发布屏障通过前不向用户流式泄露未审核内容。

### P6：长期记忆与知识治理（对应阶段六）

- 增加 candidate/confirmed/conflicted/expired/retracted 等状态、来源证据、版本与确认 API/UI。
- 权威库先提交，事务 Outbox 异步更新 Pinecone；召回后回查权威状态与权限，过滤撤回/删除/过期记录。
- 老记忆保留原始来源并标记迁移状态，不自动把文本转成已确认病史。
- 先补 Retrieval Trace 与语义重写测试，再调参；加入知识版本、影子索引、发布指针和回滚。

验收：向量同步失败不会改变事实；撤回后即使索引未更新也不能参与回答；否定/时间/剂量不被重写丢失；更新失败保持当前可用知识版本。

### P7：容量、评测与发布闭环（对应阶段七）

- 扩展现有 OTel、Prometheus、Langfuse 与评测服务，贯通 Run/Step/RAG/Memory/Cost 版本字段，限制敏感内容日志。
- 按实际基线确定分风险 SLO，冻结模型、Prompt、Workflow、Skill、知识和评测集版本。
- 执行权限/注入测试、依赖故障、重复投递、恢复、背压和资源压测；仅在开放代码执行能力时引入对应沙箱。
- 先离线和影子验证，再小流量灰度；回滚演练覆盖程序、配置、知识版本及数据兼容。

验收：严重安全回归、越权和重复副作用阻断发布；延迟与成本有可复现比较；取消和过载后资源恢复；回滚不丢新增会话和记忆。

## 5. 迁移和验收的统一要求

每个阶段拆为可独立评审的设计单元，每个单元包含：需求 ID、原文章节、接口/表结构、实现位置、单元/集成/故障用例、观测字段、兼容与回滚说明。没有 schema 变化的单元标注 migration 不适用。

迁移采用 expand → backfill → reconcile → switch → observe → contract。旧表仅在观察期结束且备份恢复演练通过后考虑收缩。切回旧存储之前必须回放新数据并对账；做不到时暂停写入并恢复，不静默退回过时数据。

通用故障用例包括：提交成功但响应丢失、同键并发请求、缓存不可用、索引同步失败、执行中取消、租约过期、重启恢复和越权访问。单元测试可用替身；事务、唯一约束和 CAS 必须在真实目标数据库集成环境验证。

需求追踪示例：

| 需求 ID | 原文章节 | 交付位置（计划） | 关键验收 |
| --- | --- | --- | --- |
| EVT-001 | 6.9、14 阶段一 | `internal/session/eventlog` | 清空缓存后重建历史 |
| RUN-001 | 5.10、14 阶段二 | `internal/runtime/run` | 非法迁移与过期 revision 被拒绝 |
| TOOL-001 | 9.1、9.4 | `internal/runtime/policy` | 伪造患者范围无法执行工具 |
| MEM-001 | 7.6、7.9 | `internal/memory` | 索引滞后时仍过滤撤回事实 |
| GEN-001 | 6.10 | 流式服务与前端 | 重连无重复且不重新执行副作用 |

## 6. 下一次实施的具体范围

建议从 P0 开始，只交付基线报告、可复现验证结果、三份 ADR 和 P1 的接口/表结构设计。随后用 P1 的“一个会话追加事件 → 重放 → Redis 重建”作为第一个功能闭环。

按单人推进、外部服务可用估算：P0 2～4 个工作日，P1 5～8 日，P2 5～8 日，P3 5～8 日，P4 7～12 日，P5 5～8 日，P6 8～12 日，P7 5～8 日；总计约 42～68 个工作日。仅用于拆分工作量，P0 后按现有失败、数据规模和接入验证结果重新估算，不作为交付承诺。

第一个里程碑是 P0～P2 的可回放单 Agent 链路；第二个是 P3～P5 的受控可恢复协作；第三个是 P6～P7 的记忆、知识和发布闭环。每个里程碑验收通过后再扩大能力范围。
