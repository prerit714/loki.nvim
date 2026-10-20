# loki.nvim

A read-only, fullscreen markdown viewer for the terminal, plus a Neovim plugin
that opens it on the current buffer.

`loki` is a small Go program built on [Bubble Tea](https://github.com/charmbracelet/bubbletea)
and [glamour](https://github.com/charmbracelet/glamour). It renders a markdown
file in the alternate screen, lets you scroll and search it, and re-renders when
the file changes on disk — so a view left open follows your saves.

> [!NOTE]
> **A lot of this project's code is AI-generated.** It was written largely with
> [Claude Code](https://claude.com/claude-code) and then reviewed, tested and
> used by the author. Read it with that in mind, and please open an issue if
> something looks off.

## Requirements

- Go 1.24+ to build (the plugin compiles the binary for you)
- Neovim 0.11+ for the plugin (uses `vim.system`, `vim.fs.joinpath`, `jobstart({ term = true })`)
- A terminal with true-color support looks best; Windows, Linux and macOS all work

## Screenshots

<img width="1920" height="1080" alt="image" src="https://github.com/user-attachments/assets/d6f92a4f-21f4-41a4-b0b4-2f470a3f17c8" />
<img width="1920" height="1080" alt="image" src="https://github.com/user-attachments/assets/4cb7b620-2d32-413f-9cbf-93b9c7d9ba6b" />


## Neovim setup

### lazy.nvim

```lua
{
  "prerit714/loki.nvim",
  -- build.lua (run automatically by lazy) compiles bin/loki with `go build`.
  cmd = "Loki",
  keys = {
    {
      "<leader>M",
      function() require("loki").open() end,
      ft = "markdown",
      desc = "Markdown view (loki)",
    },
  },
}
```

That's all: open a markdown file, press `<leader>M` (or run `:Loki`), read, and
press `q` to go back. Pick any key you like; the plugin sets none itself.

### Other plugin managers

Install `prerit714/loki.nvim` as usual, then build the binary once from the
plugin directory:

```sh
cd /path/to/loki.nvim && go build -o bin/loki ./cmd/loki     # bin/loki.exe on Windows
```

and map a key to `require("loki").open()`. `:Loki` is defined automatically by
`plugin/loki.lua`.

### Which binary is used

1. `vim.g.loki_cmd` / `require("loki").setup({ cmd = ... })` if set
2. otherwise `loki` on your `PATH` (see [Standalone CLI](#standalone-cli))
3. otherwise the plugin's own `bin/loki` built by `build.lua`

So the plugin works without touching your `PATH`; run `loki setup` only if you
also want `loki` in your shell.

### Commands and options

| What                          | Description                                                       |
|-------------------------------|-------------------------------------------------------------------|
| `:Loki`                       | view the current buffer                                           |
| `:Loki {file}`                | view a file (uses its open buffer, if any, so unsaved edits show) |
| `require("loki").open(bufnr?)`| Lua API behind both                                               |
| `:checkhealth loki`           | runs `loki health` and shows the results                          |

```lua
-- All optional; globals work without calling setup().
require("loki").setup({
  cmd = "loki",          -- or { "/path/to/loki", "--width", "120" }
  style = "dark",        -- "dark" | "light" | "auto"; default follows 'background'
})
-- equivalently: vim.g.loki_cmd = ..., vim.g.loki_style = ...
```

How it behaves inside Neovim:

- The view is a borderless float covering the whole editor, in terminal mode, so
  keys go straight to loki. `q`, `Esc` or `Ctrl+C` closes it.
- A single `Esc` quits immediately even if you map `<Esc><Esc>` in terminal
  mode; `<C-\><C-n>` still leaves terminal mode, and then `q` closes the view.
- Unsaved or unnamed buffers are viewed through a temp file that is rewritten
  as the buffer changes and deleted when the view closes.
- Relative links and images resolve against the markdown file's directory.

## Standalone CLI

```sh
go install github.com/prerit714/loki.nvim/cmd/loki@latest
loki setup     # copy loki to a per-user dir and add it to your user PATH
loki health    # from a new terminal: check the install, PATH, terminal and Neovim
```

`loki setup` installs to `%LOCALAPPDATA%\Programs\loki` on Windows (and adds it
to `HKCU\Environment\Path`) or `~/.local/bin` elsewhere (and adds a marked block
to `~/.profile` plus any existing `~/.bashrc`, `~/.zshrc` and fish `conf.d`). It
is idempotent: re-run it from a newer build to upgrade. `loki setup -uninstall`
reverses it, `-dir` picks another directory and `-no-path` skips PATH changes.
Open a new terminal (and restart Neovim) afterwards so the PATH change is seen.

`loki health` prints one `OK` / `WARN` / `FAIL` / `INFO` line per check and
exits 1 if anything failed. It also warns when another `loki` earlier on `PATH`
shadows the installed one.

### Usage

```sh
loki README.md
loki -style light notes.md
cat notes.md | loki
```

| Flag        | Default | Meaning                                                    |
|-------------|---------|------------------------------------------------------------|
| `-style`    | `dark`  | glamour style: `dark`, `light`, `auto`, `notty`, `dracula`, ... |
| `-width`    | `100`   | max wrap width, centered in wider terminals; `0` = full width |
| `-no-watch` | off     | don't re-render when the file changes on disk              |

| Keys                     | Action                  |
|--------------------------|-------------------------|
| `j`/`k`, arrows, wheel   | scroll                  |
| `d`/`u`, `f`/`b`, space  | half page / page        |
| `g`/`G`, home/end        | top / bottom            |
| `/`, then `n`/`N`        | search, next/prev match |
| `r`                      | force reload            |
| `q`, `esc`, `ctrl+c`     | quit (exit code 0)      |

The file is polled every 250ms and re-rendered on change, keeping the scroll
position. Errors (missing file, etc.) go to stderr with exit code 1.

## Troubleshooting

- **"'loki' not found on PATH"** — run `:Lazy build loki.nvim` (needs Go), or
  install the CLI and run `loki setup`, then restart Neovim.
- **Neovim still can't find loki after `loki setup`** — Neovim inherits `PATH`
  from where it was started; start it from a new terminal. `:checkhealth loki`
  shows what it resolves to.
- **Colors look wrong** — set `style` explicitly; `auto` asks the terminal for
  its background color, which not every terminal answers.

## Development

See [CLAUDE.md](CLAUDE.md) for the layout, commands and design notes.

```sh
go test ./... && go vet ./...
stylua --check lua plugin build.lua
```

## License

[MIT](LICENSE) © 2026 Prerit Dayal
