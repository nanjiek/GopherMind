# Kimi + RAG 端到端联调手册（UTF-8 版）

## 1. 目标

在本地完成以下链路验证：

1. Python RAG 服务可用（ingest/retrieve/rerank）。
2. Go 后端可用（auth/query）。
3. `/query` 使用 `model_type=kimi` 时，`usage.provider = kimi`。
4. 中文不再出现 `????`（通过 UTF-8 请求体发送）。

---

## 2. 前置条件

1. 已在项目根目录配置 `.env`，至少包含：
   - `KIMI_API_KEY`
   - `KIMI_BASE_URL=https://api.moonshot.cn/v1`
   - `KIMI_MODEL=kimi-k2.5`
2. Docker Desktop 正常运行。
3. Python RAG 环境可启动 `uvicorn`。

---

## 3. 一次性准备（PowerShell）

```powershell
chcp 65001
[Console]::InputEncoding  = [System.Text.Encoding]::UTF8
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
$OutputEncoding = [System.Text.Encoding]::UTF8

function Invoke-JsonUtf8 {
  param(
    [string]$Uri,
    [string]$Method = "Post",
    [object]$Data,
    [hashtable]$Headers = @{}
  )
  $json  = $Data | ConvertTo-Json -Depth 12
  $bytes = [System.Text.Encoding]::UTF8.GetBytes($json)
  Invoke-RestMethod -Uri $Uri -Method $Method -ContentType "application/json; charset=utf-8" -Headers $Headers -Body $bytes
}
```

---

## 4. 启动服务

### 4.1 启动 Docker（Go 后端与依赖）

```powershell
cd C:\Users\Huangsirui\OneDrive\Desktop\GopherMind
docker compose up -d mysql redis rabbitmq qdrant backend
```

### 4.2 本地启动 Python RAG

```powershell
cd C:\Users\Huangsirui\OneDrive\Desktop\GopherMind\python\rag_service
uvicorn app.main:app --host 0.0.0.0 --port 8000
```

---

## 5. 健康检查

```powershell
Invoke-RestMethod -Uri "http://127.0.0.1:8000/healthz" -Method Get
Invoke-RestMethod -Uri "http://127.0.0.1:9090/healthz" -Method Get
```

期望均返回 `status=ok`。

---

## 6. 认证（注册与登录）

```powershell
$reg = @{ username="e2e_user"; password="Passw0rd!123" }
try { Invoke-JsonUtf8 -Uri "http://127.0.0.1:9090/auth/register" -Data $reg } catch {}

$login = Invoke-JsonUtf8 -Uri "http://127.0.0.1:9090/auth/login" -Data @{
  username="e2e_user"
  password="Passw0rd!123"
  device_id="pc"
}
$token = $login.data.access_token
```

---

## 7. RAG 链路验证（UTF-8 入库）

```powershell
$uid = "u-utf8-e2e"

Invoke-JsonUtf8 -Uri "http://127.0.0.1:8000/ingest" -Data @{
  user_id     = $uid
  document_id = "doc-utf8-e2e-001"
  text        = "GopherMind 使用 Qdrant 做向量检索，BGE 做重排，Kimi 做最终生成。"
  metadata    = @{ source="utf8-e2e" }
}

$r = Invoke-JsonUtf8 -Uri "http://127.0.0.1:8000/retrieve" -Data @{
  user_id = $uid
  query   = "GopherMind 的 RAG 方案是什么"
  top_k   = 5
}
$r.documents[0].content

$rr = Invoke-JsonUtf8 -Uri "http://127.0.0.1:8000/rerank" -Data @{
  query = "GopherMind 的 RAG 方案是什么"
  docs  = $r.documents
  top_n = 3
}
$rr.documents[0].content
```

期望：内容为正常中文，不是 `????`。

---

## 8. 后端 `/query` 端到端验证（Kimi）

```powershell
$res = Invoke-JsonUtf8 -Uri "http://127.0.0.1:9090/query" `
  -Headers @{ Authorization = "Bearer $token" } `
  -Data @{
    session_id = ""
    question   = "请仅用中文说明 GopherMind 的 RAG 架构。"
    model_type = "kimi"
    use_rag    = $true
  }

$res.data.usage.provider
$res.data.answer
$res.data.citations.Count
```

期望：

1. `provider` 为 `kimi`。
2. `answer` 为中文有效回答。
3. `citations.Count` 通常大于 0。

---

## 9. 常用排障

### 9.1 查看后端日志

```powershell
cd C:\Users\Huangsirui\OneDrive\Desktop\GopherMind
docker compose logs backend --tail 200
```

### 9.2 重启后端（代码或配置变更后）

```powershell
cd C:\Users\Huangsirui\OneDrive\Desktop\GopherMind
docker compose up -d --force-recreate backend
```

### 9.3 校验容器内 Kimi 配置

```powershell
cd C:\Users\Huangsirui\OneDrive\Desktop\GopherMind
docker compose exec backend printenv | Select-String "KIMI_"
```

### 9.4 校验容器内 Kimi API 可达性

```powershell
cd C:\Users\Huangsirui\OneDrive\Desktop\GopherMind
docker compose exec backend sh -lc 'code=$(curl -sS -m 20 -o /tmp/models.txt -w "%{http_code}" https://api.moonshot.cn/v1/models -H Authorization:Bearer\ $KIMI_API_KEY); echo $code; cat /tmp/models.txt'
```

期望 HTTP 状态码为 `200`。

---

## 10. 结论判定

满足以下条件即可判定联调完成：

1. RAG ingest/retrieve/rerank 全部返回成功。
2. `/query` 返回 `code=0`。
3. `usage.provider = kimi`。
4. 中文内容显示正常（无 `????`）。

