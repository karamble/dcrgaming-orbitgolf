extends Node3D

var ribbon := MeshInstance3D.new()
var target := MeshInstance3D.new()
var origin := MeshInstance3D.new()
var material := ShaderMaterial.new()
var distance_metres := 0.0

func _ready() -> void:
	material.shader=preload("res://scripts/route.gdshader")
	ribbon.material_override=material
	ribbon.cast_shadow=GeometryInstance3D.SHADOW_CASTING_SETTING_OFF
	add_child(ribbon)
	for marker in [origin,target]:
		var ring := TorusMesh.new()
		ring.inner_radius=0.30
		ring.outer_radius=0.34
		ring.rings=48
		ring.ring_segments=6
		marker.mesh=ring
		marker.cast_shadow=GeometryInstance3D.SHADOW_CASTING_SETTING_OFF
		add_child(marker)

func draw(result: Dictionary, reduced_motion: bool, ghost := false) -> void:
	ribbon.mesh=null
	origin.hide()
	target.hide()
	distance_metres=0.0
	var points: Array[Vector3]=[]
	var raw: Array=result.Path
	# The authoritative penalty path ends with a teleport back to the tee.
	# Show the flight into the hazard, never a fictional line back to the ball.
	var count := raw.size()-(1 if result.Penalty else 0)
	for i in range(count):
		var p: Vector3=Vector3(raw[i].X,raw[i].Y,raw[i].Z)/10000.0
		if points.is_empty() or points[-1].distance_to(p)>0.055 or i==count-1:
			points.append(p+Vector3(0,0.025,0))
	if points.size()<2: return
	var tint := Color("ff9675") if result.Penalty else (Color("ffda83") if result.Ball.Holed else Color("55f5d2"))
	if ghost: tint=Color("ba99ff")
	material.set_shader_parameter("ghost",ghost)
	material.set_shader_parameter("route_color",tint)
	material.set_shader_parameter("motion",0.0 if reduced_motion or ghost else 1.0)
	var marker_material := StandardMaterial3D.new()
	marker_material.shading_mode=BaseMaterial3D.SHADING_MODE_UNSHADED
	marker_material.albedo_color=tint
	if ghost:
		marker_material.transparency=BaseMaterial3D.TRANSPARENCY_ALPHA
		marker_material.albedo_color.a=0.6
	for marker in [origin,target]: marker.material_override=marker_material
	origin.show()
	origin.position=points[0]-Vector3(0,0.13,0)
	target.position=points[-1]
	# A penalty has no resting point here: its final sample is a reset teleport.
	target.visible=not result.Penalty
	target.scale=Vector3.ONE*(0.75 if result.Ball.Holed else 1.0)
	var lengths: Array[float]=[0.0]
	for i in range(1,points.size()): lengths.append(lengths[-1]+points[i-1].distance_to(points[i]))
	distance_metres=lengths[-1]
	material.set_shader_parameter("route_length",distance_metres)
	var mesh := SurfaceTool.new()
	mesh.begin(Mesh.PRIMITIVE_TRIANGLES)
	for i in range(points.size()-1):
		var tangent := (points[i+1]-points[i]).normalized()
		if tangent.length_squared()<0.1: continue
		var side := tangent.cross(Vector3.UP).normalized()*0.20
		if side.length_squared()<0.001: side=Vector3.RIGHT*0.20
		var verts := [points[i]-side,points[i]+side,points[i+1]+side,points[i+1]-side]
		var u0: float=lengths[i]/maxf(distance_metres,0.001)
		var u1: float=lengths[i+1]/maxf(distance_metres,0.001)
		var uvs := [Vector2(0,u0),Vector2(1,u0),Vector2(1,u1),Vector2(0,u1)]
		for n in [0,1,2,0,2,3]:
			mesh.set_uv(uvs[n])
			mesh.add_vertex(verts[n])
	ribbon.mesh=mesh.commit()
