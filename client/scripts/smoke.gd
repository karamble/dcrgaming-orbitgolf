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
	game.update_controls(10.0)
	check(game.power.value==1000 and game.charging and game.backend.sequence==before,"Full charge must cap without auto-firing")
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
	game.cancel_charge()
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
	game.show_lobby()
	await wait_idle()
	check(game.cover.visible,"Title artwork not restored")
	print("ORBIT smoke: scenery motion/freeze, preview mesh/power/stale responses, held aiming, charge/reset, nine scenes, shots, replays and lobby checked")
	game.queue_free()
	await process_frame
	quit(1 if failed else 0)
