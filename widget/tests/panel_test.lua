-- Exercise the real panel drawing and input code with a small fake game surface.
local function panelFixture(width, height, count)
	local file = assert(io.open("widget/LuaUI/Widgets/gui_subcommanders.lua"))
	local src = file:read("*a")
	file:close()
	local function part(a, b)
		local first = assert(src:find(a, 1, true))
		local last = assert(src:find(b, first, true))
		return src:sub(first, last - 1)
	end
	local code = part("local function uiScale()", "-- World drawing")
		.. part("function widget:DrawScreen()", "-- Input")
		.. part("local function overPanel", "local function worldPos")
		.. part("function widget:MousePress", "function widget:MouseMove")
	local mouseX, mouseY = 0, 0
	local drawn, clips, activeClip = {}, {}, nil
	local font = {
		Begin = function() end, End = function() end, SetTextColor = function() end,
		GetTextWidth = function(_, text) return #text * 0.5 end,
		Print = function(_, text, x, y) drawn[#drawn + 1] = { text = text, x = x, y = y, visible = not activeClip or (y >= activeClip[2] and y <= activeClip[2] + activeClip[4]) } end,
	}
	local subs = {}
	for i = 1, count do
		subs[i] = { name = "Economy " .. i, role = "economy", posture = "balanced", autonomy = "full", color = { 1, 0.7, 0.2 },
			counts = { builders = 1, factories = 1, combat = 0, nanos = 0 }, spendM = 1, threats = {}, status = "building", ai = true, log = {}, units = {} }
	end
	local env = setmetatable({
		widget = {}, panel = { scroll = 0, scrollMax = 0 }, buttons = {}, font = font, subs = subs, activeSub = subs[1],
		ROLE_INFO = { economy = { label = "Economy", short = "ECO" } }, POSTURE_INFO = { balanced = { label = "Balanced" } },
		Engine = { D = { autonomy = { labels = { full = "Full" } } }, ourStrategic = function() return { nukeSilo = {}, junoSilo = {} } end },
		canControl = true, WG = {},
		Spring = { GetViewGeometry = function() return width, height end, GetConfigFloat = function(_, default) return default end,
			GetMouseState = function() return mouseX, mouseY end },
		gl = { Scissor = function(...)
			local args = { ... }
			clips[#clips + 1] = args
			activeClip = args[1] ~= false and args or nil
		end },
		glColor = function() end, glRect = function() end, drawZoneLabels = function() end,
		max = math.max, min = math.min, floor = math.floor, sformat = string.format,
	}, { __index = _G })
	assert(load(code, "panel fixture", "t", env))()
	return env, drawn, clips, function(x, y) mouseX, mouseY = x, y end
end

return {
	many_subcommanders_are_scrollable_and_header_stays_clickable = function()
		local env, drawn, clips, mouse = panelFixture(1920, 720, 12)
		env.widget:DrawScreen()
		T.ok(env.panel.scrollMax > 0, "content must scroll")
		T.ok(env.panel.bottom >= 0, "panel must fit vertically")
		T.eq(clips[1][2] >= 0, true, "clip must fit vertically")
		mouse(env.panel.x + 20, env.panel.y - 35)
		T.ok(env.widget:MouseWheel(false, 100))
		env.widget:DrawScreen()
		T.eq(env.panel.scroll, env.panel.scrollMax)
		local found = false
		for _, item in ipairs(drawn) do if item.text == "Economy 12" and item.visible then found = true end end
		T.ok(found, "last sub-commander was drawn")
		local header = env.buttons[2]
		T.ok(env.widget:GetTooltip((header.x1 + header.x2) / 2, (header.y1 + header.y2) / 2):find("Show or hide", 1, true), "header tooltip")
		T.ok(env.widget:MousePress((header.x1 + header.x2) / 2, (header.y1 + header.y2) / 2, 1))
		T.eq(env.panel.collapsed, true, "header remains clickable after scrolling")
	end,
	controls_explain_orders_and_ai_state = function()
		local env, drawn = panelFixture(1920, 1080, 1)
		env.widget:DrawScreen()
		local ai, orders, status
		for _, button in ipairs(env.buttons) do
			if button.label == "AI: on" then ai = button end
			if button.label == "Give orders…" then orders = button end
		end
		for _, item in ipairs(drawn) do
			if item.text:find("Strategist:", 1, true) then status = item.text end
		end
		T.ok(ai and orders and status, "AI status and order controls should be visible")
		T.ok(env.widget:GetTooltip((ai.x1 + ai.x2) / 2, (ai.y1 + ai.y2) / 2):find("strategist", 1, true))
		T.ok(env.widget:GetTooltip((orders.x1 + orders.x2) / 2, (orders.y1 + orders.y2) / 2):find("instructions", 1, true))
	end,
	panel_width_fits_a_narrow_screen = function()
		local env = panelFixture(200, 720, 1)
		env.widget:DrawScreen()
		for _, button in ipairs(env.buttons) do
			T.ok(button.x1 >= 0 and button.x2 <= 200, "button outside screen")
		end
	end,
}
