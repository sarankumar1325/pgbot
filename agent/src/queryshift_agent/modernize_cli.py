"""Terminal interface for the Enterprise Query Modernizer."""

from __future__ import annotations

import argparse
import json
import os
import shutil
import sys
from dataclasses import replace
from pathlib import Path

from .config import AgentConfig
from .events import RunEvent
from .modernizer_models import ModernizationReport

SOURCE_DIALECTS = ("oracle", "tsql", "db2", "mysql", "sybase", "postgres")


def parser() -> argparse.ArgumentParser:
    p = argparse.ArgumentParser(
        prog="queryshift-modernize",
        description=(
            "Modernize one legacy read-only SQL query to PostgreSQL with a bounded "
            "five-agent Strands pipeline using Amazon Bedrock."
        ),
    )
    p.add_argument(
        "input", help="legacy SQL file, or - to read SQL from standard input"
    )
    p.add_argument("--source", required=True, choices=SOURCE_DIALECTS)
    p.add_argument(
        "--schema-file",
        help="optional target schema snapshot (DDL, JSON, or text) used for mapping",
    )
    p.add_argument(
        "--live-schema",
        action="store_true",
        help="use queryshift read-only catalog/EXPLAIN tools via DATABASE_URL",
    )
    p.add_argument("--output", help="write the complete JSON report to this path")
    p.add_argument(
        "--format",
        choices=("summary", "json", "ndjson"),
        default="summary",
        help="ndjson streams lifecycle events followed by one result record",
    )
    p.add_argument("--model", help="Bedrock model or inference-profile ID")
    p.add_argument("--region", help="AWS region used for Bedrock inference")
    p.add_argument(
        "--queryshift-bin", help="path to queryshift (needed by --live-schema)"
    )
    p.add_argument("--max-repairs", type=int, help="maximum validator repair loops")
    p.add_argument("--timeout", type=int, help="whole-workflow timeout in seconds")
    return p


def _read(path: str, label: str) -> str:
    if path == "-":
        return sys.stdin.read()
    try:
        return Path(path).read_text(encoding="utf-8")
    except OSError as exc:
        raise ValueError(f"cannot read {label} {path!r}: {exc}") from exc


def resolved_config(args: argparse.Namespace) -> AgentConfig:
    config = AgentConfig.from_env()
    values = {
        "model_id": args.model,
        "region": args.region,
        "queryshift_bin": args.queryshift_bin,
        "max_repair_attempts": args.max_repairs,
        "timeout_seconds": args.timeout,
    }
    config = replace(
        config, **{name: value for name, value in values.items() if value is not None}
    )
    if config.max_repair_attempts < 0:
        raise ValueError("--max-repairs must be zero or greater")
    if config.timeout_seconds <= 0:
        raise ValueError("--timeout must be a positive integer")
    return config


def render_summary(report: ModernizationReport) -> str:
    indexes = report.optimization.index_recommendations
    lines = [
        f"{report.risk_level.upper()} risk · confidence {report.confidence_score}/100",
        report.confidence_basis,
        "",
        "Modernized PostgreSQL:",
        report.optimization.sql,
        "",
        (
            f"Validation: {'passed' if report.validation.valid else 'failed'}; "
            f"repairs: {report.repair_attempts}; unresolved mappings: "
            f"{len(report.mapping.unresolved_entities)}"
        ),
        f"Diff: +{report.diff.additions} -{report.diff.deletions}",
        (
            f"Index recommendations: {len(indexes)} "
            "(impact remains unverified unless planner_validated=true)"
        ),
        "",
        "Verification:",
        report.judge.verification_probe.comparison_method,
    ]
    return "\n".join(lines)


def _ndjson_event(event: RunEvent) -> str:
    """Serialize one metadata-only lifecycle record for interactive hosts."""

    return json.dumps(
        {"type": "event", "event": event.model_dump(mode="json")},
        ensure_ascii=False,
        separators=(",", ":"),
    )


def _ndjson_result(report: ModernizationReport) -> str:
    return json.dumps(
        {"type": "result", "result": report.model_dump(mode="json")},
        ensure_ascii=False,
        separators=(",", ":"),
    )


def main(argv: list[str] | None = None) -> int:
    args = parser().parse_args(argv)
    try:
        config = resolved_config(args)
        sql = _read(args.input, "SQL input")
        schema = _read(args.schema_file, "schema file") if args.schema_file else None
    except ValueError as exc:
        print(f"queryshift-modernize: {exc}", file=sys.stderr)
        return 64

    if args.live_schema:
        if shutil.which(config.queryshift_bin) is None:
            print(
                f"queryshift-modernize: cannot find queryshift executable {config.queryshift_bin!r}",
                file=sys.stderr,
            )
            return 3
        if not any(
            os.getenv(name)
            for name in ("DATABASE_URL", "QUERYSHIFT_DATABASE_URL", "PGSERVICE")
        ):
            print(
                "queryshift-modernize: --live-schema requires DATABASE_URL, "
                "QUERYSHIFT_DATABASE_URL, or PGSERVICE",
                file=sys.stderr,
            )
            return 64

    try:
        from .modernizer import run_modernization

        event_sink = None
        if args.format == "ndjson":
            event_sink = lambda event: print(_ndjson_event(event), flush=True)
        report = run_modernization(
            sql,
            config,
            source_dialect=args.source,
            target_schema=schema,
            live_schema=args.live_schema,
            event_sink=event_sink,
        )
    except KeyboardInterrupt:
        print("queryshift-modernize: cancelled", file=sys.stderr)
        return 130
    except Exception as exc:  # noqa: BLE001 - CLI boundary renders provider failures.
        print(f"queryshift-modernize: {exc}", file=sys.stderr)
        return 3

    report_json = report.model_dump_json(indent=2)
    if args.output:
        try:
            Path(args.output).write_text(report_json + "\n", encoding="utf-8")
        except OSError as exc:
            print(
                f"queryshift-modernize: cannot write {args.output!r}: {exc}",
                file=sys.stderr,
            )
            return 3
    if args.format == "ndjson":
        print(_ndjson_result(report), flush=True)
    else:
        print(report_json if args.format == "json" else render_summary(report))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
