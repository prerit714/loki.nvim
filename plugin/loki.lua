-- Defines `:Loki [file]`. No keymaps are set; see the README for a suggested
-- markdown-only mapping. The module itself loads on first use.
if vim.g.loaded_loki then
  return
end
vim.g.loaded_loki = true

vim.api.nvim_create_user_command("Loki", function(args)
  if args.args ~= "" then
    require("loki").open_file(args.args)
  else
    require("loki").open()
  end
end, {
  nargs = "?",
  complete = "file",
  desc = "View markdown rendered by loki (current buffer or [file])",
})
