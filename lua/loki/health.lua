-- `:checkhealth loki`: runs `loki health` and maps each line's prefix
-- (`OK  `, `WARN`, `FAIL`, `INFO`) onto vim.health. The prefix format is a
-- contract with cmd/loki/health.go; keep them in sync.

local M = {}

---@type table<string, fun(msg: string)>
local reporters = {
  OK = vim.health.ok,
  WARN = vim.health.warn,
  FAIL = vim.health.error,
  INFO = vim.health.info,
}

---@return nil
function M.check()
  vim.health.start("loki")

  local loki = require("loki")
  local cmd = loki.base_cmd()
  if vim.fn.executable(cmd[1]) ~= 1 then
    vim.health.error(loki.missing_hint(cmd[1]))
    return
  end
  vim.health.info("Using: " .. vim.fn.exepath(cmd[1]))
  -- The plugin's own build works without `loki setup`; that only matters for
  -- running loki outside Neovim, so its install/PATH failures are just advice.
  local bundled = cmd[1] == loki.bundled_bin()
  if bundled then
    vim.health.info(
      "Using the binary built into the plugin; `loki setup` is optional "
        .. "(only needed to run loki outside Neovim)."
    )
  end

  table.insert(cmd, "health")
  local ok, proc = pcall(vim.system, cmd, { text = true })
  if not ok then
    vim.health.error("failed to run `loki health`: " .. tostring(proc))
    return
  end
  local res = proc:wait(15000)
  -- On timeout wait() kills the process and reports exit code 124.
  if res.code == 124 then
    vim.health.error("`loki health` timed out after 15s")
    return
  end

  local reported = false
  for line in vim.gsplit(res.stdout or "", "\r?\n") do
    line = line:gsub("\27%[[%d;]*m", "")
    local prefix, msg = line:match("^(%u+)%s+(.*)$")
    local report = prefix and reporters[prefix]
    if bundled and prefix == "FAIL" and msg:find("loki setup", 1, true) then
      report = vim.health.warn
    end
    if report then
      report(msg)
      reported = true
    elseif vim.trim(line) ~= "" then
      vim.health.info(line)
    end
  end

  local stderr = vim.trim(res.stderr or "")
  if stderr ~= "" then
    vim.health.warn("stderr: " .. stderr)
  end
  if not reported then
    vim.health.error(
      string.format("`loki health` printed no checks (exit %d)", res.code)
    )
  end
end

return M
