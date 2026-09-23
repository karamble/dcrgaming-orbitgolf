extends RefCounted

const THICKNESS := 0.85
const RailShader = preload("res://scripts/rail.gdshader")

static func bounds(wall: Dictionary, hole: Dictionary) -> Dictionary:
	var center := Vector3(wall.X,wall.Y,wall.Z)/10000.0
	var size := Vector3(wall.W,wall.H,wall.D)/10000.0
	# Zero-thickness perimeter planes keep their playable face exactly where
	# Go puts it. Their decorative armor grows only towards open space.
	if wall.W==0 or wall.D==0:
		for p in hole.Platforms:
			if wall.W==0 and absf(absf(float(wall.X-p.X))-float(p.W)/2.0)<1.0 and absf(float(wall.Z-p.Z))<=float(p.D)/2.0:
				size.x=THICKNESS
				center.x+=signf(float(wall.X-p.X))*THICKNESS/2.0
				break
			if wall.D==0 and absf(absf(float(wall.Z-p.Z))-float(p.D)/2.0)<1.0 and absf(float(wall.X-p.X))<=float(p.W)/2.0:
				size.z=THICKNESS
				center.z+=signf(float(wall.Z-p.Z))*THICKNESS/2.0
				break
		# Close external corners, but do not extend open ramp/gap ends.
		if wall.W==0:
			var lo: float=float(wall.Z-wall.D/2)/10000.0
			var hi: float=float(wall.Z+wall.D/2)/10000.0
			for other in hole.Walls:
				if other.D!=0 or absf(float(wall.X-other.X))>float(other.W)/2.0: continue
				if absf(float(other.Z)/10000.0-lo)<0.001: lo-=THICKNESS
				if absf(float(other.Z)/10000.0-hi)<0.001: hi+=THICKNESS
			size.z=hi-lo
			center.z=(lo+hi)/2.0
	return {"center":center,"size":size}

static func build(wall: Dictionary, hole: Dictionary) -> MeshInstance3D:
	var shape := bounds(wall,hole)
	var size: Vector3=shape.size
	var along_z: bool=size.z>size.x
	var thickness: float=size.x if along_z else size.z
	var length_: float=size.z if along_z else size.x
	var h: float=size.y
	var half := thickness/2.0
	var bevel := minf(0.14,thickness*0.22)
	var profile: Array[Vector2]=[Vector2(-half,0),Vector2(-half,h-0.16),Vector2(-half+bevel,h),Vector2(half-bevel,h),Vector2(half,h-0.16),Vector2(half,0)]
	var surface := SurfaceTool.new()
	surface.begin(Mesh.PRIMITIVE_TRIANGLES)
	for i in range(profile.size()):
		var a := profile[i]
		var b := profile[(i+1)%profile.size()]
		var edge := b-a
		var normal := Vector3(-edge.y,edge.x,0).normalized()
		var vertices := [Vector3(a.x,a.y,-length_/2),Vector3(b.x,b.y,-length_/2),Vector3(b.x,b.y,length_/2),Vector3(a.x,a.y,length_/2)]
		for j in [0,1,2,0,2,3]:
			surface.set_normal(normal)
			surface.add_vertex(vertices[j])
	for end in [-1,1]:
		for i in range(1,profile.size()-1):
			for index in ([0,i,i+1] if end==1 else [0,i+1,i]):
				surface.set_normal(Vector3(0,0,end))
				surface.add_vertex(Vector3(profile[index].x,profile[index].y,end*length_/2))
	var node := MeshInstance3D.new()
	node.name="ArmoredRail"
	node.mesh=surface.commit()
	node.position=shape.center
	if not along_z: node.rotation.y=PI/2.0
	var finish := ShaderMaterial.new()
	finish.shader=RailShader
	finish.set_shader_parameter("rail_height",h)
	finish.set_shader_parameter("lamp_color",Color("36cfff"))
	node.material_override=finish
	return node
