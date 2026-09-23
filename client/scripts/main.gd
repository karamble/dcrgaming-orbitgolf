extends Node3D

const Backend = preload("res://scripts/backend.gd")
const World = preload("res://scripts/world.gd")
const Trajectory = preload("res://scripts/trajectory.gd")
const MINT := Color("39f3cd")
const INK := Color("071222")
var backend: Node
var world: Node3D
var camera: Camera3D
var canvas: CanvasLayer
var ui: Control
var lobby: PanelContainer
var hud: PanelContainer
var settings: PanelContainer
var headline: Label
var detail: Label
var score: Label
var notice: Label
var power: ProgressBar
var power_label: Label
const CHARGE_SECONDS := 1.8
const AIM_SPEED := 1024.0 # 90 degrees per second; Shift reduces this sixfold.
var held_aim: Dictionary = {}
var aim_fraction := 0.0
var charging := false
var charge_elapsed := 0.0
var charge_source := ""
var charge_context := ""
var fire: Button
var selector: OptionButton
var table_select: OptionButton
var credential_fields: Array[Control] = []
var network: OptionButton
var courses: Array = []
var state: Dictionary = {}
var current_hole := ""
var balls: Array[MeshInstance3D] = []
var trajectory: Node3D
var route_label: Label
var preview_pending := -1
var preview_context := ""
var preview_clock := 0.0
var preview_last_key := ""
var preview_drawn_context := ""
var preview_drawn_yaw := -1
var capture_power := 0
var capture_yaw := 3072
var yaw := 3072
var camera_yaw := 0.48
var camera_pitch := 0.83
var distance := 34.0
var focus := Vector3.ZERO
var playing := false
var busy := false
var path: Array = []
var path_time := 0.0
var moving_seat := 0
var after_shot: Dictionary = {}
var last_path: Array = []
var poll_time := 0.0
var capture_path := ""
var capture_hole := 0
var capture_frames := 0
var reduced_motion := false
var sound_enabled := true
var sound: AudioStreamPlayer
var cover: TextureRect
var ready_to_play := false
var capture_menu := false
var fund_button: Button
var bridge_terms: Label
var fund_confirm: ConfirmationDialog
var private_key := ""

func _ready() -> void:
	get_window().min_size=Vector2i(1000,700)
	var env := WorldEnvironment.new()
	var environment := Environment.new()
	environment.background_mode=Environment.BG_COLOR
	environment.background_color=Color("030919")
	environment.ambient_light_source=Environment.AMBIENT_SOURCE_COLOR
	environment.ambient_light_color=Color("8bb6d0")
	environment.ambient_light_energy=0.32
	environment.tonemap_mode=Environment.TONE_MAPPER_FILMIC
	env.environment=environment
	add_child(env)
	var sun := DirectionalLight3D.new()
	sun.rotation_degrees=Vector3(-48,-38,0)
	sun.light_color=Color("ffe7c4")
	sun.light_energy=1.35
	sun.shadow_enabled=true
	sun.directional_shadow_max_distance=85
	sun.shadow_bias=0.035
	add_child(sun)
	var rim := DirectionalLight3D.new()
	rim.rotation_degrees=Vector3(-28,145,0)
	rim.light_color=Color("79b8ff")
	rim.light_energy=0.55
	add_child(rim)
	world=World.new()
	add_child(world)
	trajectory=Trajectory.new()
	add_child(trajectory)
	trajectory.hide()
	camera=Camera3D.new()
	camera.fov=48
	camera.far=400
	add_child(camera)
	var prefs := ConfigFile.new()
	if prefs.load("user://preferences.cfg")==OK:
		reduced_motion=prefs.get_value("ui","reduced_motion",false)
		sound_enabled=prefs.get_value("ui","sound",true)
	world.motion_enabled=not reduced_motion
	build_ui()
	sound=AudioStreamPlayer.new()
	add_child(sound)
	for arg in OS.get_cmdline_user_args():
		if arg.begins_with("--capture="): capture_path=arg.trim_prefix("--capture=")
		if arg.begins_with("--hole="): capture_hole=int(arg.trim_prefix("--hole="))
		if arg=="--menu": capture_menu=true
		if arg.begins_with("--power="): capture_power=clampi(int(arg.trim_prefix("--power=")),1,1000)
		if arg.begins_with("--yaw="): capture_yaw=posmod(int(arg.trim_prefix("--yaw=")),4096)
	backend=Backend.new()
	backend.reply.connect(on_reply)
	add_child(backend)
	backend.send("hello")
	update_camera()

