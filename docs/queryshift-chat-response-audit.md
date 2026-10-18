# QueryShift conversational TUI audit

Date: 2026-10-18

## Evidence

- User-provided connected-home screenshot in the active chat.
- Local 130 × 40 PTY render after implementation.
- Automated connected, running, completed, queued, cancelled, and 80 × 24 render states.
- Full repository tests, vet, and binary build.

The screenshot attachment was no longer present at its original Desktop path, so it could not be copied into the repository. The visible screenshot in the active chat was used to identify the failed response state.

## Audited flow

1. **Connected home — repaired.** The long Neon hostname is retained in the header but no longer repeated beside the composer, preventing the status row from wrapping.
2. **Prompt submission — repaired.** Ordinary natural language now starts the configured model instead of only recording the user message.
3. **Immediate feedback — healthy.** The transcript shows an animated `AGENT RUNNING` row, elapsed time, live-schema context, and the cancellation shortcut immediately after Enter.
4. **Model completion — healthy in injected-model tests.** The assistant response is appended with provider and model metadata. Runtime provider failures become visible assistant messages instead of silent stalls.
5. **Busy-session input — healthy.** A follow-up message can be composed and queued while the current response runs, then starts automatically after completion.
6. **Cancellation — healthy.** Escape cancels the active model request, discards queued prompts, and leaves a visible cancellation event in the transcript.
7. **Responsive composer — healthy.** The composer now uses the full available terminal width and remains within 80 × 24 and 130 × 40 layouts.
8. **Bedrock configuration — implemented, credentials not available in the audit shell.** AWS credentials or a Bedrock API key are auto-detected, `QUERYSHIFT_AGENT_MODEL` is accepted as the conversational model fallback, and the TUI loads the model allowlist from the repository `.env` without loading `DATABASE_URL`.

## OpenCode-derived interaction decisions

- Submit locally, then perform model work asynchronously.
- Represent running state separately from the request call.
- Keep commands as direct local actions.
- Accept a queued follow-up while the session is busy.
- Keep cancellation available during model execution.

## Limits

- No live Bedrock invocation was possible because the audit shell had no AWS credentials.
- The response path was verified with an injected Bedrock-shaped model and provider-resolution tests.
- Screenshot inspection cannot prove screen-reader behavior or contrast across every terminal theme.

## Verification

```text
go test ./...  PASS
go vet ./...   PASS
make build     PASS
PTY 130×40     PASS
TUI 80×24      PASS
```
