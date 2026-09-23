extends Node3D

var accent := Color("39f3cd")
var metal := Color("163249")
var world_name := "SHIPYARD"
var decor: Node3D
var material_cache: Dictionary = {}
var motion_enabled := true
var space_time := 0.0
var animated: Array[Dictionary] = []
var comets: Array[Dictionary] = []
const Metal = preload("res://scripts/metal.gdshader")
const Surface = preload("res://scripts/surface.gdshader")
const GolfBall = preload("res://scripts/ball.gdshader")
const Rail = preload("res://scripts/rail.gd")
var wall_nodes: Array[MeshInstance3D] = []
var roofs: Array[Dictionary] = []
var gates: Array[Dictionary] = []

func material(color: Color, glow: float = 0.0) -> Material:
	var key := str(color)+"/"+str(glow)
	if material_cache.has(key): return material_cache[key]
	if glow<=0.0:
		var metal_material := ShaderMaterial.new()
		metal_material.shader=Metal
		metal_material.set_shader_parameter("base_color",color)
		material_cache[key]=metal_material
		return metal_material
	var m := StandardMaterial3D.new()
	m.albedo_color = color
	m.roughness = 0.62
	if glow > 0.0:
		m.emission_enabled = true
		m.emission = color
		m.emission_energy_multiplier = glow
	material_cache[key]=m
	return m

func golf_ball(color: Color) -> MeshInstance3D:
	var ball := sphere(Vector3.ZERO,0.18,Color.WHITE)
	var coat := ShaderMaterial.new()
	coat.shader=GolfBall
	coat.set_shader_parameter("stripe_color",color)
	ball.material_override=coat
	ball.rotation.z=0.3
	return ball

func mesh_item(mesh: Mesh, pos: Vector3, color: Color, glow: float = 0.0, parent: Node3D = self) -> MeshInstance3D:
	var node := MeshInstance3D.new()
	node.mesh = mesh
	node.material_override = material(color, glow)
	node.position = pos
	parent.add_child(node)
	if parent==decor: node.cast_shadow=GeometryInstance3D.SHADOW_CASTING_SETTING_OFF
	return node

func box(pos: Vector3, size: Vector3, color: Color, glow: float = 0.0, parent: Node3D = self) -> MeshInstance3D:
	var mesh := BoxMesh.new()
	mesh.size = size
	return mesh_item(mesh, pos, color, glow, parent)

func sphere(pos: Vector3, radius: float, color: Color, glow: float = 0.0, parent: Node3D = self) -> MeshInstance3D:
	var mesh := SphereMesh.new()
	mesh.radius = radius
	mesh.height = radius * 2
	mesh.radial_segments = 32
	mesh.rings = 16
	return mesh_item(mesh, pos, color, glow, parent)

func ring(pos: Vector3, radius: float, color: Color, parent: Node3D = self) -> MeshInstance3D:
	var mesh := TorusMesh.new()
	mesh.inner_radius = radius - 0.045
	mesh.outer_radius = radius + 0.045
	mesh.rings = 48
	mesh.ring_segments = 8
	return mesh_item(mesh, pos, color, 1.0, parent)

func beam(a: Vector3, b: Vector3, width: float, color: Color, parent: Node3D = self) -> MeshInstance3D:
	var node := box((a+b)*0.5, Vector3(width, width, a.distance_to(b)), color, 0.7, parent)
	if a.distance_to(b) > 0.001:
		node.look_at_from_position((a+b)*0.5, b, Vector3.UP if abs((b-a).normalized().y)<0.99 else Vector3.RIGHT)
	return node

func coord(d: Dictionary) -> Vector3:
	return Vector3(d.X, d.Y, d.Z) / 10000.0