func panel(pos: Vector2, size_: Vector2, parent: Control) -> PanelContainer:
	var p := PanelContainer.new()
	p.position=pos
	p.custom_minimum_size=size_
	var style := StyleBoxFlat.new()
	style.bg_color=Color(0.025,0.06,0.105,0.96)
	style.border_color=Color("26455b")
	style.set_border_width_all(1)
	style.set_corner_radius_all(12)
	style.content_margin_left=24
	style.content_margin_right=24
	style.content_margin_top=22
	style.content_margin_bottom=22
	p.add_theme_stylebox_override("panel",style)
	parent.add_child(p)
	return p

func label(text: String, size_: int, color: Color, parent: Node) -> Label:
	var l := Label.new()
	l.text=text
	l.add_theme_font_size_override("font_size",size_)
	l.add_theme_color_override("font_color",color)
	parent.add_child(l)
	return l

func button(text: String, action: Callable, parent: Node, primary := false) -> Button:
	var b := Button.new()
	b.text=text
	b.custom_minimum_size=Vector2(0,44)
	b.add_theme_font_size_override("font_size",16)
	var style := StyleBoxFlat.new()
	style.bg_color=MINT if primary else Color("172e46")
	style.set_corner_radius_all(6)
	style.content_margin_left=16
	style.content_margin_right=16
	b.add_theme_stylebox_override("normal",style)
	var hover: StyleBoxFlat=style.duplicate()
	hover.bg_color=style.bg_color.lightened(0.16)
	b.add_theme_stylebox_override("hover",hover)
	b.add_theme_color_override("font_color",INK if primary else Color("e7f2ff"))
	b.pressed.connect(action)
	parent.add_child(b)
	return b

func brand(parent: Node, width: float) -> void:
	if ResourceLoader.exists("res://assets/orbit-logo-v1.png"):
		var texture := TextureRect.new()
		texture.texture=load("res://assets/orbit-logo-v1.png")
		texture.expand_mode=TextureRect.EXPAND_IGNORE_SIZE
		texture.stretch_mode=TextureRect.STRETCH_KEEP_ASPECT_CENTERED
		texture.custom_minimum_size=Vector2(width,width/3)
		parent.add_child(texture)
	else:
		label("ORBIT GOLF",38,MINT,parent)

