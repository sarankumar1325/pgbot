"""Command-line entry point for the optional Strands agent."""

from __future__ import annotations

import argparse
import shutil
import sys
from dataclasses import replace

from .config import AgentConfig


def parser() -> argparse.ArgumentParser:
    p = argparse.ArgumentParser(
        prog="queryshift-agent",
        description=(
            "Diagnose any PostgreSQL-compatible database with queryshift's read-only "
            "tools and an Amazon Bedrock model."
        ),
    )
    p.add_argument("question", nargs="+", help="diagnostic question")
    p.add_argument("--model", help="Bedrock model or inference-profile ID")
    p.add_argument("--region", help="AWS region used for Bedrock inference")
    p.add_argument("--queryshift-bin", help="path to the queryshift binary")
    p.add_argument("--max-turns", type=int, help="maximum model/tool loop turns")
    p.add_argument("--timeout", type=int, help="wall-clock timeout in seconds")
    return p


def resolved_config(args: argparse.Namespace) -> AgentConfig:
    config = AgentConfig.from_env()
    overrides = {
        "model_id": args.model,
        "region": args.region,
        "queryshift_bin": args.queryshift_bin,
        "max_turns": args.max_turns,
        "timeout_seconds": args.timeout,
    }
    values = {name: value for name, value in overrides.items() if value is not None}
    config = replace(config, **values)
    if config.max_turns <= 0:
        raise ValueError("--max-turns must be a positive integer")
    if config.timeout_seconds <= 0:
        raise ValueError("--timeout must be a positive integer")
    return config


def main(argv: list[str] | None = None) -> int:
    args = parser().parse_args(argv)
    try:
        config = resolved_config(args)
    except ValueError as exc:
        print(f"queryshift-agent: {exc}", file=sys.stderr)
        return 64

    if shutil.which(config.queryshift_bin) is None:
        print(
            f"queryshift-agent: cannot find queryshift executable {config.queryshift_bin!r}; "
            "install queryshift or set QUERYSHIFT_BIN",
            file=sys.stderr,
        )
        return 3

    # Importing the runtime starts the optional Strands dependency surface only
    # after cheap CLI/config validation has succeeded.
    try:
        from .runtime import run

        result = run(" ".join(args.question), config)
    except KeyboardInterrupt:
        print("queryshift-agent: cancelled", file=sys.stderr)
        return 130
    except Exception as exc:  # noqa: BLE001 - CLI boundary renders provider/tool failures.
        print(f"queryshift-agent: {exc}", file=sys.stderr)
        return 3

    print(str(result))
    stop_reason = getattr(result, "stop_reason", "end_turn")
    if stop_reason != "end_turn":
        print(f"\n— agent stopped: {stop_reason}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