func build(hole: Dictionary) -> void:
	wall_nodes.clear()
	roofs.clear()
	gates.clear()
	animated.clear()
	comets.clear()
	space_time=0.0
	for child in get_children():
		remove_child(child)
		child.queue_free()
	world_name = hole.World
	accent = Color("39f3cd") if world_name == "SHIPYARD" else (Color("ffb454") if world_name == "REACTOR" else Color("ba99ff"))
	metal = Color("163249") if world_name == "SHIPYARD" else (Color("302634") if world_name == "REACTOR" else Color("272d4b"))
	var turf := Color("102f40") if world_name=="SHIPYARD" else (Color("30383e") if world_name=="REACTOR" else Color("282e48"))
	for p in hole.Platforms:
		var x: float = p.X / 10000.0
		var z: float = p.Z / 10000.0
		var w: float = p.W / 10000.0
		var d: float = p.D / 10000.0
		var y: float = p.Y / 10000.0
		var rise: float = p.Rise / 10000.0
		var corners := [Vector3(x-w/2,y,z-d/2),Vector3(x+w/2,y,z-d/2),Vector3(x+w/2,y+rise,z+d/2),Vector3(x-w/2,y+rise,z+d/2)]
		var surface := SurfaceTool.new()
		surface.begin(Mesh.PRIMITIVE_TRIANGLES)
		for index in [0,1,2,0,2,3]:
			surface.set_normal(Vector3(0,1,-rise/d).normalized())
			surface.add_vertex(corners[index])
		var deck := mesh_item(surface.commit(), Vector3.ZERO, turf)
		var finish := ShaderMaterial.new()
		finish.shader=Surface
		finish.set_shader_parameter("base_color",turf)
		finish.set_shader_parameter("trim_color",accent)
		finish.set_shader_parameter("deck_center",Vector2(x,z))
		finish.set_shader_parameter("deck_size",Vector2(w,d))
		deck.material_override=finish
		for n in range(4):
			beam(corners[n], corners[(n+1)%4], 0.07, accent)
		if rise == 0:
			box(Vector3(x,y-0.48,z),Vector3(w,0.9,d),metal.darkened(0.15))
			box(Vector3(x,y-1.0,z),Vector3(w-0.45,0.35,d-0.45),metal.lightened(0.12))
			for side in [-1,1]:
				for k in range(int(d/3)):
					var lamp := Vector3(x+side*(w/2+0.018),y-0.4,z-d/2+1.5+k*3)
					box(lamp,Vector3(0.025,0.075,0.8),accent,0.6)
		if rise == 0:
			for side in [-1,1]:
				box(Vector3(x+side*(w/2-0.5),y-1.2,z),Vector3(0.4,1.0,d*0.8),metal)
	for wall in hole.Walls if hole.Walls != null else []:
		if wall.get("Ceiling",false):
			var roof := box(coord(wall)+Vector3(0,float(wall.H)/20000.0,0),Vector3(wall.W,wall.H,wall.D)/10000.0,metal)
			var coat := StandardMaterial3D.new()
			coat.albedo_color=metal.lightened(0.25)
			coat.transparency=BaseMaterial3D.TRANSPARENCY_ALPHA
			coat.cull_mode=BaseMaterial3D.CULL_DISABLED
			roof.material_override=coat
			roofs.append({"node":roof,"material":coat,"wall":wall})
			wall_nodes.append(roof)
			for end in [-1,1]:
				box(coord(wall)+Vector3(0,-0.05,end*float(wall.D)/20000.0),Vector3(float(wall.W)/10000.0,0.10,0.15),accent,1.0)
			continue
		var rail := Rail.build(wall,hole)
		add_child(rail)
		wall_nodes.append(rail)
	for b in hole.Bumpers if hole.Bumpers != null else []:
		var pos := coord(b)
		var cylinder := CylinderMesh.new()
		cylinder.top_radius=b.Radius/10000.0
		cylinder.bottom_radius=cylinder.top_radius
		cylinder.height=1
		mesh_item(cylinder,pos+Vector3(0,0.5,0),metal.lightened(0.2))
		ring(pos+Vector3(0,0.95,0),cylinder.top_radius,accent)
		ring(pos+Vector3(0,0.18,0),cylinder.top_radius,accent.darkened(0.2))
	for f in hole.Fields if hole.Fields != null else []:
		var pos := Vector3(f.X,100,f.Z)/10000.0
		for radius in [0.4,0.7,1.0]:
			ring(pos,f.Radius/10000.0*radius,accent.darkened(0.3))
	for g in hole.Gates if hole.get("Gates")!=null else []:
		var pos := coord(g)
		var size := Vector3(g.W,g.H,g.D)/10000.0
		var door := box(pos+Vector3(0,size.y/2.0,0),size,Color("ff536a") if g.Laser else metal.lightened(0.3),0.5 if g.Laser else 0.0)
		if g.Laser:
			var laser := StandardMaterial3D.new()
			laser.transparency=BaseMaterial3D.TRANSPARENCY_ALPHA
			laser.albedo_color=Color(1.0,0.08,0.2,0.42)
			laser.emission_enabled=true
			laser.emission=Color("ff365c")
			door.material_override=laser
		for side in [-1,1]:
			box(pos+Vector3(side*(size.x/2.0+0.25),2.3,0),Vector3(0.45,4.6,0.8),metal)
		box(pos+Vector3(0,4.6,0),Vector3(size.x+0.9,0.3,0.8),metal)
		var signal_lamp := box(pos+Vector3(0,4.8,0.45),Vector3(size.x,0.12,0.12),MINT_COLOR,0.9)
		gates.append({"data":g,"node":door,"lamp":signal_lamp,"base":pos,"height":size.y})
	var cup := coord(hole.Cup)
	var disc := CylinderMesh.new()
	disc.top_radius=0.34
	disc.bottom_radius=0.34
	disc.height=0.015
	mesh_item(disc,cup+Vector3(0,0.025,0),Color("020612"))
	ring(cup+Vector3(0,0.04,0),0.36,Color.WHITE)
	box(cup+Vector3(0,1.35,0),Vector3(0.035,2.7,0.035),Color("dbeef7"),0.3)
	box(cup+Vector3(0.42,2.55,0),Vector3(0.85,0.45,0.025),Color("ffbc62"),0.15)
	var course_number := Label3D.new()
	course_number.text=hole.ID.split("-")[-1]
	course_number.position=cup+Vector3(0.4,2.55,0.025)
	course_number.font_size=48
	course_number.pixel_size=0.005
	course_number.modulate=Color("122235")
	add_child(course_number)
	ring(coord(hole.Tee)+Vector3(0,0.02,0),0.45,accent)
	build_decor()
	update_obstacles(0)