func build_ui() -> void:
	canvas=CanvasLayer.new()
	add_child(canvas)
	ui=Control.new()
	ui.set_anchors_and_offsets_preset(Control.PRESET_FULL_RECT)
	ui.mouse_filter=Control.MOUSE_FILTER_IGNORE
	canvas.add_child(ui)
	cover=TextureRect.new()
	cover.texture=load("res://assets/orbit-cover-v1.png")
	cover.expand_mode=TextureRect.EXPAND_IGNORE_SIZE
	cover.stretch_mode=TextureRect.STRETCH_KEEP_ASPECT_COVERED
	cover.set_anchors_and_offsets_preset(Control.PRESET_FULL_RECT)
	cover.mouse_filter=Control.MOUSE_FILTER_IGNORE
	ui.add_child(cover)
	lobby=panel(Vector2(40,40),Vector2(425,0),ui)
	var menu := VBoxContainer.new()
	menu.add_theme_constant_override("separation",14)
	lobby.add_child(menu)
	brand(menu,375)
	label("A SMALL SPORT.\nAN ASTRONOMICAL ARENA.",20,Color("eaf4ff"),menu)
	label("9 HOLES   /   3 WORLDS   /   2 PLAYERS",12,MINT,menu)
	selector=OptionButton.new()
	selector.custom_minimum_size.y=40
	menu.add_child(selector)
	button("Explore selected hole  →",start_practice,menu,true)
	button("Local two-player match",func(): playing=true;busy=true;backend.send("local_match"),menu)
	button("Connect dcrpulse bridge",func(): busy=true;backend.send("connect"),menu)
	button("Bridge settings",func(): settings.show();backend.send("config"),menu)
	label("DEVELOPMENT BUILD · MAINNET DISABLED",11,Color("ffbe69"),menu)
	var helper := label("Practice needs no wallet. Bridge payouts require\nboth players to sign; recovery stays in dcrpulse.",12,Color("92abc2"),menu)
	helper.autowrap_mode=TextServer.AUTOWRAP_WORD_SMART
	hud=panel(Vector2(24,24),Vector2(325,0),ui)
	var controls := VBoxContainer.new()
	controls.add_theme_constant_override("separation",10)
	hud.add_child(controls)
	brand(controls,277)
	headline=label("SHIPYARD",22,Color("eaf4ff"),controls)
	detail=label("",13,MINT,controls)
	score=label("",16,Color("eaf4ff"),controls)
	power_label=label("SHOT POWER · 0%",11,Color("91a9c0"),controls)
	power=ProgressBar.new()
	power.min_value=0
	power.max_value=1000
	power.value=0
	power.show_percentage=false
	power.custom_minimum_size=Vector2(270,30)
	var track := StyleBoxFlat.new()
	track.bg_color=Color("172e46")
	track.set_corner_radius_all(5)
	power.add_theme_stylebox_override("background",track)
	var fill := StyleBoxFlat.new()
	fill.bg_color=MINT
	fill.set_corner_radius_all(5)
	power.add_theme_stylebox_override("fill",fill)
	controls.add_child(power)
	route_label=label("Aim guide · 35% power",12,Color("91b6bb"),controls)
	fire=button("HOLD SPACE TO CHARGE",func(): pass,controls,true)
	fire.focus_mode=Control.FOCUS_NONE
	fire.button_down.connect(func(): begin_charge("mouse"))
	fire.button_up.connect(func():
		if charge_source=="mouse": release_charge())
	button("Replay last shot",replay,controls)
	button("Reset camera  ·  C",func(): focus=Vector3.ZERO;distance=34;camera_yaw=0.48;camera_pitch=0.83,controls)
	button("Back to lobby  ·  ESC",show_lobby,controls)
	label("Hold arrows / A D: aim · Shift: fine\nHold Space or button, release to putt\nRight-drag: orbit · Wheel: zoom",12,Color("92abc2"),controls)
	hud.hide()
	var top := HBoxContainer.new()
	top.position=Vector2(510,24)
	ui.add_child(top)
	table_select=OptionButton.new()
	table_select.custom_minimum_size=Vector2(220,40)
	table_select.item_selected.connect(func(i): playing=true;backend.send("select_table",{"Table":table_select.get_item_text(i)}))
	top.add_child(table_select)
	button("Open table",func():
		if table_select.selected>=0:
			playing=true
			backend.send("select_table",{"Table":table_select.get_item_text(table_select.selected)}),top)
	fund_button=button("Fund stake",func():
		fund_confirm.dialog_text=bridge_terms.text+"\n\nRequest your stake payment? Approve it in dcrpulse."
		fund_confirm.popup_centered(Vector2i(600,230)),top)
	button("Settle",func(): backend.send("settle"),top)
	button("Receipt",func(): backend.send("receipt"),top)
	top.hide()
	top.name="BridgeControls"
	bridge_terms=label("",12,Color("f5d0a0"),ui)
	bridge_terms.position=Vector2(510,78)
	bridge_terms.size=Vector2(850,80)
	bridge_terms.autowrap_mode=TextServer.AUTOWRAP_WORD_SMART
	fund_confirm=ConfirmationDialog.new()
	fund_confirm.title="Confirm stake request"
	fund_confirm.confirmed.connect(func(): backend.send("fund"))
	ui.add_child(fund_confirm)
	notice=label("Preparing local simulation…",15,Color("d9edff"),ui)
	notice.position=Vector2(40,820)
	notice.size=Vector2(1280,60)
	notice.autowrap_mode=TextServer.AUTOWRAP_WORD_SMART
	settings=panel(Vector2(470,100),Vector2(720,0),ui)
	var form := VBoxContainer.new()
	form.add_theme_constant_override("separation",8)
	settings.add_child(form)
	var settings_header := HBoxContainer.new()
	form.add_child(settings_header)
	var settings_title := label("Connect your gaming bridge",25,MINT,settings_header)
	settings_title.size_flags_horizontal=Control.SIZE_EXPAND_FILL
	var settings_close := button("×",func(): settings.hide(),settings_header)
	settings_close.name="CloseSettings"
	settings_close.custom_minimum_size=Vector2(44,44)
	settings_close.add_theme_font_size_override("font_size",26)
	settings_close.tooltip_text="Close bridge settings (Esc)"
	label("Register game ID dcrminigolf in dcrpulse. No wallet keys belong here.",13,Color.WHITE,form)
	network=OptionButton.new()
	network.add_item("simnet")
	network.add_item("testnet3")
	form.add_child(network)
	for title in ["Address","Port","Client certificate PEM","Client private key PEM","Bridge certificate PEM"]:
		label(title,12,Color("92abc2"),form)
		var field: Control
		if credential_fields.size()<2:
			field=LineEdit.new()
			field.text="127.0.0.1" if credential_fields.is_empty() else "8443"
		elif credential_fields.size()==3:
			field=LineEdit.new()
			field.secret=true
			field.editable=false
			field.placeholder_text="Use Paste key to preserve PEM line breaks"
		else:
			field=TextEdit.new()
			field.custom_minimum_size.y=65
		form.add_child(field)
		credential_fields.append(field)
		if credential_fields.size()==4:
			button("Paste key from clipboard (hidden)",func():
				private_key=DisplayServer.clipboard_get()
				credential_fields[3].text="Key pasted" if not private_key.is_empty() else "",form)
	var row := HBoxContainer.new()
	form.add_child(row)
	button("Save credentials",save_config,row,true)
	button("Disconnect",func(): backend.send("disconnect"),row)
	button("Close",func(): settings.hide(),row)
	var reduced := CheckButton.new()
	reduced.text="Reduced motion (camera, space and guide)"
	reduced.button_pressed=reduced_motion
	reduced.toggled.connect(func(v):
		reduced_motion=v
		world.motion_enabled=not v
		trajectory.material.set_shader_parameter("motion",0.0 if v else 1.0)
		save_prefs())
	form.add_child(reduced)
	var audio_toggle := CheckButton.new()
	audio_toggle.text="Sound"
	audio_toggle.button_pressed=sound_enabled
	audio_toggle.toggled.connect(func(v): sound_enabled=v;save_prefs())
	form.add_child(audio_toggle)
	settings.hide()

