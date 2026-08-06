# Messages safety design

Aspen combines private local Apple data with an operation that can contact another person. Its design therefore separates read access, display identity, stable selection, and sending.

## Data flow

```text
~/Library/Messages/chat.db ──read-only──┐
                                      ├──> Bubble Tea model ──> terminal
Contacts databases ─────────read-only──┘

captured chat.guid + text ──argv──> /usr/bin/osascript ──> Messages.app
```

Aspen has no daemon, server, analytics endpoint, or message cache.

## Read-only databases

Messages and Contacts databases are opened through SQLite URI paths with `mode=ro`. Each connection enables:

```sql
PRAGMA query_only = 1;
```

Aspen does not execute inserts, updates, deletes, schema changes, checkpoints, repairs, or migrations against Apple databases. Conversation and history queries are bounded. Older history uses `(date, ROWID)` keyset cursors rather than `OFFSET` scans.

Contacts are display-only. Failure to load a Contacts database leaves the underlying Messages handle visible and cannot change a send target.

## Stable selection

Conversation list positions can change after refresh. Aspen therefore uses database chat IDs for model state and drafts rather than visible indexes.

Async conversation loads carry a generation. Message loads carry both a chat ID and generation. A result is ignored when it no longer belongs to the active request. Switching chats also stops full-history chaining after the in-flight read completes.

## Exact-chat sending

When composition begins, Aspen captures an immutable target containing:

- database `ChatID`;
- `chat.guid`;
- service name;
- group status.

The sender rejects missing GUIDs, empty messages, groups, and services other than one-to-one iMessage or SMS.

AppleScript receives the GUID and message text as separate command-line arguments:

```text
/usr/bin/osascript - <chat.guid> <message text>
```

The fixed script asks Messages.app for chats whose `id` equals the captured GUID and sends to that exact object. No participant fallback exists. Aspen never targets a display name, resolved contact, phone number, email address, or current list position.

The argument boundary also means quotes, Unicode, and multiline text cannot become AppleScript source.

## Send lifecycle

Only one send can be pending for a draft at a time. Repeated Enter events do not create duplicate commands. A failed send preserves the draft and returns classified guidance for permission, unavailable app, missing chat, recipient, timeout, or script failures.

After Messages.app accepts a send command, Aspen refreshes the selected history. Acceptance does not prove delivery; Messages.app remains authoritative for delivery state.

## Restricted targets

Group and RCS sending remain disabled. Exact object selection prevents accidental participant fallback, but these services still require deliberate real-world verification before being exposed as composable targets.

Aspen also does not start new conversations. It operates only on chats already present in the local Messages database.

## Private logging

The logger records operation names, durations, counts, stable numeric IDs, protocols, and error classes. Tests assert that message text, names, handles, GUIDs, and attachment paths do not enter logs.

Runtime storage:

```text
~/.config/aspen/          mode 0700
~/.config/aspen/aspen.log mode 0600
```

Logs rotate locally at 5 MB. Aspen has no telemetry.

## Attachment handling

Attachment paths come from the local Messages database and are used only for display. Aspen checks that an image is a regular file below the source-size limit, bounds decoded pixel count, resizes transfer payloads, and encodes terminal output locally.

HEIC/HEIF files are converted with `/usr/bin/sips` into owner-only temporary files. Temporary files are removed after encoding. Ghostty uses Kitty graphics; iTerm2 uses OSC 1337. Kitty placement IDs are deleted before redraws, overlays, and shutdown.

## Trust boundary

Aspen trusts macOS and Messages.app to own account state, routing, and delivery. It trusts the local Apple databases as read-only input but isolates their private schema behind `internal/messages` and `internal/contacts`.

Private Apple schemas can change. Unsupported schema errors should fail clearly rather than trigger writes or broad compatibility guesses.
