-- Exercise the actual Autopilot widget with the same sub-commanders across a reload.
local function fixture(existing)
	local frame = 10
	local subs = existing or {}
	local created, changed = {}, {}
	local defs = { [1] = { commander = true, builder = true }, [2] = { factory = true }, [3] = { builder = true } }
	local sc = {
		list = function() local list = {} for _, sub in pairs(subs) do list[#list + 1] = sub end return list end,
		get = function(id) return subs[id] end,
		create = function(role) local id = #created + 1 while subs[id] do id = id + 1 end
			subs[id] = { id = id, role = role, units = {}, counts = { builders = 0, factories = 0, combat = 0 } }
			created[#created + 1] = id return id end,
		assign = function(id, units) for _, unit in ipairs(units) do subs[id].units[#subs[id].units + 1] = { id = unit } end end,
		ownerOf = function(unit) for id, sub in pairs(subs) do for _, u in ipairs(sub.units) do if u.id == unit then return id end end end end,
		setAI = function(id, on) subs[id].ai = on changed[#changed + 1] = { id, on } end,
		setAutonomy = function() end, setDirective = function() end,
	}
	local env = setmetatable({
		widget = {}, WG = { SubCommanders = sc }, Game = { mapName = "test map" },
		Spring = { GetMyTeamID = function() return 1 end, GetGameFrame = function() return frame end,
			GetTeamUnits = function() return { 100 } end, GetUnitDefID = function(id) return id == 101 and 3 or 1 end,
			GetSpectatingState = function() return false end, GetUnitIsBeingBuilt = function() return false end,
			GetTeamResources = function() return 0, 0, 0, 0 end, Echo = function() end },
		UnitDefs = { [1] = { buildOptions = { 2 } }, [2] = { name = "armlab", buildOptions = { 3 } } },
		VFS = { LoadFile = function() return nil end, Include = function() return defs end },
	}, { __index = _G })
	assert(loadfile(os.getenv("AUTOPILOT_WIDGET") or "widget/LuaUI/Widgets/gui_subcommanders_autopilot.lua", "t", env))()
	return env.widget, subs, created, changed, function(n) frame = n end
end

return {
	ai_command_works_before_the_first_game_frame = function()
		local w, subs = fixture()
		w:Initialize()
		T.eq(w:TextCommand("autopilot ai"), true)
		w:GameFrame(10)
		T.eq(subs[1].ai, true)
	end,
	reload_keeps_ownership_and_ai_switch = function()
		local first, subs, created, _, setFrame = fixture()
		first:Initialize()
		first:GameFrame(10)
		T.eq(#created, 1)
		first:TextCommand("autopilot ai")
		setFrame(120)
		local saved = first:GetConfigData()
		local nextWidget, _, newCreated, changed, nextFrame = fixture(subs)
		nextFrame(121)
		nextWidget:SetConfigData(saved)
		nextWidget:Initialize()
		nextWidget:GameFrame(121)
		T.eq(#newCreated, 0, "reload must reuse the existing economy")
		nextWidget:TextCommand("autopilot rules")
		nextWidget:TextCommand("autopilot ai")
		T.eq(#changed, 2)
		T.eq(changed[1][1], 1)
		T.eq(changed[1][2], false)
		T.eq(changed[2][1], 1)
		T.eq(changed[2][2], true)
	end,
	early_ai_choice_survives_reload_before_first_frame = function()
		local first, subs, _, _, setFrame = fixture()
		first:Initialize() first:GameFrame(10) first:TextCommand("autopilot ai") setFrame(120)
		local saved = first:GetConfigData()
		local nextWidget, _, _, _, nextFrame = fixture(subs)
		nextFrame(121) nextWidget:SetConfigData(saved) nextWidget:Initialize()
		T.eq(nextWidget:TextCommand("autopilot rules"), true)
		local savedAgain = nextWidget:GetConfigData()
		local third, _, _, _, thirdFrame = fixture(subs)
		thirdFrame(122) third:SetConfigData(savedAgain) third:Initialize()
		local savedThird = third:GetConfigData()
		local fourth, _, newCreated, changed, fourthFrame = fixture(subs)
		fourthFrame(123) fourth:SetConfigData(savedThird) fourth:Initialize() fourth:GameFrame(123)
		T.eq(#newCreated, 0, "repeated reloads must reuse the existing sub")
		T.eq(subs[1].ai, false, "early rules command must override saved AI mode")
		T.eq(#changed, 1)
		T.eq(changed[1][1], 1)
	end,
	reload_does_not_adopt_a_reused_id_with_the_wrong_role = function()
		local first, subs, _, _, setFrame = fixture()
		first:Initialize() first:GameFrame(10) setFrame(120)
		local saved = first:GetConfigData()
		saved.game.ids.def = 2
		subs[2] = { id = 2, role = "army", units = {}, counts = { builders = 0, factories = 0, combat = 0 } }
		local nextWidget, _, _, changed, nextFrame = fixture(subs)
		nextFrame(121) nextWidget:SetConfigData(saved) nextWidget:Initialize() nextWidget:GameFrame(121)
		nextWidget:TextCommand("autopilot ai")
		T.eq(#changed, 1)
		T.eq(changed[1][1], 1)
	end,
	missing_economy_does_not_duplicate_others_or_take_manual_units = function()
		local first, subs, _, _, setFrame = fixture()
		first:Initialize() first:GameFrame(10) setFrame(120)
		local saved = first:GetConfigData()
		saved.game.ids.def = 2
		subs[1] = nil
		subs[2] = { id = 2, role = "defense", units = {}, counts = { builders = 0, factories = 0, combat = 0 } }
		subs[4] = { id = 4, role = "army", units = { { id = 100 } }, counts = { builders = 0, factories = 0, combat = 0 } }
		local nextWidget, _, created, _, nextFrame = fixture(subs)
		nextFrame(121) nextWidget:SetConfigData(saved) nextWidget:Initialize() nextWidget:GameFrame(121)
		T.eq(#created, 1, "only the missing economy should be created")
		T.eq(subs[created[1]].role, "economy")
		T.eq(#subs[created[1]].units, 0, "do not steal a commander's units")
		T.eq(#subs[4].units, 1)
	end,
	new_subcommanders_keep_autopilot_ai_after_reload = function()
		local first, subs, _, _, setFrame = fixture()
		first:Initialize() first:GameFrame(10) first:TextCommand("autopilot ai")
		subs[1].units[#subs[1].units + 1] = { id = 101 }
		setFrame(120)
		local saved = first:GetConfigData()
		local nextWidget, _, created, _, nextFrame = fixture(subs)
		nextFrame(121) nextWidget:SetConfigData(saved) nextWidget:Initialize() nextWidget:GameFrame(121)
		nextFrame(5400) nextWidget:GameFrame(5400)
		T.eq(#created, 2, "defense and army should be created once")
		T.eq(subs[created[1]].ai, true)
		T.eq(subs[created[2]].ai, true)
	end,
	old_game_state_is_not_restored_to_a_new_game = function()
		local first, _, _, _, frame = fixture()
		first:Initialize() first:GameFrame(10) first:TextCommand("autopilot ai") frame(120)
		local saved = first:GetConfigData()
		local nextWidget, subs, created = fixture()
		nextWidget:SetConfigData(saved) nextWidget:Initialize() nextWidget:GameFrame(10)
		T.eq(#created, 1)
		T.eq(subs[1].ai, false, "previous game's AI setting must not leak")
	end,
}
