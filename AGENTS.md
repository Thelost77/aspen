# Aspen Agent Guide

## Purpose

Aspen is a macOS terminal client for reading existing Apple Messages conversations and sending text to exact one-to-one iMessage or SMS chats.

Safety outranks convenience. Wrong-recipient sends, Apple database writes, and private-data leakage are release blockers.

## Commands

```sh
go test ./... -count=1
go test -race ./...
go vet ./...
golangci-lint run
go build .
```

Run `gofmt` on changed Go files. Use synthetic fixtures only.

Never invoke a real Messages send from automated tests, scripts, or development tooling.

## Architecture

```text
main.go               flags, dependencies, Bubble Tea lifecycle
internal/app          model, update loop, async commands, rendering
internal/messages     read-only chat.db queries and typedstream fallback
internal/contacts     read-only AddressBook resolution
internal/sender       exact-chat AppleScript sending
internal/inlineimage  terminal probing, conversion, Kitty/iTerm2 output
internal/logger       private operation logs
internal/ui           ANSI-safe width, styles, help
```

## Safety invariants

### Database access

- Open Messages and Contacts SQLite databases read-only.
- Keep `PRAGMA query_only=1` enabled.
- Never write, migrate, vacuum, checkpoint, or repair Apple databases.
- Use bounded queries and keyset pagination; do not scan with `OFFSET`.

### Sending

- Capture immutable `ChatID`, `chat.guid`, service, and group state from the selected chat.
- Send only to the exact Messages chat object matching `chat.guid`.
- Pass GUID and message through AppleScript `argv`; never interpolate them into source.
- Never fall back to a participant, display name, phone number, email, or list index.
- Keep group and RCS sending disabled until explicit manual verification exists.
- Preserve drafts on failure and suppress duplicate sends while one is pending.

### Async state

- Associate async results with stable chat IDs and generations.
- Ignore stale results after chat switches or newer loads.
- Keep drafts keyed by chat ID.
- Stop load-all chaining when the user switches chats or requests cancellation.

### Privacy

- Never log message text, contact names, handles, chat GUIDs, or attachment paths.
- Do not add message caches, telemetry, analytics, or network listeners.
- Keep runtime directories `0700` and files `0600`.
- Keep test phone numbers in reserved `+1555` ranges and domains under `.test`.

### Inline images

- Bound source bytes, decoded pixels, and transfer dimensions.
- Convert HEIC/HEIF locally with `/usr/bin/sips`.
- Secure temporary files and delete them after encoding.
- Clear Kitty placements before overlays and on shutdown.
- Crop partial Kitty placements to viewport source rows; do not stretch iTerm2 partials.

## UI conventions

- Preserve responsive split and narrow layouts.
- Keep message bubbles outline-only with terminal-default interiors.
- Keep all rendered output inside requested width and height.
- Use ANSI-aware and Unicode-safe width helpers from `internal/ui`.
- `/` filters the active list or loaded messages.
- Mouse and keyboard behavior must stay equivalent where practical.

## Testing

Prefer focused regression tests for changed behavior, then run the full quality suite. Sender tests must use fake runners. Database tests must use synthetic temporary SQLite fixtures.

Optional integration checks are strictly read-only:

```sh
ASPEN_INTEGRATION_DB="$HOME/Library/Messages/chat.db" \
  go test ./internal/messages -run TestIntegration -v

ASPEN_CONTACTS_INTEGRATION=1 \
  go test ./internal/contacts -run TestIntegration -v
```

Manual real-send acceptance must be deliberate and performed by the user. See `docs/testing.md`.

## Releases

- Keep public behavior in `README.md`.
- Keep design rationale under `docs/design/`.
- Add notes under `docs/releases/vX.Y.Z.md`.
- Run the full quality suite before tagging.
- Publish with `./scripts/release.sh vX.Y.Z` from a clean `main` branch.
- Never move a published release tag.
