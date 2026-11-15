-- Exercise the actual widget response for rejected actions and execution errors.
return {
	rejection_is_distinct_from_exception = function()
		local handler, sent
		local sc = {
			get = function() return { id = 1, ai = true, role = "economy" } end,
			tune = function() return nil, "cooldown" end,
		}
		local env = setmetatable({
			widget = {}, WG = { SubCommanders = sc },
			Spring = { GetConfigString = function(_, default) return default end, GetSpectatingState = function() return false end, Echo = function() end },
			VFS = { Include = function(path)
				if path:find("sc_guard", 1, true) then return function() return { widget = function() end } end end
				return function(opts)
					handler = opts.onMessage
					return { send = function(msg) sent = msg end }
				end
			end },
		}, { __index = _G })
		assert(loadfile("widget/LuaUI/Widgets/gui_subcommanders_ai.lua", "t", env))()
		env.widget:Initialize()
		handler({ type = "action", id = "1", sub = 1, op = "tune_rules", args = { key = "x", value = 1 } })
		T.eq(sent.ok, false)
		T.eq(sent.rejected, true)
		T.eq(sent.error, "cooldown")
		sc.tune = function() error("unexpected failure") end
		handler({ type = "action", id = "2", sub = 1, op = "tune_rules", args = { key = "x", value = 1 } })
		T.eq(sent.ok, false)
		T.eq(sent.rejected, nil)
	end,
}
