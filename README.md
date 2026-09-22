# Aspen

A keyboard-first terminal client for Apple Messages on macOS.

Aspen reads existing conversations and contacts directly from their local SQLite databases, renders message history in a responsive TUI, and sends text through Messages.app to the exact selected chat. It works locally or over SSH and supports native inline images in Ghostty and iTerm2.

The name comes from the quaking aspen: its leaves rustle in even a light breeze, giving the tree a quietly chatty character.

## Features

- Responsive split and narrow layouts
- Direct iMessage and SMS conversations
- Read-only Messages and Contacts database access
- Exact-chat AppleScript sending without participant fallback
- Per-conversation drafts and duplicate-send protection
- Conversation and loaded-message filtering
- Keyset pagination and cancellable full-history loading
- Mouse selection and conversation scrolling
- Rounded, outline-only message bubbles
- Native Ghostty/Kitty and iTerm2 inline images
- Automatic HEIC/HEIF conversion with bounded image decoding
- Private operation logs without message or contact content

## Requirements

- macOS with Messages.app configured
- Go 1.25.6 or newer when installing from source
- Full Disk Access for the process that launches Aspen
- Automation permission for Messages.app when sending
- Ghostty or iTerm2 for inline images; text works in other terminals

Apple Messages uses private local schemas. A macOS update can require query adjustments.

## Installation

Install the latest release:

```sh
go install github.com/Thelost77/aspen@latest
```

Ensure `$(go env GOPATH)/bin` is in `PATH`, then run:

```sh
aspen
```

Build from source:

```sh
git clone https://github.com/Thelost77/aspen.git
cd aspen
go build .
./aspen
```

## Permissions

### Full Disk Access

Aspen reads these databases:

```text
~/Library/Messages/chat.db
~/Library/Application Support/AddressBook/Sources/*/AddressBook-v22.abcddb
```

Grant Full Disk Access in:

```text
System Settings → Privacy & Security → Full Disk Access
```

Grant access to the exact process that launches Aspen. For a local session this is usually the terminal application. For SSH, the relevant remote-session process can differ. Restart that process after changing permission.

### Messages automation

The first send can trigger an Automation prompt. Allow Aspen to control Messages in:

```text
System Settings → Privacy & Security → Automation
```

Aspen does not modify TCC settings or request administrator privileges.

## Usage

```text
aspen [--db PATH] [--debug] [--graphics MODE] [--version]
```

Options:

```text
--db PATH        Messages chat.db path
--debug          enable debug operation logging
--graphics MODE  auto, kitty, iterm2, or off
--version        print version and exit
```

`--graphics auto` actively probes the terminal. Unidentified SSH sessions fall back to Kitty because Ghostty is the primary remote target. Use `--graphics iterm2` when terminal identity does not survive SSH.

## Keybindings

### Conversation list

| Key | Action |
|---|---|
| `j` / `k`, `↑` / `↓` | Move selection |
| `Enter` / `→` | Open conversation |
| `/` | Filter conversations |
| mouse click | Select or open conversation |
| mouse wheel | Move through conversations |

### Conversation

| Key | Action |
|---|---|
| `j` / `k`, `↑` / `↓`, wheel | Scroll history |
| `g` / `G` | Jump to top / bottom |
| `H` / `L`, `PageUp` / `PageDown` | Jump one page |
| `A` | Load all older messages; press again to stop |
| `/` | Filter loaded messages and attachment names |
| `i` | Compose a message |
| `r` | Refresh |
| `R` | Retry visible images |
| `Esc` / `←` | Return to conversation list |

### Composer

| Key | Action |
|---|---|
| `Enter` | Send |
| `Ctrl+J` | Insert newline |
| `Esc` | Cancel composition |

### Global

| Key | Action |
|---|---|
| `Tab` | Switch pane |
| `?` | Toggle help |
| `q` | Quit outside text input |
| `Ctrl+C` | Quit |

## Sending safety

