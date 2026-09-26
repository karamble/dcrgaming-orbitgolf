extends SceneTree

var game: Node
var failed := false

func _initialize() -> void:
	call_deferred("run")

func check(condition: bool, message: String) -> void:
	if not condition:
		failed=true
		push_error(message)

func wait_idle() -> void:
	for frame in range(600):
		if not game.busy: return
		await process_frame
	check(false,"Backend did not answer")

func key(code: int, pressed: bool, echo := false) -> void:
	var event := InputEventKey.new()
	event.keycode=code
	event.pressed=pressed
	event.echo=echo
	root.push_input(event)

func wait_preview() -> void:
	for frame in range(600):
		if game.preview_pending<0: return
		await process_frame
	check(false,"Trajectory preview did not answer")

func check_preview() -> void:
	game.set_process(false)
	await wait_preview()
	game.yaw=3072
	game.power.value=300
	var state_before := JSON.stringify(game.state)
	game.preview_last_key=""
	game.update_preview(0.1)
	var pending: int=game.preview_pending
	var sequence: int=game.backend.sequence
	game.update_preview(0.1)
	check(game.preview_pending==pending and game.backend.sequence==sequence,"Preview requests must be single-flight")
	await wait_preview()
	check(game.trajectory.visible and game.trajectory.ribbon.mesh!=null,"Trajectory mesh was not drawn")
	check(JSON.stringify(game.state)==state_before,"Preview changed frontend match state")
	var short_distance: float=game.trajectory.distance_metres
	game.power.value=600
	game.update_preview(0.1)
	await wait_preview()
	check(game.trajectory.distance_metres>short_distance,"Preview did not respond to power")
	# A late preview must never unlock an actual shot that's already in flight.
	game.power.value=650
	game.update_preview(0.1)
	game.busy=true
	await wait_preview()
	check(game.busy,"Preview response incorrectly cleared shot busy flag")
	game.busy=false
	game.preview_last_key=""
	game.update_preview(0.1)
	game.trajectory.hide()
	var saved_hole: int=game.state.view.Hole
	game.state.view.Hole=saved_hole+1
	await wait_preview()
	check(not game.trajectory.visible,"Stale preview from a different hole was drawn")
	game.state.view.Hole=saved_hole
	game.preview_last_key=""
	game.power.value=0
	game.set_process(true)

func check_space_motion() -> void:
	var scene: Node3D=game.world
	scene.set_process(false)
	var previous_motion: bool=scene.motion_enabled
	scene.motion_enabled=true
	scene.space_time=0.0
	var before: Transform3D=scene.animated[0].node.transform
	var state_before := JSON.stringify(game.state)
	scene._process(2.0)
	check(scene.animated[0].node.transform!=before,"Space objects did not animate")
	check(scene.comets.size()==3 and scene.comets[0].node.visible,"Comet pass did not start")
	check(scene.comets[0].node.position!=scene.comets[0].start,"Comet did not travel")
	check(scene.comets[0].material.get_shader_parameter("opacity")>0.0,"Comet trail did not fade in")
	check(JSON.stringify(game.state)==state_before,"Decorative animation changed match state")
	var frozen: Array[Transform3D]=[]
	for item in scene.animated: frozen.append(item.node.transform)
	var comet_position: Vector3=scene.comets[0].node.position
	scene.motion_enabled=false
	scene._process(10.0)
	for i in range(frozen.size()): check(scene.animated[i].node.transform==frozen[i],"Reduced motion did not freeze scenery")
	check(scene.comets[0].node.position==comet_position,"Reduced motion did not freeze comet")
	scene.motion_enabled=previous_motion
	scene.set_process(true)