const MINT_COLOR := Color("39f3cd")

# Keep this integer phase/lift calculation identical to course.GateWall.
# Reduced motion must not freeze gameplay obstacles, only decoration.
func update_obstacles(tick: int) -> void:
	for item in gates:
		var g: Dictionary=item.data
		var phase := (tick+int(g.Offset))%int(g.Period)
		var opened := phase<int(g.Open)
		if g.Laser:
			item.node.visible=not opened
		else:
			var lift := mini(24,mini(phase,int(g.Open)-phase)) if opened else 0
			var lift_units: int=lift*(int(g.H)+10000)/24
			item.node.position=item.base+Vector3(0,item.height/2.0+float(lift_units)/10000.0,0)
		item.lamp.material_override=material(MINT_COLOR if opened else Color("ff536a"),0.9)

func reveal_tunnels(ball: Vector3, overview: bool) -> void:
	for item in roofs:
		var w: Dictionary=item.wall
		var near := absf(ball.x-float(w.X)/10000.0)<float(w.W)/20000.0+5.0 and absf(ball.z-float(w.Z)/10000.0)<float(w.D)/20000.0+8.0
		var tint: Color=item.material.albedo_color
		tint.a=0.12 if near or overview else 0.85
		item.material.albedo_color=tint

func build_decor() -> void:
	decor = Node3D.new()
	add_child(decor)
	var rng := RandomNumberGenerator.new()
	rng.seed=72341
	for n in range(100):
		var p := Vector3(rng.randf_range(-100,100),rng.randf_range(-70,-25),rng.randf_range(-100,70))
		var star := sphere(p,rng.randf_range(0.04,0.13),Color("b3cde3"),1.0,decor)
		if n%5==0:
			animate(star,Vector3.ZERO,Vector3.ZERO,0.5,float(n))
			animated[-1]["twinkle"]=true
	var planet := sphere(Vector3(18,-55,-65),22,Color("2b7188") if world_name == "SHIPYARD" else Color("544675"),0.1,decor)
	planet.mesh.radial_segments=96
	planet.mesh.rings=64
	var atmosphere := ShaderMaterial.new()
	atmosphere.shader=preload("res://scripts/planet.gdshader")
	atmosphere.set_shader_parameter("ocean",Color("16436c"))
	planet.material_override=atmosphere
	animate(planet,Vector3(0.002,0.025,0.001),Vector3(2,1.2,1.5),0.03)
	for n in range(3):
		var orbit := ring(planet.position,29+n*0.5,accent.darkened(0.4),decor)
		orbit.rotation_degrees=Vector3(14,0,24)
		animate(orbit,Vector3(0.003,0.009,0.004)*(1.0+float(n)*0.2),Vector3(2,1.2,1.5),0.03)
	if world_name == "SHIPYARD":
		for side in [-1,1]:
			for n in range(5):
				var p := Vector3(side*15,-5,-30+n*14)
				box(p,Vector3(2,20,2),metal,0,decor)
				beam(p+Vector3(0,10,0),p+Vector3(-side*7,14,0),0.5,metal.lightened(0.2),decor)
				beam(p+Vector3(0,8,0),p+Vector3(0,1,0),0.15,accent,decor)
			for n in range(8):
				box(Vector3(side*22,-9,-28+n*8),Vector3(8,4,5),metal.lightened(0.08),0,decor)
				box(Vector3(side*22,-6.9,-28+n*8),Vector3(7,0.08,0.15),accent,0.7,decor)
	elif world_name == "REACTOR":
		for n in range(6):
			var r := ring(Vector3(0,-8-n*1.5,0),10+n*0.7,accent,decor)
			r.rotation_degrees.z=n*3
			animate(r,Vector3(0.012,0.04,0.018)*(1.0+float(n)*0.15),Vector3(0,0.12,0),0.5,float(n))
		sphere(Vector3(0,-12,0),4,accent,1.4,decor)
		for side in [-1,1]:
			for n in range(6):
				box(Vector3(side*16,0,-32+n*12),Vector3(3,22,3),metal,0,decor)
				box(Vector3(side*14.4,0,-32+n*12),Vector3(0.1,18,0.3),accent,0.8,decor)
	else:
		for n in range(32):
			var p := Vector3(rng.randf_range(-35,35),rng.randf_range(-28,-10),rng.randf_range(-35,35))
			var rock := asteroid(p,rng.randf_range(1,4))
			rock.scale=Vector3(1,rng.randf_range(0.6,1.6),0.8)
			animate(rock,Vector3(rng.randf_range(-0.12,0.12),rng.randf_range(0.04,0.13),rng.randf_range(-0.08,0.08)),Vector3(2,0.65,1.5),rng.randf_range(0.1,0.22),rng.randf_range(0,TAU))
	if world_name!="SHATTERED MOON":
		for n in range(8):
			var rock := asteroid(Vector3(rng.randf_range(-50,50),rng.randf_range(-35,-18),rng.randf_range(-55,30)),rng.randf_range(0.8,2.4))
			animate(rock,Vector3(0.04,0.06,-0.02),Vector3(2,0.6,2),0.14,float(n))
	for n in range(3):
		make_comet(n)