func save_prefs() -> void:
	var config := ConfigFile.new()
	config.set_value("ui","reduced_motion",reduced_motion)
	config.set_value("ui","sound",sound_enabled)
	config.save("user://preferences.cfg")

func save_config() -> void:
	var config := {"network":network.get_item_text(network.selected),"host":credential_fields[0].text,"port":credential_fields[1].text,"client_certificate":credential_fields[2].text,"client_private_key":private_key,"bridge_certificate":credential_fields[4].text}
	backend.send("save_config",{"Config":config})

func start_practice() -> void:
	if not ready_to_play: return
	cancel_charge()
	playing=true
	busy=true
	backend.send("practice",{"hole":selector.selected})

func show_lobby() -> void:
	if not path.is_empty() or busy: return
	cancel_charge()
	held_aim.clear()
	playing=false
	cover.show()
	lobby.show()
	hud.hide()
	backend.send("lobby")

func on_reply(message: Dictionary) -> void:
	if message.get("method","")=="preview":
		if int(message.get("id",-1))!=preview_pending: return
		preview_pending=-1
		if not message.ok:
			preview_last_key=""
			return
		if not can_shoot() or shot_context()!=preview_context: return
		var result: Dictionary=message.data
		# A response may arrive while aim/power is changing. Discard a route
		# for another direction, but allow a small charging update latency.
		if absi(wrapi(int(result.yaw)-yaw,-2048,2048))>96 or absi(int(result.power)-preview_power())>90: return
		trajectory.draw(result.preview,reduced_motion)
		preview_drawn_context=preview_context
		preview_drawn_yaw=int(result.yaw)
		trajectory.show()
		var outcome := "OUT OF BOUNDS" if result.preview.Penalty else ("IN THE CUP" if result.preview.Ball.Holed else "%.1f m route" % trajectory.distance_metres)
		route_label.text="%d%% · %s" % [int(round(float(result.power)/10.0)),outcome]
		return
	if message.get("method","")!="poll": busy=false
	if not message.ok:
		notice.text=message.get("error","Backend error")
		return
	var data = message.data
	if data == null:
		if message.get("method","")=="save_config":
			private_key=""
			for i in range(2,5): credential_fields[i].text=""
		notice.text="Saved. Connect when ready."
		return
	if data is String:
		notice.text="Receipt saved: "+data
		return
	if data.has("courses"):
		ready_to_play=true
		courses=data.courses
		for h in courses:
			selector.add_item("%02d  %s · %s" % [selector.item_count+1,h.World,h.Name])
		world.build(courses[0])
		notice.text="A new frontier. Pick your course."
		if not capture_path.is_empty() and not capture_menu:
			selector.select(capture_hole)
			start_practice()
	elif data.has("result"):
		path=data.result.Path
		last_path=path.duplicate(true)
		path_time=0
		moving_seat=int(state.get("view",{}).get("Turn",0))
		after_shot=data.state
		notice.text="Out of bounds · one penalty stroke" if data.result.Penalty else ("IN THE CUP!" if data.result.Ball.Holed else "Shot verified locally")
		play_sound(740 if data.result.Ball.Holed else 240)
	elif data.has("mode"):
		if path.is_empty(): apply_state(data)
	elif data.has("host"):
		credential_fields[0].text=data.host
		credential_fields[1].text=data.port
		network.select(1 if data.network=="testnet3" else 0)
		notice.text="Credentials saved" if data.configured else "Paste the three credentials issued by dcrpulse."

