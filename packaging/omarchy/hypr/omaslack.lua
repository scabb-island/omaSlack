-- omaslack window rule for Hyprland's Lua config (Hyprland 0.56+, as
-- Omarchy ships it). The plugin's full window is an omarchy-shell
-- FloatingWindow (class org.quickshell) titled exactly "omaslack": float
-- it, centered, at the size the window asks for (plugin/Window.qml).
--
-- `omaslack setup --window-rule` copies it to ~/.config/hypr/omaslack.lua.
-- Load it from ~/.config/hypr/hyprland.lua, after
-- require("default.hypr.omarchy"), which defines `o`:
--   if package.searchpath("hypr.omaslack", package.path) then require("hypr.omaslack") end
o.window({ class = "^org\\.quickshell$", title = "^omaslack$" }, { float = true, center = true, size = { 1100, 720 } })