func check_rail_geometry(hole: Dictionary) -> void:
	var walls: Array=hole.Walls if hole.Walls!=null else []
	check(game.world.wall_nodes.size()==walls.size(),"Rail count differs from collision walls")
	for i in range(walls.size()):
		var wall: Dictionary=walls[i]
		var shape: Dictionary=game.world.Rail.bounds(wall,hole)
		var size: Vector3=shape.size
		var center: Vector3=shape.center
		var node: MeshInstance3D=game.world.wall_nodes[i]
		var visual: AABB=node.transform*node.mesh.get_aabb()
		check(visual.size.is_equal_approx(size),"Rail mesh differs from its declared bounds")
		check(is_equal_approx(visual.position.y,float(wall.Y)/10000.0),"Rail base moved vertically")
		if wall.W==0:
			check(is_equal_approx(size.x,0.85),"Perimeter rail is not thick armor")
			check(is_equal_approx(center.x-signf(float(wall.X))*size.x/2.0,float(wall.X)/10000.0),"Thick rail intrudes into playable X boundary")
			var lo: float=float(wall.Z-wall.D/2)/10000.0
			var hi: float=float(wall.Z+wall.D/2)/10000.0
			if absf(lo)<12.0: check(is_equal_approx(center.z-size.z/2.0,lo),"Armor extended into open ramp end")
			if absf(hi)<12.0: check(is_equal_approx(center.z+size.z/2.0,hi),"Armor extended into open ramp end")
		elif wall.D==0:
			check(is_equal_approx(size.z,0.85),"End rail is not thick armor")
			check(is_equal_approx(center.z-signf(float(wall.Z))*size.z/2.0,float(wall.Z)/10000.0),"Thick rail intrudes into playable Z boundary")
		else:
			check(is_equal_approx(size.x,float(wall.W)/10000.0) and is_equal_approx(size.z,float(wall.D)/10000.0),"Interior obstacle footprint changed")

func check_controls() -> void:
	game.set_process(false)
	game.yaw=0
	key(KEY_RIGHT,true)
	for frame in range(60): game.update_controls(1.0/60.0)
	check(absi(game.yaw-1024)<=1,"Held arrow did not aim continuously")
	key(KEY_RIGHT,false)
	var angle: int=game.yaw
	game.update_controls(1.0)
	check(game.yaw==angle,"Aiming continued after release")
	key(KEY_LEFT,true)
	game.update_controls(1.0)
	key(KEY_LEFT,false)
	check(game.yaw==0 or game.yaw==4095,"Left arrow did not reverse the aim")
	key(KEY_DOWN,true)
	game.update_controls(0.5)
	key(KEY_DOWN,false)
	check(game.yaw>500 and game.yaw<520,"Down arrow did not turn aim")
	# Space must reach the game even if a UI button has focus.
	game.fire.focus_mode=Control.FOCUS_ALL
	game.fire.grab_focus()
	var before: int=game.backend.sequence
	key(KEY_SPACE,true)
	game.update_controls(0.45)
	check(game.charging and game.power.value==250,"Hold duration did not charge to 25 percent")
	key(KEY_SPACE,true,true)
	check(game.power.value==250,"OS key repeat reset the charge")
	check(not game.busy and game.backend.sequence==before,"Holding Space fired prematurely")
	key(KEY_SPACE,false)
	check(game.busy and game.backend.sequence==before+1,"Release did not fire exactly one shot")
	check(game.power.value==0 and game.preview_power()==350,"Shot must reset charge and restore neutral guide power")
	key(KEY_SPACE,false)
	check(game.backend.sequence==before+1,"Duplicate release fired twice")
	await wait_idle()
	check(not game.path.is_empty(),"Charged shot did not return a trajectory")
	game.path_time=100000
	game._process(0.0)
	game.fire.focus_mode=Control.FOCUS_NONE
	before=game.backend.sequence
	key(KEY_SPACE,true)
	game.update_controls(1.8)
	check(game.power.value==1000,"Charge did not reach full in 1.8 seconds")
	game.update_controls(0.99)
	check(game.power.value==1000,"Full-power pause ended before one second")
	game.update_controls(0.01)
	check(game.power.value==1000,"Full-power pause is not one second")
	game.update_controls(0.9)
	check(game.power.value==500,"Power did not fall to half strength")
	game.update_controls(0.9)
	check(game.power.value==1,"Power did not reach its minimum")
	game.update_controls(0.9)
	check(game.power.value==500,"Power did not rise again after the minimum")
	game.update_controls(0.9)
	game.update_controls(0.5)
	check(game.power.value==1000,"Second peak did not pause")
	game.update_controls(1.4)
	check(game.power.value==500,"Second falling sweep differs from the first")
	game.update_controls(game.CHARGE_CYCLE*10.0)
	check(game.power.value==500 and game.charging and game.backend.sequence==before,"Long held charge drifted or fired automatically")
	game._notification(Node.NOTIFICATION_APPLICATION_FOCUS_OUT)
	key(KEY_SPACE,false)
	check(not game.charging and game.power.value==0 and game.backend.sequence==before,"Focus loss must cancel without firing")
	game.settings.show()
	key(KEY_SPACE,true)
	key(KEY_SPACE,false)
	check(not game.charging and game.backend.sequence==before,"Settings captured a gameplay charge")
	var close_button: Button=game.settings.find_child("CloseSettings",true,false)
	check(close_button!=null,"Settings close X is missing")
	if close_button!=null: close_button.pressed.emit()
	check(not game.settings.visible and game.backend.sequence==before,"Settings X must close without saving or disconnecting")
	game.settings.show()
	key(KEY_ESCAPE,true)
	key(KEY_ESCAPE,false)
	check(not game.settings.visible and game.playing,"Escape must close settings without leaving the game")
	game.begin_charge("mouse")
	game.update_controls(0.9)
	check(game.power.value==500,"Mouse button charging differs from Space")
	game.update_controls(2.8)
	check(game.power.value==500,"Mouse charge did not follow the falling sweep")
	before=game.backend.sequence
	game.release_charge()
	check(game.busy and game.backend.sequence==before+1,"Release on the falling sweep failed to fire once")
	check(game.power.value==0 and not game.charging,"Falling-sweep release did not reset the charge")
	await wait_idle()
	game.path_time=100000
	game._process(0.0)
	game.set_process(true)