func apply_state(data: Dictionary) -> void:
	state=data
	if charging and shot_context()!=charge_context: cancel_charge()
	ui.get_node("BridgeControls").visible=data.get("connected",false)
	bridge_terms.visible=data.get("connected",false)
	fund_button.disabled=not data.get("can_fund",false) or data.get("fund_pending",false)
	if data.has("seating"):
		var terms: Dictionary=data.seating.Terms
		bridge_terms.text="Stake %s DCR · Bond %s DCR · Refund %d blocks\n%s" % [dcr(int(terms.BuyInAtoms)),dcr(int(terms.BondAtoms)),terms.RefundBlocks,data.seating.NextDetail]
	var tables: Array=data.get("tables",[])
	if table_select.item_count!=tables.size():
		table_select.clear()
		for sid in tables: table_select.add_item(sid)
	if not data.has("course"): return
	var hole: Dictionary=data.course
	if current_hole!=hole.ID:
		last_path.clear()
		current_hole=hole.ID
		world.build(hole)
		balls.clear()
		for color in [MINT,Color("568aff")]:
			balls.append(world.golf_ball(color))
		focus=Vector3.ZERO
		yaw=3072
		if capture_power>0 and not capture_path.is_empty():
			yaw=capture_yaw
			power.value=capture_power
	var view: Dictionary=data.view
	for i in range(2):
		balls[i].position=world.coord(view.Grid.Balls[i].Pos)
		balls[i].visible=not view.Grid.Balls[i].Holed and (i==view.Turn or data.mode!="practice")
		balls[i].scale=Vector3.ONE if i==view.Turn else Vector3.ONE*0.8
	headline.text=hole.Name
	detail.text="%s  /  HOLE %02d  /  PAR %d" % [hole.World,int(view.Hole)+1,hole.Par]
	score.text="%s  ·  STROKE %d\nHOLES WON   %d : %d" % ["TURQUOISE" if view.Turn==0 else "COBALT",int(view.Grid.Balls[view.Turn].Strokes)+1,view.Score[0],view.Score[1]]
	if view.Done:
		score.text="HOLE COMPLETE" if data.mode=="practice" else ("MATCH WON · "+("TURQUOISE" if view.Winner==0 else "COBALT") if view.Won else "MATCH DRAWN")
		notice.text="Explore another hole from the lobby." if data.mode=="practice" else "Final score: %d : %d" % [view.Score[0],view.Score[1]]
	elif data.mode=="bridge":
		notice.text=data.get("status","")+" · "+data.get("fund_reason","")
	else:
		notice.text=hole.Hint+"   ·   LOCAL / NO FUNDS"
	lobby.visible=not playing
	hud.visible=playing
	cover.visible=not playing

func dcr(atoms: int) -> String:
	return "%d.%08d" % [atoms/100000000,atoms%100000000]

