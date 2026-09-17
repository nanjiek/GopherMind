from __future__ import annotations

import math
import re
import time
import uuid
from typing import Dict, List, Optional, Set, Tuple

import requests

from langchain_huggingface import HuggingFaceEmbeddings
from langchain_openai import OpenAIEmbeddings
from langchain_text_splitters import RecursiveCharacterTextSplitter
from qdrant_client import QdrantClient, models
from qdrant_client.http.exceptions import UnexpectedResponse
from rank_bm25 import BM25Okapi

from .schemas import RetrieveDoc
from .settings import Settings

try:
    from FlagEmbedding import FlagReranker
except Exception:  # pragma: no cover - optional import guard for minimal runtime robustness
    FlagReranker = None


class RAGEngine:
    """
    Minimal production-ready pipeline:
    - Embedding: LangChain embeddings
    - Retrieve: Qdrant vector search with user-level filtering
    - Rerank: BGE reranker (FlagEmbedding)
    """

    def __init__(self, settings: Optional[Settings] = None) -> None:
        self.settings = settings or Settings.load()
        self._embeddings = self._build_embeddings()
        self._splitter = RecursiveCharacterTextSplitter(
            chunk_size=self.settings.chunk_size,
            chunk_overlap=self.settings.chunk_overlap,
            separators=["\n\n", "\n", ".", " ", ""],
        )
        self._qdrant = QdrantClient(
            url=self.settings.qdrant_url,
            api_key=self.settings.qdrant_api_key or None,
            check_compatibility=False,
            timeout=10.0,
        )
        self._reranker = None
        self._ensure_collection()

    def _build_embeddings(self):
        provider = self.settings.embedding_provider
        if provider == "openai":
            kwargs: Dict[str, str] = {
                "model": self.settings.embedding_model,
                "api_key": self.settings.openai_api_key,
            }
            if self.settings.openai_base_url:
                kwargs["base_url"] = self.settings.openai_base_url
            return OpenAIEmbeddings(**kwargs)
        return HuggingFaceEmbeddings(model_name=self.settings.embedding_model)

    def _distance(self) -> models.Distance:
        dist = self.settings.qdrant_distance.lower()
        if dist == "dot":
            return models.Distance.DOT
        if dist == "euclid":
            return models.Distance.EUCLID
        return models.Distance.COSINE

    def _ensure_collection(self) -> None:
        name = self.settings.qdrant_collection
        try:
            self._with_qdrant_retry(lambda: self._qdrant.get_collection(name))
            return
        except Exception:
            pass
        try:
            self._with_qdrant_retry(
                lambda: self._qdrant.create_collection(
                    collection_name=name,
                    vectors_config=models.VectorParams(
                        size=self.settings.qdrant_vector_size,
                        distance=self._distance(),
                    ),
                )
            )
        except Exception as e:
            # Handle create race or transient errors by checking one more time.
            if self._is_already_exists(e):
                return
            self._with_qdrant_retry(lambda: self._qdrant.get_collection(name))

    def _get_reranker(self):
        if self._reranker is not None:
            return self._reranker
        if FlagReranker is None:
            raise RuntimeError("FlagEmbedding is required for BGE rerank.")
        self._reranker = FlagReranker(
            self.settings.bge_rerank_model,
            use_fp16=self.settings.bge_use_fp16,
        )
        return self._reranker

    def embed(self, text: str) -> List[float]:
        return list(self._embeddings.embed_query(text))

    def ingest(
        self,
        user_id: str,
        document_id: str,
        text: str,
        metadata: Optional[Dict[str, str]] = None,
    ) -> int:
        metadata = metadata or {}
        chunks = self._splitter.split_text(text)
        if not chunks:
            return 0

        vectors = self._embeddings.embed_documents(chunks)
        points: List[models.PointStruct] = []
        for idx, (chunk, vector) in enumerate(zip(chunks, vectors)):
            chunk_id = f"{document_id}-chunk-{idx}"
            payload = {
                "user_id": user_id,
                "doc_id": document_id,
                "chunk_id": chunk_id,
                "content": chunk,
                "metadata": metadata,
            }
            points.append(
                models.PointStruct(
                    id=str(uuid.uuid4()),
                    vector=vector,
                    payload=payload,
                )
            )

        self._with_qdrant_retry(
            lambda: self._qdrant.upsert(
                collection_name=self.settings.qdrant_collection, points=points, wait=True
            )
        )
        return len(points)

    def retrieve(self, user_id: str, query: str, top_k: int) -> List[RetrieveDoc]:
        vector = self._embeddings.embed_query(query)
        dense_top_k = top_k
        if self.settings.hybrid_enabled:
            dense_top_k = max(top_k, self.settings.hybrid_dense_top_k)
        flt = self._build_user_filter(user_id)

        try:
            hits = self._with_qdrant_retry(
                lambda: self._qdrant.search(
                    collection_name=self.settings.qdrant_collection,
                    query_vector=vector,
                    query_filter=flt,
                    limit=dense_top_k,
                    with_payload=True,
                    with_vectors=False,
                )
            )
        except Exception:
            hits = self._search_via_rest(vector=vector, user_id=user_id, top_k=dense_top_k)

        # Some client/server combinations may return empty unexpectedly.
        if not hits:
            rest_hits = self._search_via_rest(vector=vector, user_id=user_id, top_k=dense_top_k)
            if rest_hits:
                hits = rest_hits

        dense_docs = [self._hit_to_doc(hit) for hit in hits]
        dense_docs = [d for d in dense_docs if d.chunk_id or d.doc_id]
        if not self.settings.hybrid_enabled and not self.settings.bm25_enabled:
            return dense_docs[:top_k]

        try:
            recall_lists = [dense_docs]
            if self.settings.bm25_enabled:
                bm25_docs = self._bm25_recall(
                    user_id=user_id,
                    query=query,
                    candidate_limit=max(1, self.settings.bm25_candidate_limit),
                    top_k=max(1, self.settings.bm25_top_k),
                )
                if bm25_docs:
                    recall_lists.append(bm25_docs)
            elif self.settings.hybrid_enabled:
                keyword_docs = self._keyword_recall(
                    user_id=user_id,
                    query=query,
                    candidate_limit=self.settings.hybrid_keyword_candidate_limit,
                )
                if keyword_docs:
                    recall_lists.append(keyword_docs)

            merged = self._rrf_merge_many(
                recall_lists=recall_lists,
                top_k=top_k,
                rrf_k=max(1, self.settings.hybrid_rrf_k),
            )
            if merged:
                return merged
        except Exception:
            # Keep retrieval available even if hybrid lexical branch fails.
            pass
        return dense_docs[:top_k]

    def rerank(self, query: str, docs: List[RetrieveDoc], top_n: int) -> List[RetrieveDoc]:
        if not docs:
            return []
        try:
            reranker = self._get_reranker()
            pairs = [[query, d.content] for d in docs]
            scores = reranker.compute_score(pairs)
            if isinstance(scores, float):
                scores = [scores]

            rescored: List[Tuple[float, RetrieveDoc]] = []
            for score, doc in zip(scores, docs):
                # Keep a weighted blend to preserve retrieval signal.
                doc.score = float(doc.score * 0.6 + float(score) * 0.4)
                rescored.append((doc.score, doc))
            rescored.sort(key=lambda x: x[0], reverse=True)
            return [d for _, d in rescored[:top_n]]
        except Exception:
            # Degrade gracefully when reranker model init/inference fails.
            sorted_docs = sorted(docs, key=lambda d: d.score, reverse=True)
            return sorted_docs[:top_n]

    def kg_placeholder(self, query: str) -> str:
        # Placeholder for future KG lookup service.
        tokens = [t for t in re.split(r"[^0-9a-zA-Z\u4e00-\u9fa5]+", query.lower()) if t]
        entities = [t for t in tokens if len(t) >= 2][:5]
        if not entities:
            return ""
        return " -> ".join(entities)

    def _with_qdrant_retry(self, fn, attempts: int = 6):
        last_exc: Optional[Exception] = None
        for i in range(attempts):
            try:
                return fn()
            except Exception as e:  # pragma: no cover - retry robustness
                last_exc = e
                if not self._is_retryable_qdrant_error(e) or i == attempts - 1:
                    raise
                time.sleep(min(2.0, 0.2 * (2**i)))
        if last_exc is not None:
            raise last_exc
        raise RuntimeError("unexpected qdrant retry state")

    def _is_retryable_qdrant_error(self, err: Exception) -> bool:
        if isinstance(err, UnexpectedResponse):
            return err.status_code in (429, 500, 502, 503, 504)
        text = str(err).lower()
        return "503" in text or "service unavailable" in text or "connection refused" in text

    def _is_already_exists(self, err: Exception) -> bool:
        if isinstance(err, UnexpectedResponse):
            return err.status_code == 409
        text = str(err).lower()
        return "already exists" in text or "409" in text

    def _search_via_rest(self, vector: List[float], user_id: str, top_k: int) -> List[dict]:
        base = self.settings.qdrant_url.rstrip("/")
        url = f"{base}/collections/{self.settings.qdrant_collection}/points/search"
        headers = {"Content-Type": "application/json"}
        if self.settings.qdrant_api_key:
            headers["api-key"] = self.settings.qdrant_api_key
        body = {
            "vector": vector,
            "limit": top_k,
            "with_payload": True,
            "with_vector": False,
            "filter": self._rest_user_filter(user_id),
        }
        resp = requests.post(url, json=body, headers=headers, timeout=10)
        resp.raise_for_status()
        payload = resp.json()
        result = payload.get("result", [])
        if isinstance(result, list):
            return result
        return []

    def _scroll_candidates_via_rest(self, user_id: str, candidate_limit: int) -> List[dict]:
        base = self.settings.qdrant_url.rstrip("/")
        url = f"{base}/collections/{self.settings.qdrant_collection}/points/scroll"
        headers = {"Content-Type": "application/json"}
        if self.settings.qdrant_api_key:
            headers["api-key"] = self.settings.qdrant_api_key

        collected: List[dict] = []
        offset = None
        while len(collected) < candidate_limit:
            body = {
                "limit": min(64, candidate_limit - len(collected)),
                "with_payload": True,
                "with_vector": False,
                "filter": self._rest_user_filter(user_id),
            }
            if offset is not None:
                body["offset"] = offset
            resp = requests.post(url, json=body, headers=headers, timeout=10)
            resp.raise_for_status()
            payload = resp.json().get("result", {})
            if isinstance(payload, dict):
                points = payload.get("points", [])
                offset = payload.get("next_page_offset")
            elif isinstance(payload, list):
                points = payload
                offset = None
            else:
                break
            if not points:
                break
            collected.extend(points)
            if offset is None:
                break
        return collected[:candidate_limit]

    def _hit_to_doc(self, hit) -> RetrieveDoc:
        if isinstance(hit, dict):
            payload = hit.get("payload") or {}
            score = float(hit.get("score") or 0.0)
        else:
            payload = hit.payload or {}
            score = float(hit.score or 0.0)
        meta = payload.get("metadata", {})
        return RetrieveDoc(
            doc_id=str(payload.get("doc_id", "")),
            chunk_id=str(payload.get("chunk_id", "")),
            content=str(payload.get("content", "")),
            score=score,
            metadata={str(k): str(v) for k, v in meta.items()},
        )

    def _keyword_recall(self, user_id: str, query: str, candidate_limit: int) -> List[RetrieveDoc]:
        query_tokens = self._tokenize_for_lexical(query)
        if not query_tokens:
            return []

        candidates = self._scroll_candidates_via_rest(
            user_id=user_id,
            candidate_limit=max(1, candidate_limit),
        )
        rescored: List[RetrieveDoc] = []
        for point in candidates:
            payload = point.get("payload") or {}
            content = str(payload.get("content", ""))
            if not content:
                continue
            doc_tokens = self._tokenize_for_lexical(content)
            if not doc_tokens:
                continue
            overlap = len(query_tokens & doc_tokens)
            if overlap <= 0:
                continue
            score = overlap / math.sqrt(len(doc_tokens) + 1.0)
            meta = payload.get("metadata", {})
            rescored.append(
                RetrieveDoc(
                    doc_id=str(payload.get("doc_id", "")),
                    chunk_id=str(payload.get("chunk_id", "")),
                    content=content,
                    score=float(score),
                    metadata={str(k): str(v) for k, v in meta.items()},
                )
            )

        rescored.sort(key=lambda d: d.score, reverse=True)
        return rescored

    def _bm25_recall(
        self, user_id: str, query: str, candidate_limit: int, top_k: int
    ) -> List[RetrieveDoc]:
        query_tokens = list(self._tokenize_for_lexical(query))
        if not query_tokens:
            return []

        candidates = self._scroll_candidates_via_rest(
            user_id=user_id,
            candidate_limit=max(1, candidate_limit),
        )
        docs: List[RetrieveDoc] = []
        tokenized_corpus: List[List[str]] = []
        for point in candidates:
            payload = point.get("payload") or {}
            content = str(payload.get("content", ""))
            if not content:
                continue
            tokens = list(self._tokenize_for_lexical(content))
            if not tokens:
                continue
            meta = payload.get("metadata", {})
            docs.append(
                RetrieveDoc(
                    doc_id=str(payload.get("doc_id", "")),
                    chunk_id=str(payload.get("chunk_id", "")),
                    content=content,
                    score=0.0,
                    metadata={str(k): str(v) for k, v in meta.items()},
                )
            )
            tokenized_corpus.append(tokens)

        if not docs:
            return []

        bm25 = BM25Okapi(tokenized_corpus)
        scores = bm25.get_scores(query_tokens)
        for i, score in enumerate(scores):
            docs[i].score = float(score)
        docs.sort(key=lambda d: d.score, reverse=True)
        return docs[:top_k]

    def _user_scope_values(self, user_id: str) -> List[str]:
        user_ids = [user_id]
        global_id = self.settings.global_knowledge_user_id.strip()
        if global_id and global_id != user_id:
            user_ids.append(global_id)
        return user_ids

    def _build_user_filter(self, user_id: str) -> models.Filter:
        should = [
            models.FieldCondition(
                key="user_id",
                match=models.MatchValue(value=uid),
            )
            for uid in self._user_scope_values(user_id)
        ]
        return models.Filter(should=should, min_should=models.MinShould(conditions=should, min_count=1))

    def _rest_user_filter(self, user_id: str) -> dict:
        should = [{"key": "user_id", "match": {"value": uid}} for uid in self._user_scope_values(user_id)]
        return {
            "should": should,
            "min_should": {
                "conditions": should,
                "min_count": 1,
            },
        }

    def _tokenize_for_lexical(self, text: str) -> Set[str]:
        parts = re.findall(r"[0-9a-zA-Z]+|[\u4e00-\u9fff]+", text.lower())
        tokens: Set[str] = set()
        for p in parts:
            if re.fullmatch(r"[\u4e00-\u9fff]+", p):
                if len(p) == 1:
                    tokens.add(p)
                    continue
                for i in range(len(p) - 1):
                    tokens.add(p[i : i + 2])
            else:
                tokens.add(p)
        return tokens

    def _doc_key(self, doc: RetrieveDoc) -> str:
        if doc.chunk_id:
            return doc.chunk_id
        if doc.doc_id:
            return doc.doc_id
        return doc.content[:128]

    def _rrf_merge(
        self,
        dense_docs: List[RetrieveDoc],
        keyword_docs: List[RetrieveDoc],
        top_k: int,
        rrf_k: int,
    ) -> List[RetrieveDoc]:
        scores: Dict[str, float] = {}
        docs: Dict[str, RetrieveDoc] = {}

        for rank, doc in enumerate(dense_docs, start=1):
            key = self._doc_key(doc)
            if key not in docs:
                docs[key] = doc
            scores[key] = scores.get(key, 0.0) + 1.0 / (rrf_k + rank)

        for rank, doc in enumerate(keyword_docs, start=1):
            key = self._doc_key(doc)
            if key not in docs:
                docs[key] = doc
            scores[key] = scores.get(key, 0.0) + 1.0 / (rrf_k + rank)

        merged: List[RetrieveDoc] = []
        for key, doc in docs.items():
            merged.append(
                RetrieveDoc(
                    doc_id=doc.doc_id,
                    chunk_id=doc.chunk_id,
                    content=doc.content,
                    score=float(scores.get(key, 0.0)),
                    metadata=doc.metadata,
                )
            )
        merged.sort(key=lambda d: d.score, reverse=True)
        return merged[:top_k]

    def _rrf_merge_many(
        self, recall_lists: List[List[RetrieveDoc]], top_k: int, rrf_k: int
    ) -> List[RetrieveDoc]:
        if not recall_lists:
            return []
        merged = recall_lists[0]
        for docs in recall_lists[1:]:
            merged = self._rrf_merge(
                dense_docs=merged,
                keyword_docs=docs,
                top_k=max(top_k, len(merged), len(docs)),
                rrf_k=rrf_k,
            )
        return merged[:top_k]

