from __future__ import annotations

import argparse
import json
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass
from pathlib import Path
from typing import Dict, Iterator, List, Optional, Tuple


DEFAULT_DATASETS = [
    "FreedomIntelligence/huatuo_encyclopedia_qa",
    "FreedomIntelligence/huatuo_knowledge_graph_qa",
]


@dataclass
class ImportStats:
    rows_seen: int = 0
    qa_seen: int = 0
    qa_ingested: int = 0
    chunks_ingested: int = 0
    qa_failed: int = 0


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Import Huatuo QA datasets into gophermind RAG /ingest endpoint."
    )
    parser.add_argument("--base-url", default="http://localhost:8000")
    parser.add_argument("--global-user-id", default="__global_medical__")
    parser.add_argument("--split", default="train")
    parser.add_argument("--datasets", nargs="+", default=DEFAULT_DATASETS)
    parser.add_argument("--checkpoint", default="tmp/huatuo_import_checkpoint.json")
    parser.add_argument("--resume", action="store_true")
    parser.add_argument("--max-rows", type=int, default=0)
    parser.add_argument("--timeout", type=float, default=30.0)
    parser.add_argument("--retries", type=int, default=3)
    parser.add_argument("--retry-backoff-seconds", type=float, default=0.8)
    parser.add_argument("--save-every-rows", type=int, default=50)
    parser.add_argument("--workers", type=int, default=4)
    parser.add_argument("--dry-run", action="store_true")
    return parser.parse_args()


def load_checkpoint(path: Path) -> Dict[str, int]:
    if not path.exists():
        return {}
    with path.open("r", encoding="utf-8") as f:
        data = json.load(f)
    if isinstance(data, dict):
        return {str(k): int(v) for k, v in data.items()}
    return {}