func animate(node: Node3D, spin: Vector3, drift: Vector3, rate: float, phase := 0.0) -> void:
	animated.append({"node":node,"home":node.position,"spin":spin,"drift":drift,"rate":rate,"phase":phase,"scale":node.scale})

func make_comet(index: int) -> void:
	var comet := Node3D.new()
	decor.add_child(comet)
	var head := sphere(Vector3.ZERO,0.13,Color("d9f5ff"),2.0,comet)
	head.cast_shadow=GeometryInstance3D.SHADOW_CASTING_SETTING_OFF
	var tail := SurfaceTool.new()
	tail.begin(Mesh.PRIMITIVE_TRIANGLES)
	for axis in [Vector3.RIGHT,Vector3.UP]:
		var vertices := [-axis*0.16,axis*0.16,Vector3(0,0,9)+axis*0.5,Vector3(0,0,9)-axis*0.5]
		var uvs := [Vector2(0,0),Vector2(1,0),Vector2(1,1),Vector2(0,1)]
		for n in [0,1,2,0,2,3]:
			tail.set_uv(uvs[n])
			tail.add_vertex(vertices[n])
	var ribbon := mesh_item(tail.commit(),Vector3.ZERO,Color.WHITE,1.0,comet)
	ribbon.cast_shadow=GeometryInstance3D.SHADOW_CASTING_SETTING_OFF
	var trail := ShaderMaterial.new()
	trail.shader=preload("res://scripts/comet.gdshader")
	ribbon.material_override=trail
	var start := Vector3(-75,-30-float(index)*5,-65+float(index)*12)
	var finish := Vector3(75,-18-float(index)*4,15-float(index)*20)
	if index%2==1:
		start.x=75
		finish.x=-75
	comet.position=start
	comet.look_at(finish,Vector3.UP)
	comet.hide()
	comets.append({"node":comet,"start":start,"finish":finish,"phase":float(index)*12.0,"material":trail})

