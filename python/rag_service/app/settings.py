from __future__ import annotations

import os
from dataclasses import dataclass


@dataclass
class Settings:
    qdrant_url: str
    qdrant_api_key: str
    qdrant_collection: str
    qdrant_vector_size: int
    qdrant_distance: str

    embedding_provider: str
    embedding_model: str
    openai_api_key: str
    openai_base_url: str

    bge_rerank_model: str
    bge_use_fp16: bool

    chunk_size: int
    chunk_overlap: int

    hybrid_enabled: bool
    hybrid_dense_top_k: int
    hybrid_keyword_candidate_limit: int
    hybrid_rrf_k: int
    bm25_enabled: bool
    bm25_candidate_limit: int
    bm25_top_k: int
    global_knowledge_user_id: str

    @classmethod
    def load(cls) -> "Settings":
        return cls(
            qdrant_url=os.getenv("QDRANT_URL", "http://localhost:6333"),
            qdrant_api_key=os.getenv("QDRANT_API_KEY", ""),
            qdrant_collection=os.getenv("QDRANT_COLLECTION", "gophermind_docs"),
            qdrant_vector_size=int(os.getenv("QDRANT_VECTOR_SIZE", "1024")),
            qdrant_distance=os.getenv("QDRANT_DISTANCE", "cosine"),
            embedding_provider=os.getenv("EMBEDDING_PROVIDER", "huggingface").lower(),
            embedding_model=os.getenv("EMBEDDING_MODEL", "BAAI/bge-m3"),
            openai_api_key=os.getenv("OPENAI_API_KEY", ""),
            openai_base_url=os.getenv("OPENAI_BASE_URL", ""),
            bge_rerank_model=os.getenv("BGE_RERANK_MODEL", "BAAI/bge-reranker-v2-m3"),
            bge_use_fp16=os.getenv("BGE_USE_FP16", "true").lower() == "true",
            chunk_size=int(os.getenv("CHUNK_SIZE", "600")),
            chunk_overlap=int(os.getenv("CHUNK_OVERLAP", "120")),
            hybrid_enabled=os.getenv("HYBRID_ENABLED", "true").lower() == "true",
            hybrid_dense_top_k=int(os.getenv("HYBRID_DENSE_TOP_K", "30")),
            hybrid_keyword_candidate_limit=int(
                os.getenv("HYBRID_KEYWORD_CANDIDATE_LIMIT", "200")
            ),
            hybrid_rrf_k=int(os.getenv("HYBRID_RRF_K", "60")),
            bm25_enabled=os.getenv("BM25_ENABLED", "true").lower() == "true",
            bm25_candidate_limit=int(os.getenv("BM25_CANDIDATE_LIMIT", "500")),
            bm25_top_k=int(os.getenv("BM25_TOP_K", "50")),
            global_knowledge_user_id=os.getenv(
                "GLOBAL_KNOWLEDGE_USER_ID", "__global_medical__"
            ),
        )
