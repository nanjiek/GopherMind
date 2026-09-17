@echo off
setlocal

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

uvicorn app.main:app --host 0.0.0.0 --port 8000