func can_shoot() -> bool:
	if busy or not path.is_empty() or not playing or not state.has("view"): return false
	if settings.visible or fund_confirm.visible or state.view.Done: return false
	return state.mode!="bridge" or (state.get("can_play",false) and state.view.Seat==state.view.Turn)

func shot_context() -> String:
	if not state.has("view"): return ""
	return JSON.stringify([state.mode,state.get("table",""),state.view.Hole,state.view.Turn,state.view.Grid.Balls])

func begin_charge(source: String = "keyboard") -> void:
	if charging or not can_shoot(): return
	charging=true
	charge_source=source
	charge_context=shot_context()
	charge_elapsed=0.0
	power.value=1

func cancel_charge() -> void:
	charging=false
	charge_source=""
	charge_elapsed=0.0
	if is_instance_valid(power): power.value=0

func release_charge() -> void:
	if not charging: return
	if not can_shoot() or shot_context()!=charge_context:
		cancel_charge()
		return
	charging=false
	charge_source=""
	shoot()

func update_controls(delta: float) -> void:
	if not can_shoot():
		if charging: cancel_charge()
		held_aim.clear()
		aim_fraction=0.0
		return
	var direction := int(held_aim.has(KEY_RIGHT) or held_aim.has(KEY_DOWN) or held_aim.has(KEY_D))-int(held_aim.has(KEY_LEFT) or held_aim.has(KEY_UP) or held_aim.has(KEY_A))
	if direction!=0:
		aim_fraction+=direction*AIM_SPEED*delta/(6.0 if Input.is_key_pressed(KEY_SHIFT) else 1.0)
		var step := int(aim_fraction)
		yaw=posmod(yaw+step,4096)
		aim_fraction-=step
	else:
		aim_fraction=0.0
	if charging:
		charge_elapsed=minf(CHARGE_SECONDS,charge_elapsed+delta)
		power.value=maxf(1.0,round(charge_elapsed/CHARGE_SECONDS*1000.0))

func shoot() -> void:
	if not can_shoot(): return
	var shot_power := clampi(int(power.value),1,1000)
	trajectory.hide()
	busy=true
	backend.send("shot",{"Yaw":yaw,"Power":shot_power})
	cancel_charge()
	preview_last_key=""
	preview_drawn_context=""
	preview_drawn_yaw=-1
	route_label.text="Shot in progress"

func replay() -> void:
	if busy or not path.is_empty() or last_path.is_empty(): return
	cancel_charge()
	path=last_path.duplicate(true)
	path_time=0
	after_shot=state

func play_sound(frequency: float) -> void:
	if not sound_enabled: return
	sound.stop()
	var stream := AudioStreamWAV.new()
	stream.format=AudioStreamWAV.FORMAT_16_BITS
	stream.mix_rate=22050
	var bytes := PackedByteArray()
	bytes.resize(4410*2)
	for n in range(4410):
		var sample := int(sin(float(n)*TAU*frequency/22050.0)*exp(-float(n)/600.0)*5500)
		bytes.encode_s16(n*2,sample)
	stream.data=bytes
	sound.stream=stream
	sound.play()

func _exit_tree() -> void:
	if is_instance_valid(sound):
		sound.stop()
		sound.stream=null

func _process(delta: float) -> void:
	# Noninteractive screenshot mode may lose desktop focus while another
	# window is open; keep its explicitly requested demonstration power.
	if not capture_path.is_empty() and capture_power>0 and playing:
		power.value=capture_power
	update_controls(delta)
	power_label.text="SHOT POWER · %d%%" % int(round(power.value/10.0))
	poll_time+=delta
	if poll_time>2 and backend!=null and state.get("connected",false) and not busy:
		poll_time=0
		backend.send("poll")
	if not path.is_empty() and not balls.is_empty():
		path_time+=delta*60
		var index := int(path_time)
		if index>=path.size()-1:
			path.clear()
			apply_state(after_shot)
		else:
			var pos: Vector3=world.coord(path[index]).lerp(world.coord(path[index+1]),path_time-index)
			balls[moving_seat].position=pos
			if not reduced_motion: focus=focus.lerp(Vector3(pos.x,0,pos.z)*0.3,delta*2)
	update_preview(delta)
	if playing and state.has("view"):
		fire.disabled=not can_shoot()
		fire.text="RELEASE TO PUTT · %d%%" % int(round(power.value/10.0)) if charging else "HOLD SPACE TO CHARGE"
	update_camera()
	if not capture_path.is_empty() and ready_to_play and (capture_menu or (playing and not state.is_empty())):
		capture_frames+=1
		if capture_frames>=40 and (capture_menu or trajectory.visible):
			# Capture only once while awaiting the draw.
			set_process(false)
			await RenderingServer.frame_post_draw
			get_viewport().get_texture().get_image().save_png(capture_path)
			get_tree().quit()

