-- Read-only rendered markdown view via the external `loki` TUI.
--
-- `open` runs `loki [--style dark|light] <file>` in a fullscreen floating
-- terminal (`:Loki` is defined in plugin/loki.lua; no default keymaps).
-- loki watches the file and re-renders on change, so a view left open stays in
-- sync after `:w`. Buffers without a file name, or with unsaved changes, are
-- snapshotted to a temp .md file first, which is rewritten (debounced) while the
-- buffer changes so the view stays live. The float, its buffer and the temp
-- file are wiped when loki exits (q / Esc / Ctrl+C).
-- `:checkhealth loki` (lua/loki/health.lua) runs `loki health`.
--
-- The binary is `loki` on PATH (see `loki setup`), else the copy build.lua
-- compiled into this plugin's bin/.
--
-- Config (read at call time, `setup` wins over globals):
--   vim.g.loki_cmd   = "loki" | { "path/to/loki", "--flag" }
--   vim.g.loki_style = "dark" | "light" | "auto" (default: from 'background')

local M = {}

---@class Loki.Config
---@field cmd? string|string[] Executable (or argv prefix) to run.
---@field style? "dark"|"light"|"auto" Value passed to `--style`.

---@type Loki.Config
local config = {}

---Runtime handles for the single open viewer.
---@class LokiState
---@field buf integer? Terminal buffer handle.
---@field win integer? Float window handle.
---@field tmp string? Temp file to delete when the viewer exits.
---@field timer uv.uv_timer_t? Debounce timer for rewriting `tmp`.
local state = {}

local augroup = vim.api.nvim_create_augroup("LokiFloat", { clear = true })

-- loki polls its file every 250ms, so this only coalesces bursts of typing.
local SYNC_DEBOUNCE_MS = 150

-- Plugin root: this file is <root>/lua/loki/init.lua.
local root = vim.fn.fnamemodify(debug.getinfo(1, "S").source:sub(2), ":p:h:h:h")

---Path of the binary build.lua compiles into the plugin.
---@return string
function M.bundled_bin()
  local exe = vim.fn.has("win32") == 1 and "loki.exe" or "loki"
  return vim.fs.joinpath(root, "bin", exe)
end

---Hint shown when the loki binary can't be found.
---@param exe string
---@return string
function M.missing_hint(exe)
  return string.format(
    "'%s' not found on PATH: install it with `go install "
      .. "github.com/prerit714/loki.nvim/cmd/loki@latest` and run `loki setup`, "
      .. "then restart Neovim. Or rebuild the plugin (`:Lazy build loki.nvim`, "
      .. "needs Go), or point vim.g.loki_cmd at the binary.",
    exe
  )
end

---Override defaults (optional; globals work without calling this).
---@param opts? Loki.Config
---@return nil
function M.setup(opts)
  config = vim.tbl_extend("force", config, opts or {})
end

---@return string[] argv Command prefix (executable first).
function M.base_cmd()
  local cmd = config.cmd or vim.g.loki_cmd
  if cmd == nil then
    cmd = "loki"
    if
      vim.fn.executable(cmd) ~= 1
      and vim.fn.executable(M.bundled_bin()) == 1
    then
      cmd = M.bundled_bin()
    end
  end
  if type(cmd) == "string" then
    return { cmd }
  end
  return vim.deepcopy(cmd)
end

---@return string style
local function style()
  return config.style or vim.g.loki_style or vim.o.background
end

-- Cover the whole editor except the command line. No border: a fullscreen
-- view has nothing to frame, and a border would just eat two rows/columns.
---@return vim.api.keyset.win_config geometry
local function float_geometry()
  return {
    relative = "editor",
    row = 0,
    col = 0,
    width = math.max(vim.o.columns, 1),
    height = math.max(vim.o.lines - vim.o.cmdheight, 1),
  }
end

---@return boolean valid
local function win_valid()
  return state.win ~= nil and vim.api.nvim_win_is_valid(state.win)
end

---@return nil
local function cleanup()
  vim.api.nvim_clear_autocmds({ group = augroup })
  if state.timer then
    state.timer:stop()
    state.timer:close()
  end
  if win_valid() then
    vim.api.nvim_win_close(state.win, true)
  end
  if state.buf and vim.api.nvim_buf_is_valid(state.buf) then
    vim.api.nvim_buf_delete(state.buf, { force = true })
  end
  if state.tmp then
    os.remove(state.tmp)
  end
  state = {}
  -- The TUI paints the whole screen; avoid stale cells on the window below.
  vim.schedule(function()
    pcall(vim.cmd, "redraw!")
  end)
end

---Resolve the file loki should render for `bufnr`, snapshotting the buffer to
---a temp .md file when it has no readable file or has unsaved changes.
---@param bufnr integer
---@return string path, string? tmp Path to view, and the temp file if any.
local function source_for(bufnr)
  local name = vim.api.nvim_buf_get_name(bufnr)
  if
    name ~= ""
    and vim.bo[bufnr].buftype == ""
    and not vim.bo[bufnr].modified
    and vim.fn.filereadable(name) == 1
  then
    return vim.fn.fnamemodify(name, ":p"), nil
  end
  local tmp = vim.fn.tempname() .. ".md"
  local lines = vim.api.nvim_buf_get_lines(bufnr, 0, -1, false)
  vim.fn.writefile(lines, tmp)
  return tmp, tmp
