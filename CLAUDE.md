# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

loki.nvim is two things in one repo:

- `cmd/loki/` — `loki`, a Go TUI (Bubble Tea + bubbles viewport + glamour) that renders one markdown file read-only, fullscreen, in the alternate screen. It also has `setup` / `health` / `version` subcommands.
- `lua/`, `plugin/`, `build.lua` — a Neovim plugin that runs `loki` on the current buffer in a fullscreen floating terminal.

Module path is `github.com/prerit714/loki.nvim`; the main package lives in `cmd/loki` so `go install …/cmd/loki` produces a binary named `loki` (not `loki.nvim`). Much of the code is AI-generated; the README says so and that note should stay.

## Commands

```sh
go test ./...                     # model + setup tests
go vet ./...
GOOS=linux  go build -o /dev/null ./cmd/loki   # keep all three platforms building:
GOOS=darwin go build -o /dev/null ./cmd/loki   # path_windows.go vs path_unix.go
go build -o bin/loki ./cmd/loki   # what build.lua does (bin/ is gitignored)

stylua lua plugin build.lua       # .stylua.toml: 80 cols, 2 spaces, double quotes
stylua --check lua plugin build.lua

# Plugin smoke test without a plugin manager:
nvim --headless --clean --cmd "set rtp^=." "+checkhealth loki" "+w! out.txt" +qa
```

## Architecture

### The TUI (`cmd/loki`)

- `main.go` dispatches subcommands (`setup`, `health`, `version`) before flag parsing; anything else is a file to view (or stdin when piped — then keys come from the TTY via `tea.WithInputTTY`).
- `model.go` is the Bubble Tea model. `render()` re-runs glamour at the current width (capped by `-width`, centered with left padding) and restores the scroll offset, so resizes and reloads don't jump.
- **Watching is polling**, not fsnotify: a `tea.Tick` every 250ms stats the file off the main loop (`checkFile`) and only emits `reloadMsg` when size/mtime changed. Errors are ignored on purpose (editors that save by rename briefly delete the file).
- **Search** lowercases ANSI-stripped rendered lines and marks matching lines with a `>` in the left gutter. It deliberately does not recolor text inside glamour's ANSI output.
- The status bar is the last row; the viewport gets `height-1`.

### setup / health

- `setup.go`: copies `os.Executable()` to the install dir via temp file + rename. If the target is running (Windows can't overwrite a running exe) it renames the old one to `.old` first. Default dir: `%LOCALAPPDATA%\Programs\loki` on Windows, `~/.local/bin` elsewhere.
- `path_windows.go`: edits `HKCU\Environment\Path`. It preserves the value type (`REG_EXPAND_SZ` entries like `%PNPM_HOME%` stay unexpanded), compares entries case-insensitively after expansion, then broadcasts `WM_SETTINGCHANGE` so new processes see it.
- `path_unix.go`: writes a `# >>> loki >>>` … `# <<< loki <<<` block to `~/.profile` (always), plus existing `.bashrc`/`.zshrc`, and `conf.d/loki.fish` when fish is configured. The block is replaced, never duplicated; `-uninstall` removes it.
- `health.go`: each line is printed as `%-4s  message` with level `OK`, `WARN`, `FAIL` or `INFO`. It exits 1 on any FAIL. **This is a contract with `lua/loki/health.lua`**, which parses those prefixes into `vim.health.*`; change both together. The Neovim probe runs `nvim` with `cmd.Dir = os.TempDir()`, because Windows `exepath()` searches the cwd first and would find a stray build output.

### The Neovim plugin

- `plugin/loki.lua` only defines `:Loki [file]`. No default keymaps; the README suggests one.
- `lua/loki/init.lua`:
  - **Binary resolution** (`base_cmd`): `setup{cmd}`/`vim.g.loki_cmd`, else `loki` on PATH, else `bin/loki[.exe]` under the plugin root, found from `debug.getinfo` of this file. The command is always an argv list passed to `jobstart({ term = true })`, never a shell string, so paths with spaces are safe.
  - **One viewer at a time:** a borderless float covering the editor minus the cmdline. `on_exit` checks `state.buf ~= buf` so a replaced viewer's late exit is ignored. A non-zero exit reads the terminal lines back into an ERROR notification.
  - **Unsaved or nameless buffers** are snapshotted to `tempname() .. ".md"`. TextChanged/TextChangedI on the source buffer rewrite it through one 150ms-debounced uv timer, so loki's file poll picks it up. `cleanup()` stops the timer, clears the augroup and deletes the temp file.
  - **`<Esc>`** gets a buffer-local `nowait` terminal map that passes it straight through. Otherwise a user's `<Esc><Esc>` terminal mapping would delay loki's quit by `timeoutlen`.
- `lua/loki/health.lua`: `:checkhealth loki`. When the bundled binary is in use, the `loki setup` FAILs are downgraded to warnings, since PATH only matters outside Neovim.
- `build.lua`: lazy.nvim runs it automatically. It shells out to `go build -trimpath -o bin/loki ./cmd/loki` and `error()`s with a readable message when Go is missing or the build fails.

## Gotchas

- On Windows, headless Neovim's `:terminal` renders nothing (not even `cmd /c echo hi`), so the viewer can't be screen-scraped there. Test TUI behavior through the model in Go (`model_test.go`). Test the plugin through module loads, `base_cmd()` and `:checkhealth loki` output.
- `loki health` compares the installed binary with the running one by content. Plain `go build` binaries all report version `dev` (or `dev-<rev>` inside a git checkout), so trust the content check over the version string.
- Don't introduce Nerd Font glyphs in plugin UI or TUI output; plain ASCII only.