func check_camera_and_obstacles() -> void:
	game.set_process(false)
	var state_before := JSON.stringify(game.state)
	var original: Vector3=game.balls[0].position
	game.balls[0].position=Vector3(2,4,-95)
	game.reset_camera()
	check(game.focus.distance_to(game.camera_target())<0.01,"Camera reset did not find the ball on a long course")
	game.balls[0].position+=Vector3(0,2,-8)
	game.update_camera_focus(1.0)
	check(game.focus.distance_to(game.camera_target())<0.05,"Camera failed to follow elevation and progress")
	game.toggle_overview()
	check(game.overview and game.focus==game.overview_center and game.overview_distance>100,"Overview failed to frame long course")
	game.toggle_overview()
	var previous_yaw: float=game.camera_yaw
	var motion := InputEventMouseMotion.new()
	motion.button_mask=MOUSE_BUTTON_MASK_RIGHT
	motion.relative=Vector2(40,0)
	game._unhandled_input(motion)
	check(game.camera_yaw!=previous_yaw,"Manual camera orbit unavailable")
	var previous_reduced: bool=game.reduced_motion
	game.reduced_motion=true
	game.balls[0].position+=Vector3(0,0,-4)
	game.update_camera_focus(0.1)
	check(game.focus==game.camera_target(),"Reduced-motion camera lost the ball")
	game.reduced_motion=previous_reduced
	game.balls[0].position=original
	game.reset_camera()
	for roof in game.world.roofs:
		game.world.reveal_tunnels(game.world.coord(roof.wall),false)
		check(roof.material.albedo_color.a<0.2,"Tunnel roof obscures ball")
	for item in game.world.gates:
		var g: Dictionary=item.data
		var open_tick := posmod(24-int(g.Offset),int(g.Period))
		game.world.update_obstacles(open_tick)
		if g.Laser: check(not item.node.visible,"Laser failed to open")
		else: check(is_equal_approx(item.node.position.y,item.base.y+item.height*1.5+1.0),"Door lift disagrees with integer physics")
		game.world.update_obstacles(open_tick+int(g.Open))
		if g.Laser: check(item.node.visible,"Laser failed to close")
		else: check(is_equal_approx(item.node.position.y,item.base.y+item.height/2.0),"Door failed to close")
	check(JSON.stringify(game.state)==state_before,"Camera/obstacle display mutated match state")
	game.world.update_obstacles(game.phase_tick())
	game.set_process(true)

