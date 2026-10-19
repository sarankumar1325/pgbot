# QueryShift OpenCode-Style TUI Pass

Date: 2026-10-19

## Scope

The primary QueryShift conversation, command discovery, agent-running state, queued prompts, and compact-terminal behavior were reviewed against the interaction patterns in OpenCode's current open-source TUI.

Primary references:

- OpenCode `Prompt` component: multiline composer, left accent, embedded model metadata, interrupt state, and responsive footer.
- OpenCode `CommandPaletteDialog`: searchable command descriptions, categories, shortcuts, and suggested actions.
- OpenCode session route: optimistic messages, queued-prompt dock, persistent composer, and history navigation.

## Flow health

1. **Launch and empty conversation — healthy.** QueryShift opens to one conversation surface with a compact database prerequisite instead of workflow cards.
2. **Command discovery — healthy.** `/` and `Ctrl+P` open a grouped command list; locked database actions explain the prerequisite. Escape restores an existing draft.
3. **Compose and paste — healthy.** The composer is multiline, full-width, and accepts `Ctrl+J` or `Alt+Enter` for new lines without submitting.
4. **Agent response — healthy in injected-model tests.** Submission is immediate, activity is visible, and the composer remains usable while the model runs.
5. **Queued follow-up — healthy.** A follow-up submitted during a run appears in a dedicated dock and starts after the active response.
6. **Conversation history — healthy.** `PgUp` and `PgDn` browse older messages; `End` returns to the newest turn.
7. **Session reset — healthy.** `/new`, `/clear`, and `Ctrl+N` reset chat state while retaining the verified database workspace.
8. **Database safety — unchanged and healthy.** Read mode remains the default; versioned write unlock and per-change confirmation are still separate from chat.
9. **Responsive layout — healthy at tested sizes.** Automated tests cover 80×24 and the live PTY render was inspected at 130×40.

## Evidence limits

- A live 130×40 terminal render and deterministic Bubble Tea view tests were inspected in this run. A pixel screenshot could not be captured from the isolated PTY, so font rendering, terminal-specific color calibration, mouse behavior, and assistive-technology behavior remain unverified.
- Agent completion is tested with an injected Bedrock-compatible model response. A live Bedrock call still depends on valid local AWS credentials and model access.
- Screenshot review alone cannot establish WCAG conformance for a terminal application.

## Verification

- `go test -count=1 ./...`
- `go vet ./...`
- `make build`
- Live TUI smoke render at 130×40

