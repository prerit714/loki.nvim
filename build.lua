-- lazy.nvim runs this on install/update (`:Lazy build loki.nvim` reruns it):
-- compile the loki binary into the plugin's bin/, where lua/loki/init.lua
-- falls back to it when `loki` is not on PATH. Needs Go.
local root = vim.fn.fnamemodify(debug.getinfo(1, "S").source:sub(2), ":p:h")
local exe = vim.fn.has("win32") == 1 and "loki.exe" or "loki"
local out = vim.fs.joinpath(root, "bin", exe)

if vim.fn.executable("go") ~= 1 then
  error(
    "loki.nvim: `go` not found; install Go (https://go.dev/dl) and run "
      .. "`:Lazy build loki.nvim`, or install loki yourself and put it on PATH"
  )
end

local res = vim
  .system({ "go", "build", "-trimpath", "-o", out, "./cmd/loki" }, {
    cwd = root,
    text = true,
  })
  :wait()
if res.code ~= 0 then
  error("loki.nvim: go build failed:\n" .. (res.stderr or ""))
end
print("loki.nvim: built " .. out)