Aspen only enables composition for existing one-to-one iMessage and SMS chats. Group and RCS sends remain disabled.

Each send captures the selected database chat ID and `chat.guid`. AppleScript receives the GUID and text as separate `argv` values, finds the exact Messages chat object, and sends to that object. Aspen never falls back to a display name, phone number, email address, or participant search.

A successful AppleScript command means Messages.app accepted the request; it does not prove delivery. Messages.app remains the delivery authority.

See [Messages safety design](docs/design/messages-safety.md) for the complete data and trust model.

## Inline images

Aspen automatically reserves image space inside the message outline:

- Ghostty uses Kitty Unicode placeholders so images follow normal TUI redraws and scrolling.
- iTerm2 uses OSC 1337.
- HEIC and HEIF attachments are converted locally with `/usr/bin/sips` (by extension or file brand).
- Temporary conversion files use owner-only `0600` permissions.
- Source size, pixel count, transfer dimensions, and Aspen's encoded-image cache are bounded.
- Kitty transfers use zlib compression to keep SSH payloads smaller.
- Aspen processes visible and near-visible images and evicts least-recently-visible cached payloads.
- Kitty terminal data is freed after an image leaves the viewport. Press `R` to retry visible images.

Inline images need a direct terminal graphics channel. Use SSH (or a local terminal), not mosh: mosh does not forward Kitty/iTerm2 graphics sequences. Under mosh Aspen shows `Image not available via mosh` instead of empty placeholders.

OSC 1337 has no source-cropping operation, so iTerm2 only places an image while the full image area is visible.

## Refresh and history

While Aspen is open, it checks SQLite's change version every two seconds with a lightweight read-only query. It reloads the conversation list and merges the latest page into the selected conversation only when the database changed. Polling stops when Aspen exits. Aspen also refreshes on startup, when selecting a conversation, on `r`, and after a successful local send.

History loads through read-only keyset pages. Press `A` to chain pages until the complete conversation is loaded. Press `A` again to stop after the current page.

## Privacy

- Messages and Contacts databases open with `mode=ro` and `PRAGMA query_only=1`.
- Aspen never writes to Apple databases.
- Message bodies, contact names, handles, chat GUIDs, and attachment paths are excluded from logs.
- No message cache, network service, analytics, or telemetry exists.
- Image decoding and conversion stay local.

Logs live at:

```text
~/.config/aspen/aspen.log
```

The directory is `0700`; log files and rotated logs are `0600`.

## Limitations

- macOS only
- Existing conversations only; Aspen does not start new chats
- Text sending only
- Group and RCS sending disabled
- Attachments are view-only
- No reactions, edits, replies, read receipts, or typing indicators
- No daemon or filesystem watcher; polling runs only while Aspen is open
- Private Apple schemas can change between macOS releases

## Development

```sh
go test ./... -count=1
go test -race ./...
go vet ./...
golangci-lint run
go build .
```

Optional read-only integration checks:

```sh
ASPEN_INTEGRATION_DB="$HOME/Library/Messages/chat.db" \
  go test ./internal/messages -run TestIntegration -v

ASPEN_CONTACTS_INTEGRATION=1 \
  go test ./internal/contacts -run TestIntegration -v
```

Integration checks never send messages. Real sending belongs only in intentional manual acceptance testing. See [Testing Aspen](docs/testing.md).

## Architecture

```text
main.go               process wiring and flags
internal/app          Bubble Tea state, commands, rendering, filtering
internal/messages     read-only Messages SQLite adapter
internal/contacts     read-only Contacts resolution
internal/sender       exact-chat AppleScript sender
internal/inlineimage  terminal detection, image conversion, protocols
internal/logger       private structured operation logging
internal/ui           width-safe formatting, styles, help
```

## Releases

Release notes live in [`docs/releases`](docs/releases). Maintainers publish an annotated tag and GitHub Release with:

```sh
./scripts/release.sh v0.1.0
```

## License

[MIT](LICENSE) © 2026 Wiktor Ziebka
