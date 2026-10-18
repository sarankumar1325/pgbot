import json

from queryshift_agent.events import RunEvent
from queryshift_agent.modernize_cli import _ndjson_event, parser


def test_parser_accepts_streaming_ndjson_format():
    args = parser().parse_args(["-", "--source", "oracle", "--format", "ndjson"])

    assert args.format == "ndjson"


def test_ndjson_event_is_one_typed_metadata_record():
    event = RunEvent(
        run_id="run-1",
        sequence=1,
        kind="stage_started",
        stage="mapper",
        data={"agent": "schema-semantic-mapper"},
    )

    line = _ndjson_event(event)
    record = json.loads(line)

    assert "\n" not in line
    assert record["type"] == "event"
    assert record["event"]["run_id"] == "run-1"
    assert record["event"]["data"] == {"agent": "schema-semantic-mapper"}