def save_checkpoint(path: Path, offsets: Dict[str, int]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8") as f:
        json.dump(offsets, f, ensure_ascii=False, indent=2)


def iter_qa_pairs(row: dict) -> Iterator[Tuple[str, str, int]]:
    questions = row.get("questions") or []
    answers = row.get("answers") or []
    if isinstance(questions, list) and isinstance(answers, list):
        n = min(len(questions), len(answers))
        for qa_idx in range(n):
            q = str(questions[qa_idx]).strip()
            a = str(answers[qa_idx]).strip()
            if q and a:
                yield q, a, qa_idx
        return

    q = str(row.get("question", "")).strip()
    a = str(row.get("answer", "")).strip()
    if q and a:
        yield q, a, 0


def build_payload(
    dataset_name: str,
    split: str,
    global_user_id: str,
    row_idx: int,
    qa_idx: int,
    question: str,
    answer: str,
) -> dict:
    doc_id = f"huatuo:{dataset_name}:{split}:{row_idx}:{qa_idx}"
    text = f"问题：{question}\n答案：{answer}\n来源：{dataset_name}"
    return {
        "user_id": global_user_id,
        "document_id": doc_id,
        "text": text,
        "metadata": {
            "source_dataset": dataset_name,
            "source_split": split,
            "row_idx": str(row_idx),
            "qa_idx": str(qa_idx),
            "lang": "zh",
            "domain": "medical",
            "ingest_version": "v1",
        },
    }


def post_ingest(
    session,
    ingest_url: str,
    payload: dict,
    timeout: float,
    retries: int,
    retry_backoff_seconds: float,
) -> int:
    last_error: Optional[Exception] = None
    for attempt in range(retries + 1):
        try:
            resp = session.post(ingest_url, json=payload, timeout=timeout)
            resp.raise_for_status()
            body = resp.json()
            chunks = int(body.get("chunks", 0))
            return max(chunks, 0)
        except Exception as err:
            last_error = err
            if attempt >= retries:
                break
            time.sleep(retry_backoff_seconds * (2**attempt))
    raise RuntimeError(f"ingest failed: {last_error}")


def run_dataset_import(
    session,
    dataset_name: str,
    split: str,
    ingest_url: str,
    global_user_id: str,
    timeout: float,
    retries: int,
    retry_backoff_seconds: float,
    dry_run: bool,
    start_row: int,
    max_rows: int,
    save_every_rows: int,
    workers: int,
    offsets: Dict[str, int],
    checkpoint_path: Path,
) -> ImportStats:
    stats = ImportStats()
    from datasets import load_dataset

    dataset = load_dataset(dataset_name, split=split, streaming=True)
    pool: Optional[ThreadPoolExecutor] = None
    if not dry_run and workers > 1:
        pool = ThreadPoolExecutor(max_workers=workers)

    try:
        for row_idx, row in enumerate(dataset):
            if row_idx < start_row:
                continue
            if max_rows > 0 and stats.rows_seen >= max_rows:
                break

            stats.rows_seen += 1
            payloads: List[dict] = []
            for question, answer, qa_idx in iter_qa_pairs(row):
                stats.qa_seen += 1
                payloads.append(
                    build_payload(
                        dataset_name=dataset_name,
                        split=split,
                        global_user_id=global_user_id,
                        row_idx=row_idx,
                        qa_idx=qa_idx,
                        question=question,
                        answer=answer,
                    )
                )

            if dry_run:
                stats.qa_ingested += len(payloads)
            elif pool is None:
                for payload in payloads:
                    try:
                        chunks = post_ingest(
                            session=session,
                            ingest_url=ingest_url,
                            payload=payload,
                            timeout=timeout,
                            retries=retries,
                            retry_backoff_seconds=retry_backoff_seconds,
                        )
                        stats.qa_ingested += 1
                        stats.chunks_ingested += chunks
                    except Exception:
                        stats.qa_failed += 1
            else:
                futures = [
                    pool.submit(
                        post_ingest,
                        session,
                        ingest_url,
                        payload,
                        timeout,
                        retries,
                        retry_backoff_seconds,
                    )
                    for payload in payloads
                ]
                for fut in as_completed(futures):
                    try:
                        chunks = fut.result()
                        stats.qa_ingested += 1
                        stats.chunks_ingested += chunks
                    except Exception:
                        stats.qa_failed += 1

            offsets[dataset_name] = row_idx + 1
            if stats.rows_seen % save_every_rows == 0:
                save_checkpoint(checkpoint_path, offsets)
                print(
                    f"[{dataset_name}] rows={stats.rows_seen} qa={stats.qa_seen} "
                    f"ok={stats.qa_ingested} fail={stats.qa_failed} chunks={stats.chunks_ingested}"
                )
    finally:
        if pool is not None:
            pool.shutdown(wait=True)

    save_checkpoint(checkpoint_path, offsets)
    return stats


def main() -> int:
    args = parse_args()
    ingest_url = args.base_url.rstrip("/") + "/ingest"
    checkpoint_path = Path(args.checkpoint)
    offsets = load_checkpoint(checkpoint_path) if args.resume else {}

    print(f"ingest_url={ingest_url}")
    print(f"global_user_id={args.global_user_id}")
    print(f"split={args.split}")
    print(f"datasets={args.datasets}")
    print(f"resume={args.resume} dry_run={args.dry_run}")

    all_stats = ImportStats()
    import requests

    with requests.Session() as session:
        for dataset_name in args.datasets:
            start_row = offsets.get(dataset_name, 0)
            print(f"\nstart dataset={dataset_name} from row={start_row}")
            stats = run_dataset_import(
                session=session,
                dataset_name=dataset_name,
                split=args.split,
                ingest_url=ingest_url,
                global_user_id=args.global_user_id,
                timeout=args.timeout,
                retries=args.retries,
                retry_backoff_seconds=args.retry_backoff_seconds,
                dry_run=args.dry_run,
                start_row=start_row,
                max_rows=args.max_rows,
                save_every_rows=args.save_every_rows,
                workers=max(1, args.workers),
                offsets=offsets,
                checkpoint_path=checkpoint_path,
            )
            print(
                f"done dataset={dataset_name} rows={stats.rows_seen} qa={stats.qa_seen} "
                f"ok={stats.qa_ingested} fail={stats.qa_failed} chunks={stats.chunks_ingested}"
            )
            all_stats.rows_seen += stats.rows_seen
            all_stats.qa_seen += stats.qa_seen
            all_stats.qa_ingested += stats.qa_ingested
            all_stats.qa_failed += stats.qa_failed
            all_stats.chunks_ingested += stats.chunks_ingested

    print(
        f"\nall datasets done rows={all_stats.rows_seen} qa={all_stats.qa_seen} "
        f"ok={all_stats.qa_ingested} fail={all_stats.qa_failed} chunks={all_stats.chunks_ingested}"
    )
    return 0 if all_stats.qa_failed == 0 else 1


if __name__ == "__main__":
    raise SystemExit(main())
