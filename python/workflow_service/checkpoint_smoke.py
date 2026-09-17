"""P0 spike: verify a LangGraph checkpoint survives checkpointer reopen.

SQLite is deliberately limited to this local spike. Production uses the
PostgreSQL checkpointer selected in ADR-0002.
"""

from __future__ import annotations

import argparse
import operator
from pathlib import Path
from typing import Annotated, TypedDict

from langgraph.checkpoint.sqlite import SqliteSaver
from langgraph.graph import END, START, StateGraph


class State(TypedDict):
    values: Annotated[list[str], operator.add]


def append_step(_: State) -> State:
    return {"values": ["completed"]}


def build_graph(checkpointer: SqliteSaver):
    builder = StateGraph(State)
    builder.add_node("append_step", append_step)
    builder.add_edge(START, "append_step")
    builder.add_edge("append_step", END)
    return builder.compile(checkpointer=checkpointer)


def run(database: Path) -> None:
    config = {"configurable": {"thread_id": "p0-checkpoint-smoke"}}
    database.parent.mkdir(parents=True, exist_ok=True)

    with SqliteSaver.from_conn_string(str(database)) as saver:
        result = build_graph(saver).invoke({"values": []}, config)
        assert result["values"] == ["completed"]

    with SqliteSaver.from_conn_string(str(database)) as saver:
        snapshot = build_graph(saver).get_state(config)
        assert snapshot.values["values"] == ["completed"]

    print("checkpoint reopen passed")


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--database", type=Path, required=True)
    run(parser.parse_args().database)
