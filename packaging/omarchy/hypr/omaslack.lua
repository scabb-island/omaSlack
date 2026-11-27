-- omaslack window rule for Hyprland's Lua config (Hyprland 0.56+, as
-- Omarchy ships it). The plugin's full window is an omarchy-shell
-- FloatingWindow (class org.quickshell) titled exactly "omaslack": float
-- it, centered, at the size the window asks for (plugin/Window.qml).
--
-- Load it from ~/.config/hypr/hyprland.lua, after
-- require("default.hypr.omarchy"), which defines `o`:
--   require("hypr.omaslack")   -- with this file at ~/.config/hypr/omaslack.lua
o.window({ class = "^org\\.quickshell$", title = "^omaslack$" }, { float = true, center = true, size = { 1100, 720 } })
