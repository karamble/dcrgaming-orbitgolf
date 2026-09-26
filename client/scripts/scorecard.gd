extends Control
signal close_requested

const MINT := Color("39f3cd")
const BLUE := Color("79a4ff")
const WHITE := Color("eaf4ff")
const MUTED := Color("92abc2")
const NAMES := ["TURQUOISE","COBALT"]
var sheet: PanelContainer
var lead: Label
var totals: Label
var legend: Label
var close_button: Button
var rows: Array = []

func text_cell(text: String, width: float, color: Color, parent: Node, font_size := 16) -> Label:
	var cell := Label.new()
	cell.text=text
	cell.custom_minimum_size=Vector2(width,34)
	cell.add_theme_font_size_override("font_size",font_size)
	cell.add_theme_color_override("font_color",color)
	parent.add_child(cell)
	return cell

func _ready() -> void:
	set_anchors_and_offsets_preset(Control.PRESET_FULL_RECT)
	mouse_filter=Control.MOUSE_FILTER_STOP
	var shade := ColorRect.new()
	shade.color=Color(0.01,0.025,0.05,0.80)
	shade.set_anchors_and_offsets_preset(Control.PRESET_FULL_RECT)
	shade.mouse_filter=Control.MOUSE_FILTER_STOP
	add_child(shade)
	sheet=PanelContainer.new()
	add_child(sheet)
	sheet.set_anchors_and_offsets_preset(Control.PRESET_CENTER)
	sheet.offset_left=-470
	sheet.offset_right=470
	sheet.offset_top=-320
	sheet.offset_bottom=320
	var style := StyleBoxFlat.new()
	style.bg_color=Color("091727")
	style.border_color=Color("34566f")
	style.set_border_width_all(1)
	style.set_corner_radius_all(12)
	style.content_margin_left=24
	style.content_margin_right=24
	style.content_margin_top=20
	style.content_margin_bottom=20
	sheet.add_theme_stylebox_override("panel",style)
	var layout := VBoxContainer.new()
	layout.add_theme_constant_override("separation",8)
	sheet.add_child(layout)
	var header := HBoxContainer.new()
	layout.add_child(header)
	var title := text_cell("NINE-HOLE SCORECARD",0,WHITE,header,26)
	title.size_flags_horizontal=Control.SIZE_EXPAND_FILL
	close_button=Button.new()
	close_button.text="Close · Tab / Esc"
	close_button.custom_minimum_size=Vector2(170,40)
	close_button.pressed.connect(func(): close_requested.emit())
	header.add_child(close_button)
	lead=text_cell("",0,MINT,layout,19)
	layout.add_child(HSeparator.new())
	var grid := GridContainer.new()
	grid.columns=6
	grid.add_theme_constant_override("h_separation",12)
	grid.add_theme_constant_override("v_separation",3)
	layout.add_child(grid)
	var widths := [42,220,48,110,110,166]
	var headings := ["HOLE","COURSE","PAR","TURQUOISE","COBALT","RESULT"]
	for col in range(6): text_cell(headings[col],widths[col],MINT if col==3 else (BLUE if col==4 else MUTED),grid,13)
	for hole in range(9):
		var cells: Array[Label]=[]
		for col in range(6): cells.append(text_cell("",widths[col],WHITE,grid))
		rows.append(cells)
	layout.add_child(HSeparator.new())
	totals=text_cell("",0,WHITE,layout)
	legend=text_cell("",0,MUTED,layout,13)
	hide()

func update_card(courses: Array, state: Dictionary) -> void:
	if courses.size()!=9 or not state.has("view"): return
	var view: Dictionary=state.view
	var practice: bool=state.mode=="practice"
	var results: Array=view.Results if view.get("Results") is Array else []
	var completed_par := 0
	var strokes := [0,0]
	for hole in range(9):
		var cells: Array=rows[hole]
		var active: bool=hole==int(view.Hole) and (practice or hole>=results.size())
		var color := WHITE if active or (not practice and hole<results.size()) else MUTED
		for cell in cells: cell.add_theme_color_override("font_color",color)
		cells[0].text="%02d%s" % [hole+1," ›" if active else ""]
		cells[1].text=courses[hole].Name
		cells[1].tooltip_text=courses[hole].World
		cells[2].text=str(int(courses[hole].Par))
		cells[3].text="—"
		cells[4].text="—"
		cells[5].text="Not played" if practice or view.Done else "Upcoming"
		if not practice and hole<results.size():
			var result: Dictionary=results[hole]
			completed_par+=int(courses[hole].Par)
			for seat in range(2):
				strokes[seat]+=int(result.Strokes[seat])
				cells[3+seat].text=str(int(result.Strokes[seat]))+("*" if int(result.Strokes[seat])==13 else "")
			cells[5].text=NAMES[int(result.Winner)] if result.Decided else "TIED"
			if result.Decided:
				var winner_color := MINT if int(result.Winner)==0 else BLUE
				cells[3+int(result.Winner)].add_theme_color_override("font_color",winner_color)
				cells[5].add_theme_color_override("font_color",winner_color)
		elif active:
			for seat in range(1 if practice else 2):
				var ball: Dictionary=view.Grid.Balls[seat]
				var capped: bool=not practice and not ball.Holed and int(ball.Strokes)>=12
				cells[3+seat].text="13*" if capped else str(int(ball.Strokes))+("" if ball.Holed else "…")
			cells[5].text="HOLED" if practice and view.Done else "PLAYING"
			cells[0].add_theme_color_override("font_color",MINT)
			cells[5].add_theme_color_override("font_color",MINT)
	if practice:
		lead.text="PRACTICE · HOLE %02d" % [int(view.Hole)+1]
		totals.text="Selected hole only · practice attempts are independent."
		legend.text="… strokes so far; ball still in play."
	else:
		var gap := int(view.Score[0])-int(view.Score[1])
		var scoreline := "%d : %d" % [view.Score[0],view.Score[1]]
		if view.Done:
			lead.text=(NAMES[int(view.Winner)]+" WINS" if view.Won else "MATCH DRAWN")+" · "+scoreline
		elif gap==0: lead.text="ALL SQUARE · "+scoreline
		else: lead.text="%s LEADS BY %d %s · %s" % [NAMES[0 if gap>0 else 1],absi(gap),"HOLE" if absi(gap)==1 else "HOLES",scoreline]
		totals.text="COMPLETED %d / 9  ·  STROKES %d : %d  ·  PAR %d" % [results.size(),strokes[0],strokes[1],completed_par]
		legend.text="… strokes so far   ·   13* stroke cap   ·   Hole wins decide the match; ties award no point."