func _process(delta: float) -> void:
	if not motion_enabled: return
	space_time+=delta
	for item in animated:
		var node: Node3D=item.node
		var phase: float=space_time*item.rate+item.phase
		node.rotation+=item.spin*delta
		node.position=item.home+item.drift*Vector3(sin(phase),sin(phase*0.7),cos(phase)-1.0)
		if item.get("twinkle",false): node.scale=item.scale*(0.88+0.12*sin(phase))
	for item in comets:
		var phase := fposmod(space_time+float(item.phase),36.0)/6.0
		item.node.visible=phase<1.0
		if phase<1.0:
			item.node.position=item.start.lerp(item.finish,phase)
			item.material.set_shader_parameter("opacity",smoothstep(0.0,0.12,phase)*(1.0-smoothstep(0.80,1.0,phase)))

func asteroid(pos: Vector3, radius: float) -> MeshInstance3D:
	var base := SphereMesh.new()
	base.radius=radius
	base.height=radius*2.0
	base.radial_segments=12
	base.rings=7
	var arrays := base.get_mesh_arrays()
	var vertices: PackedVector3Array=arrays[Mesh.ARRAY_VERTEX]
	var indices: PackedInt32Array=arrays[Mesh.ARRAY_INDEX]
	var surface := SurfaceTool.new()
	surface.begin(Mesh.PRIMITIVE_TRIANGLES)
	surface.set_smooth_group(-1)
	for index in indices:
		var v := vertices[index]
		var distortion := 0.88+0.16*sin(v.x*3.1+pos.x)*sin(v.y*4.7+pos.z)+0.09*cos(v.z*7.0)
		surface.add_vertex(v*distortion)
	surface.generate_normals()
	var rock := mesh_item(surface.commit(),pos,Color("66637a"),0,decor)
	var stone := ShaderMaterial.new()
	stone.shader=preload("res://scripts/stone.gdshader")
	rock.material_override=stone
	return rock