func preview_power() -> int:
	return clampi(int(power.value),1,1000) if power.value>0 else 350

func update_preview(delta: float) -> void:
	preview_clock+=delta
	if not can_shoot():
		trajectory.hide()
		preview_last_key=""
		return
	var context := shot_context()
	if context!=preview_drawn_context or absi(wrapi(yaw-preview_drawn_yaw,-2048,2048))>96: trajectory.hide()
	var wanted_power := preview_power()
	var key := context+"/%d/%d" % [yaw,wanted_power]
	if preview_pending>=0 or preview_clock<0.075 or key==preview_last_key: return
	preview_clock=0.0
	preview_context=context
	preview_last_key=key
	preview_pending=backend.send("preview",{"Yaw":yaw,"Power":wanted_power})

func update_camera() -> void:
	if camera==null: return
	camera.position=focus+Vector3(sin(camera_yaw)*cos(camera_pitch),sin(camera_pitch),cos(camera_yaw)*cos(camera_pitch))*distance
	camera.look_at(focus,Vector3.UP)

func _notification(what: int) -> void:
	if what==NOTIFICATION_APPLICATION_FOCUS_OUT:
		cancel_charge()
		held_aim.clear()

# Gameplay keys are consumed before focused buttons can treat Space/arrow keys
# as UI navigation. Text entry and modal dialogs retain their normal controls.
func _input(event: InputEvent) -> void:
	if settings.visible and event is InputEventKey and event.keycode==KEY_ESCAPE and event.pressed:
		settings.hide()
		get_viewport().set_input_as_handled()
		return
	if event is InputEventMouseButton and event.button_index==MOUSE_BUTTON_LEFT and not event.pressed and charging and charge_source=="mouse":
		release_charge()
		get_viewport().set_input_as_handled()
		return
	if not playing or settings.visible or fund_confirm.visible: return
	if event is InputEventKey:
		if event.keycode in [KEY_LEFT,KEY_RIGHT,KEY_UP,KEY_DOWN,KEY_A,KEY_D]:
			if event.pressed: held_aim[event.keycode]=true
			else: held_aim.erase(event.keycode)
			get_viewport().set_input_as_handled()
		elif event.keycode==KEY_SPACE:
			if not event.echo:
				if event.pressed: begin_charge()
				elif charge_source=="keyboard": release_charge()
			get_viewport().set_input_as_handled()

func _unhandled_input(event: InputEvent) -> void:
	if settings.visible or fund_confirm.visible: return
	if event is InputEventKey and event.pressed and not event.echo:
		match event.keycode:
			KEY_ESCAPE: show_lobby()
			KEY_C: focus=Vector3.ZERO;camera_yaw=0.48;camera_pitch=0.83;distance=34
			KEY_R: replay()
	if event is InputEventMouseMotion and event.button_mask&MOUSE_BUTTON_MASK_RIGHT:
		camera_yaw-=event.relative.x*0.005
		camera_pitch=clampf(camera_pitch+event.relative.y*0.005,0.25,1.48)
	if event is InputEventMouseButton and event.pressed:
		if event.button_index==MOUSE_BUTTON_WHEEL_UP: distance=maxf(16,distance-2)
		if event.button_index==MOUSE_BUTTON_WHEEL_DOWN: distance=minf(60,distance+2)
		if event.button_index==MOUSE_BUTTON_LEFT and playing and not balls.is_empty():
			var ball: Vector3=balls[int(state.view.Turn)].position
			var plane := Plane(Vector3.UP,ball.y)
			var hit = plane.intersects_ray(camera.project_ray_origin(event.position),camera.project_ray_normal(event.position))
			if hit!=null:
				yaw=int(round(atan2(hit.z-ball.z,hit.x-ball.x)*4096/TAU)+4096)%4096