func check_practice_ghost() -> void:
	check(not game.ghost_result.is_empty() and game.ghost.visible,"Completed practice shot has no ghost")
	check(game.ghost.material.get_shader_parameter("motion")==0.0,"Practice ghost must be static")
	var before := JSON.stringify(game.state)
	var history := JSON.stringify(game.ghost_result)
	game.toggle_ghost()
	check(not game.ghost.visible,"Ghost toggle did not hide the route")
	game.toggle_ghost()
	check(game.ghost.visible and JSON.stringify(game.state)==before,"Ghost toggle altered match state")
	game.retry_practice()
	await wait_idle()
	check(game.state.view.Grid.Balls[0].Strokes==0,"Retry hole did not reset practice")
	check(game.focus.distance_to(game.camera_target())<0.01,"Retry hole left the camera at the previous endpoint")
	check(JSON.stringify(game.ghost_result)==history and game.ghost.visible,"Retry hole lost the comparison ghost")
	check(game.last_path.is_empty(),"Retry hole retained a replay from the old attempt")
	# A hazard trace must not draw the authoritative reset teleport as a flight.
	var penalty := {"Path":[{"X":0,"Y":1800,"Z":0},{"X":10000,"Y":-10000,"Z":0},{"X":0,"Y":1800,"Z":0}],"Penalty":true,"Ball":{"Holed":false}}
	game.ghost.draw(penalty,true,true)
	check(not game.ghost.target.visible and game.ghost.distance_metres<2.0,"Penalty ghost invented a landing point or return route")
	game.ghost.draw(game.ghost_result,true,true)

func capture_polish(name_: String) -> void:
	for arg in OS.get_cmdline_user_args():
		if arg.begins_with("--capture-polish="):
			await RenderingServer.frame_post_draw
			root.get_texture().get_image().save_png(arg.trim_prefix("--capture-polish=")+"/"+name_+".png")

func check_cup_finish() -> void:
	game.set_process(false)
	await wait_preview()
	game.selector.select(0)
	game.start_practice()
	await wait_idle()
	check(game.ghost_result.is_empty(),"A new practice session retained a ghost")
	var old_reduced: bool=game.reduced_motion
	game.reduced_motion=false
	# The same real IPC shots as the Go frozen safe route: first hole, birdie.
	for shot in [0x326398,0x2e2b34,0x2c7694,0x31fb0c]:
		game.yaw=(shot>>10)&4095
		game.power.value=shot&1023
		game.shoot()
		await wait_idle()
		check(not game.finish.active and not game.finish_banner.visible,"Cup celebration started before arrival")
		game.path_time=100000
		game._process(0.0)
		if not game.finish.active:
			game.update_camera_focus(1.0)
			game.update_camera()
			await wait_preview()
			await capture_polish("practice-ghost")
	check(game.finish.active and game.finish_banner.visible,"Holed shot did not celebrate on arrival")
	check(game.finish_title.text=="BIRDIE","Finish did not use the actual stroke count and par")
	check(not game.can_shoot(),"A shot was allowed during the finish")
	check(not game.balls[0].visible,"Holed ball remained above the cup")
	var pending := JSON.stringify(game.after_shot)
	game.finish.advance(0.9,false)
	game.update_camera_focus(1.0)
	game.update_camera()
	check(game.finish.push>0.9,"Normal finish did not push the camera")
	await capture_polish("cup-finish")
	game.reduced_motion=true
	game.finish.advance(0.0,true)
	game.update_camera()
	check(game.finish.push==0.0 and game.finish.lights[0].scale==Vector3.ONE,"Reduced motion animated the cup finish")
	check(game.finish_banner.visible,"Reduced motion hid the finish result")
	game._process(2.0)
	check(not game.finish.active and not game.finish_banner.visible,"Finish did not clear after its duration")
	check(game.state.view.Done and JSON.stringify(game.state)==pending,"Celebration changed or lost the authoritative result")
	game.replay()
	check(game.balls[0].visible and not game.path.is_empty(),"Holed-shot replay did not restore the ball")
	game.path_time=100000
	game._process(0.0)
	check(game.finish.active and game.finish.push==0.0,"Reduced-motion replay did not show a static finish")
	game._process(2.0)
	check(JSON.stringify(game.state)==pending,"Replaying a finish changed the score")
	# Distinct scoring and long-putt motifs; muted tests never play these streams.
	var result: Dictionary=game.last_result.duplicate(true)
	result.Path[0]=result.Ball.Pos.duplicate(true)
	var birdie: Dictionary=game.CupFinish.describe(result,5)
	var eagle: Dictionary=game.CupFinish.describe(result,6)
	check(eagle.title=="EAGLE","Eagle classification failed")
	check(game.CupFinish.sound_stream(birdie).data!=game.CupFinish.sound_stream(eagle).data,"Birdie and eagle use identical sounds")
	result.Path[0].X=result.Ball.Pos.X+150000
	result.Path[0].Z=result.Ball.Pos.Z
	var long_putt: Dictionary=game.CupFinish.describe(result,5)
	check(long_putt.long_putt and long_putt.detail.contains("15.0 m"),"Long putt did not use start-to-cup distance")
	check(game.CupFinish.sound_stream(birdie).data!=game.CupFinish.sound_stream(long_putt).data,"Long putt lacks a distinct sound")
	game.reduced_motion=old_reduced
	# Real local match: both seats finish, and the celebration must retain the
	# old cup/course until the final player's path and finish have completed.
	game.busy=true
	game.backend.send("local_match")
	await wait_idle()
	check(game.ghost_result.is_empty() and not game.ghost.visible,"Practice ghost leaked into a match")
	for shot in [0x326398,0x2e2b34,0x2c7694,0x31fb0c]:
		for seat in range(2):
			game.yaw=(shot>>10)&4095
			game.power.value=shot&1023
			game.shoot()
			await wait_idle()
			game.path_time=100000
			game._process(0.0)
			if game.finish.active:
				check(game.state.view.Hole==0 and game.finish.position.distance_to(game.world.coord(game.courses[0].Cup))<0.1,"Finish jumped to the next hole too soon")
				game._process(2.0)
	check(game.state.view.Hole==1 and not game.finish.active,"Local match failed to advance after cup finish")
	check(game.last_path.is_empty() and not game.ghost.visible,"Old shot visuals survived a course transition")
	game.set_process(true)