end

---Keep `tmp` in sync with `src`: rewrite it (debounced) whenever the buffer
---changes, so loki's file watcher re-renders the live buffer contents.
---@param src integer Source buffer that was snapshotted.
---@param tmp string Temp file loki is rendering.
---@return nil
local function sync_tmp(src, tmp)
  local timer = assert(vim.uv.new_timer())
  state.timer = timer
  vim.api.nvim_create_autocmd({ "TextChanged", "TextChangedI" }, {
    group = augroup,
    buffer = src,
    desc = "Rewrite the loki temp snapshot",
    callback = function()
      timer:stop()
      timer:start(
        SYNC_DEBOUNCE_MS,
        0,
        vim.schedule_wrap(function()
          if state.tmp ~= tmp or not vim.api.nvim_buf_is_valid(src) then
            return
          end
          local lines = vim.api.nvim_buf_get_lines(src, 0, -1, false)
          vim.fn.writefile(lines, tmp)
        end)
      )
    end,
  })
end

---Open loki on `path` in a fullscreen float.
---@param path string File to render.
---@param tmp? string Temp file owned by this viewer (deleted on exit).
---@param src? integer Buffer `tmp` snapshots; kept in sync while open.
---@return nil
local function launch(path, tmp, src)
  local cmd = M.base_cmd()
  if vim.fn.executable(cmd[1]) ~= 1 then
    if tmp then
      os.remove(tmp)
    end
    vim.notify("loki: " .. M.missing_hint(cmd[1]), vim.log.levels.ERROR)
    return
  end

  -- One viewer at a time: replace any existing one.
  if state.buf then
    cleanup()
  end

  vim.list_extend(cmd, { "--style", style(), path })

  local buf = vim.api.nvim_create_buf(false, true)
  local cfg = float_geometry()
  cfg.style = "minimal"
  cfg.border = "none"
  cfg.zindex = 60
  local win = vim.api.nvim_open_win(buf, true, cfg)
  vim.wo[win].winfixbuf = true
  state.buf, state.win, state.tmp = buf, win, tmp

  local job = vim.fn.jobstart(cmd, {
    term = true,
    -- Relative links/images in the markdown resolve against its directory.
    cwd = vim.fn.fnamemodify(path, ":h"),
    ---@param code integer
    on_exit = function(_, code)
      -- A replaced viewer exits after its successor started.
      if state.buf ~= buf then
        return
      end
      if code == 0 then
        cleanup()
        return
      end
      -- Give the terminal a moment to flush loki's final (stderr) output
      -- before reading it back for the error message.
      vim.defer_fn(function()
        if state.buf ~= buf then
          return
        end
        local lines = vim.tbl_filter(function(l)
          return vim.trim(l) ~= "" and not l:match("^%[Process exited")
        end, vim.api.nvim_buf_get_lines(buf, 0, -1, false))
        vim.notify(
          string.format(
            "loki exited with code %d%s",
            code,
            #lines > 0 and (":\n" .. table.concat(lines, "\n")) or ""
          ),
          vim.log.levels.ERROR
        )
        cleanup()
      end, 100)
    end,
  })
  if job <= 0 then
    cleanup()
    vim.notify("loki: failed to start " .. cmd[1], vim.log.levels.ERROR)
    return
  end

  ---@type vim.keymap.set.Opts
  local map_opts = { buffer = buf, silent = true, nowait = true }
  -- loki quits on a single <Esc>; the global <Esc><Esc> terminal map would
  -- otherwise delay it by 'timeoutlen'. <C-\><C-n> still leaves terminal mode.
  vim.keymap.set("t", "<Esc>", "<Esc>", map_opts)
  -- If terminal mode was left, `q` closes the viewer from normal mode too.
  vim.keymap.set("n", "q", cleanup, map_opts)

  if tmp and src then
    sync_tmp(src, tmp)
  end

  vim.api.nvim_create_autocmd("VimResized", {
    group = augroup,
    desc = "Keep the loki view fullscreen",
    callback = function()
      if win_valid() then
        vim.api.nvim_win_set_config(state.win, float_geometry())
      end
    end,
  })

  vim.cmd("startinsert")
end

---View `bufnr` (default: current buffer) rendered by loki.
---@param bufnr? integer
---@return nil
function M.open(bufnr)
  if bufnr == nil or bufnr == 0 then
    bufnr = vim.api.nvim_get_current_buf()
  end
  local path, tmp = source_for(bufnr)
  launch(path, tmp, bufnr)
end

---View a markdown file on disk; an open buffer for it is used instead so
---unsaved changes are shown.
---@param file string
---@return nil
function M.open_file(file)
  local path = vim.fn.fnamemodify(vim.fn.expand(file), ":p")
  local bufnr = vim.fn.bufnr(path)
  if bufnr ~= -1 and vim.api.nvim_buf_is_loaded(bufnr) then
    return M.open(bufnr)
  end
  if vim.fn.filereadable(path) ~= 1 then
    vim.notify("loki: cannot read " .. file, vim.log.levels.ERROR)
    return
  end
  launch(path)
end

return M
