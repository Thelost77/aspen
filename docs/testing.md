# Testing Aspen

Automated tests never send real messages. Database integration checks are read-only. Any real send must be deliberate, manual, and directed by the user.

## Automated quality suite

```sh
gofmt -w $(find . -name '*.go')
go mod tidy
go mod verify
go test ./... -count=1
go test -race ./...
go vet ./...
golangci-lint run
go build .
```

The test suite covers synthetic Messages and Contacts databases, stale async results, stable chat selection, sender arguments, draft isolation, pagination, filtering, width-safe rendering, privacy, inline protocol output, and image placement lifecycle.

## Read-only integration checks

These checks open actual local databases read-only. They print no message bodies or contact records.

```sh
ASPEN_INTEGRATION_DB="$HOME/Library/Messages/chat.db" \
  go test ./internal/messages -run TestIntegration -v

ASPEN_CONTACTS_INTEGRATION=1 \
  go test ./internal/contacts -run TestIntegration -v
```

Unset variables skip integration checks.

## Manual acceptance

Build the exact candidate:

```sh
go build -o aspen .
./aspen --debug
```

### Startup and permissions

1. Start without Full Disk Access and confirm the error identifies that permission.
2. Grant access to the launching process, restart it, and confirm conversations load.
3. Confirm contact names resolve where available without changing raw chat targets.
4. Inspect `~/.config/aspen/aspen.log`; permissions must be `0600` and content must not include messages, names, handles, GUIDs, or attachment paths.

### Navigation and layout

1. Move with arrows and `j`/`k`; open with Enter and mouse click.
2. Scroll conversation rows and message history with the mouse wheel.
3. Switch panes with Tab and return with Esc/Left.
4. Resize across split and narrow layouts.
5. Confirm borders, bubbles, dates, and footer remain inside the terminal bounds.
6. Open `?` at multiple sizes and confirm it closes cleanly.

### Filtering and history

1. Use `/` to filter conversations; Enter applies and Esc clears.
2. Use `/` in history to filter loaded text and attachment names.
3. Clear an applied message filter and confirm the previous scroll offset returns.
4. Press `A` to load all history and watch progress increase.
5. Press `A` again and confirm loading stops after the in-flight page.
6. Switch chats during loading and confirm the old chat does not replace current state.

### Inline images

1. Test PNG/JPEG and HEIC/HEIF attachments in Ghostty.
2. Confirm each image remains inside its message outline.
3. Scroll an image partly behind the top and bottom of history; Ghostty must clip its placeholder rows without covering the header or composer.
4. Open and close help repeatedly while a Kitty image is visible; its placeholders must disappear and return each time.
5. Press `R` and confirm visible images return after terminal-side image loss.
6. Load image-heavy history and confirm Aspen processes only visible and near-visible attachments.
7. Repeat in iTerm2 and confirm full images render without low-quality block fallback.
8. Use `--graphics kitty`, `--graphics iterm2`, and `--graphics off` for protocol diagnosis.

### Draft safety

1. Start drafts in two direct chats and switch repeatedly.
2. Confirm each chat retains only its own draft.
3. Press Enter repeatedly during a fake or denied send and confirm no duplicate command starts.
4. Deny Automation permission and confirm the draft survives with actionable guidance.
5. Confirm group and RCS chats cannot enter composition.

### Intentional direct send

This step contacts another person. Do it only with a chosen test recipient who expects the message.

1. Select an existing one-to-one conversation.
2. Verify its title and recent history against Messages.app.
3. Send text containing quotes, Unicode, and a newline.
4. Confirm only that exact conversation receives the message.
5. Confirm Aspen refreshes history after Messages.app accepts the command.
6. Repeat once for SMS only when a suitable test recipient is available.

A wrong-recipient send is a release blocker.

### SSH

1. Run Aspen through the actual SSH launch path with Full Disk Access.
2. Verify keyboard, mouse, resize, refresh, filtering, and quit behavior.
3. Confirm Ghostty-over-SSH image protocol selection in the log.
4. Disconnect during loading, then reconnect and confirm clean startup.

## Release acceptance

Release only when:

- automated tests, race detector, vet, lint, and build pass;
- no private fixture or runtime data exists in the repository;
- direct-send manual acceptance reaches only the selected chat;
- Ghostty/SSH rendering works in the intended environment;
- group and RCS sending remain disabled until separately verified.