func check_scorecard() -> void:
	game.set_process(false)
	await wait_preview()
	var original: Dictionary=game.state.duplicate(true)
	var before := JSON.stringify(game.state)
	game.begin_charge()
	game.update_controls(0.5)
	var sequence: int=game.backend.sequence
	key(KEY_TAB,true)
	key(KEY_TAB,false)
	check(game.scorecard.visible and not game.charging and game.power.value==0,"Opening scorecard must cancel charge")
	key(KEY_SPACE,false)
	check(game.backend.sequence==sequence and not game.can_shoot(),"Scorecard allowed a shot or released a cancelled charge")
	check(game.scorecard.rows.size()==9,"Scorecard is missing holes")
	check(game.scorecard.rows[0][3].text=="4" and game.scorecard.rows[0][4].text=="4" and game.scorecard.rows[0][5].text=="TIED","Scorecard lost the actual completed hole")
	check(game.scorecard.rows[1][3].text=="0…" and game.scorecard.rows[1][5].text=="PLAYING","Current hole needs live stroke counts")
	check(game.scorecard.rows[8][5].text=="Upcoming","Future hole was treated as played")
	check(game.scorecard.lead.text.begins_with("ALL SQUARE"),"Tied match lead is wrong")
	check(JSON.stringify(game.state)==before,"Opening scorecard mutated match state")
	await capture_polish("scorecard-live")
	key(KEY_ESCAPE,true)
	key(KEY_ESCAPE,false)
	check(not game.scorecard.visible and game.playing,"Escape left the match instead of closing scorecard")
	check(game.can_shoot(),"Closing scorecard did not restore controls")
	key(KEY_TAB,true)
	key(KEY_TAB,false)
	game.scorecard.close_button.pressed.emit()
	check(not game.scorecard.visible,"Scorecard close button failed")
	var fixture: Dictionary=original.duplicate(true)
	fixture.view.Results=[{"Strokes":[4,5],"Winner":0,"Decided":true},{"Strokes":[13,6],"Winner":1,"Decided":true},{"Strokes":[5,5],"Winner":0,"Decided":false}]
	fixture.view.Score=[1,1]
	fixture.view.Hole=3
	fixture.course=game.courses[3]
	for mode in ["local","bridge"]:
		fixture.mode=mode
		game.scorecard.update_card(game.courses,fixture)
		check(game.scorecard.rows[0][5].text=="TURQUOISE" and game.scorecard.rows[1][5].text=="COBALT","Wrong hole winners in "+mode)
		check(game.scorecard.rows[1][3].text=="13*","Stroke-cap score was not marked")
		check(game.scorecard.totals.text.contains("STROKES 22 : 16"),"Totals include live or unplayed holes")
	fixture.view.Score=[2,1]
	game.scorecard.update_card(game.courses,fixture)
	check(game.scorecard.lead.text.contains("TURQUOISE LEADS BY 1 HOLE"),"Single-hole lead is wrong")
	fixture.view.Score=[1,3]
	game.scorecard.update_card(game.courses,fixture)
	check(game.scorecard.lead.text.contains("COBALT LEADS BY 2 HOLES"),"Multi-hole lead is wrong")
	# A finished, early-clinched match opens the card and leaves four holes unplayed.
	fixture.mode="local"
	fixture.view.Results=[]
	for hole in range(5): fixture.view.Results.append({"Strokes":[4,5],"Winner":0,"Decided":true})
	fixture.view.Hole=4
	fixture.course=game.courses[4]
	fixture.view.Done=true
	fixture.view.Won=true
	fixture.view.Winner=0
	fixture.view.Score=[5,0]
	game.apply_state(fixture)
	check(game.scorecard.visible and game.scorecard.lead.text=="TURQUOISE WINS · 5 : 0","Final scorecard did not open with the winner")
	check(game.scorecard.rows[4][3].text=="4" and game.scorecard.rows[5][5].text=="Not played","Early clinch fabricated extra scores")
	await capture_polish("scorecard-final")
	fixture.view.Results=[]
	for hole in range(9): fixture.view.Results.append({"Strokes":[13,13],"Winner":0,"Decided":false})
	fixture.view.Hole=8
	fixture.view.Won=false
	fixture.view.Score=[0,0]
	game.scorecard.update_card(game.courses,fixture)
	check(game.scorecard.lead.text=="MATCH DRAWN · 0 : 0" and game.scorecard.rows[8][5].text=="TIED","Final draw was rendered as a win or active hole")
	check(game.scorecard.totals.text.contains("STROKES 117 : 117"),"Nine-hole stroke totals are wrong")
	fixture.mode="practice"
	fixture.view.Hole=3
	fixture.view.Done=false
	fixture.view.Grid.Balls[0].Strokes=2
	game.scorecard.update_card(game.courses,fixture)
	check(game.scorecard.rows[3][3].text=="2…" and game.scorecard.rows[3][4].text=="—","Practice duplicated the player's score")
	check(game.scorecard.rows[0][3].text=="—" and game.scorecard.lead.text.begins_with("PRACTICE"),"Practice leaked match history")
	game.close_scorecard()
	game.apply_state(original)
	game.set_process(true)

