from queryshift_agent.events import EventRecorder


def test_events_are_ordered_and_streamed_with_one_run_id():
    streamed = []
    recorder = EventRecorder(streamed.append, run_id="run-1")

    first = recorder.emit("run_started", model_id="model")
    second = recorder.emit("stage_started", stage="mapper")

    assert [event.sequence for event in recorder.events] == [1, 2]
    assert first.run_id == second.run_id == "run-1"
    assert streamed == recorder.events


def test_run_failed_is_a_supported_terminal_event():
    recorder = EventRecorder(run_id="run-2")

    event = recorder.emit("run_failed", error_category="authentication")

    assert event.kind == "run_failed"
    assert event.data["error_category"] == "authentication"
