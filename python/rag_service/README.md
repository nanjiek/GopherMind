# python/rag_service 使用说明

## 1. 模块定位
`python/rag_service` 是 GopherMind 的 RAG 子服务，负责：
- 文本向量化（Embedding）
- 向量检索（Qdrant）
- 重排（BGE Reranker）
- 入库接口（`/ingest`）

Go 后端通过 HTTP 调用该服务。

## 2. 依赖安装
在 `python/rag_service` 目录执行：

```bash
pip install -r requirements.txt
```

## 3. 本地启动（必须）
如果你在本机直连 Qdrant 并执行导入，请先设置以下环境变量，再启动服务。

```cmd
set HTTP_PROXY=
set HTTPS_PROXY=
set ALL_PROXY=
set NO_PROXY=127.0.0.1,localhost
set no_proxy=127.0.0.1,localhost

set QDRANT_URL=http://127.0.0.1:6333
set QDRANT_API_KEY=
set QDRANT_COLLECTION=gophermind_docs_bgem3

set EMBEDDING_PROVIDER=huggingface
set EMBEDDING_MODEL=BAAI/bge-m3
set QDRANT_VECTOR_SIZE=1024

set CHUNK_SIZE=10000
set CHUNK_OVERLAP=0

set BM25_ENABLED=true
set BM25_CANDIDATE_LIMIT=500
set BM25_TOP_K=50

set GLOBAL_KNOWLEDGE_USER_ID=__global_medical__

uvicorn app.main:app --host 0.0.0.0 --port 8000
```

也可以直接运行：

```cmd
scripts\start_rag_local.cmd
```

## 4. 健康检查

```powershell
Invoke-RestMethod -Method GET -Uri "http://localhost:8000/healthz"
```

返回 `status=ok` 表示服务可用。

## 5. 只导入百科数据集（个人使用推荐）
只导入 `huatuo_encyclopedia_qa`，不导入 `huatuo_knowledge_graph_qa`：

```cmd
cd /d C:\Users\Huangsirui\OneDrive\Desktop\GopherMind\python\rag_service\scripts

python import_huatuo.py ^
  --base-url http://localhost:8000 ^
  --global-user-id __global_medical__ ^
  --datasets FreedomIntelligence/huatuo_encyclopedia_qa ^
  --checkpoint tmp/huatuo_encyclopedia_checkpoint.json ^
  --workers 8 ^
  --save-every-rows 1 ^
  --resume
```

## 6. 清空并重导（仅清理全局医学库）
清空 `__global_medical__`：

```powershell
$deleteBody = @{
  filter = @{
    must = @(
      @{ key = "user_id"; match = @{ value = "__global_medical__" } }
    )
  }
  wait = $true
} | ConvertTo-Json -Depth 10

Invoke-RestMethod -Method POST `
  -Uri "http://localhost:6333/collections/gophermind_docs_bgem3/points/delete" `
  -ContentType "application/json" `
  -Body $deleteBody | ConvertTo-Json -Depth 10
```

验证是否清空：

```powershell
$countBody = @{
  exact = $true
  filter = @{
    must = @(
      @{ key = "user_id"; match = @{ value = "__global_medical__" } }
    )
  }
} | ConvertTo-Json -Depth 10

Invoke-RestMethod -Method POST `
  -Uri "http://localhost:6333/collections/gophermind_docs_bgem3/points/count" `
  -ContentType "application/json" `
  -Body $countBody | ConvertTo-Json -Depth 10
```

## 7. 导入进度观察（每 30 秒）

```powershell
while ($true) {
  $body = @{
    exact = $true
    filter = @{
      must = @(
        @{ key = "user_id"; match = @{ value = "__global_medical__" } }
      )
    }
  } | ConvertTo-Json -Depth 10

  $res = Invoke-RestMethod -Method POST `
    -Uri "http://localhost:6333/collections/gophermind_docs_bgem3/points/count" `
    -ContentType "application/json" `
    -Body $body

  Write-Host ("{0} __global_medical__ count={1}" -f (Get-Date -Format "HH:mm:ss"), $res.result.count)
  Start-Sleep -Seconds 30
}
```

## 8. 常见问题

### 8.1 `ModuleNotFoundError: No module named 'rank_bm25'`

```bash
pip install -r requirements.txt
```

### 8.2 HuggingFace 连接超时
可设置镜像后重试：

```cmd
set HF_ENDPOINT=https://hf-mirror.com
set HF_HUB_ENABLE_HF_TRANSFER=1
set HF_HUB_DOWNLOAD_TIMEOUT=180
set HF_HUB_ETAG_TIMEOUT=30
```

### 8.3 导入开始前等待较久
通常是在线读取数据集或模型热加载，属于正常现象；一旦开始写入，`chunks` 会持续增长。