func run() -> void:
	game=load("res://main.tscn").instantiate()
	root.add_child(game)
	# The headless dummy audio device does not consume queued playback.
	game.sound_enabled=false
	for frame in range(600):
		if game.ready_to_play: break
		await process_frame
	check(game.ready_to_play,"Backend hello missing")
	if not game.ready_to_play:
		quit(1)
		return
	for hole in range(9):
		game.selector.select(hole)
		game.start_practice()
		await wait_idle()
		check(game.state.view.Hole==hole,"Wrong hole")
		check(game.balls.size()==2,"Ball geometry missing")
		check_rail_geometry(game.state.course)
		check(game.world.animated.size()<80,"Animated nodes accumulated between courses")
		check_space_motion()
		check_camera_and_obstacles()
		if hole==0:
			await check_preview()
			await check_controls()
			await check_practice_ghost()
		game.power.value=1000 if hole==1 else 550
		game.shoot()
		check(game.power.value==0 and not game.charging,"Shot retained its power in the bar")
		check(game.preview_power()==350,"Next preview reused the previous shot's power")
		await wait_idle()
		check(not game.path.is_empty(),"Shot did not return an animation path")
		game.path_time=100000
		await process_frame
		await process_frame
		check(game.state.view.Grid.Balls[0].Strokes>=1,"Shot state not applied")
		game.replay()
		check(not game.path.is_empty(),"Replay unavailable")
		game.path_time=100000
		await process_frame
		await process_frame
	await check_cup_finish()
	await check_scorecard()
	game.show_lobby()
	await wait_idle()
	check(game.cover.visible,"Title artwork not restored")
	print("ORBIT smoke: cycling charge/pause/release, nine-hole scorecard/live/caps/ties/early wins/modal controls, practice ghost/retry/isolation, cup timing/scoring/replay/reduced motion/course transitions, scenery motion/freeze, preview mesh/power/stale responses, held aiming, charge/reset, nine scenes, shots, replays and lobby checked")
	game.queue_free()
	await process_frame
	quit(1 if failed else 0)
