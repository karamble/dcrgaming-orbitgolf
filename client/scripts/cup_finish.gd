extends Node3D

# Presentation only. The caller starts this after the verified path arrives.
const DURATION := 1.8
var active := false
var elapsed := 0.0
var push := 0.0
var lights: Array[MeshInstance3D] = []
var light_material := StandardMaterial3D.new()

func _ready() -> void:
	light_material.shading_mode=BaseMaterial3D.SHADING_MODE_UNSHADED
	light_material.transparency=BaseMaterial3D.TRANSPARENCY_ALPHA
	light_material.albedo_color=Color("ffda83")
	for radius in [0.48,0.85]:
		var ring := MeshInstance3D.new()
		var mesh := TorusMesh.new()
		mesh.inner_radius=radius-0.035
		mesh.outer_radius=radius+0.035
		mesh.rings=64
		mesh.ring_segments=8
		ring.mesh=mesh
		ring.material_override=light_material
		ring.cast_shadow=GeometryInstance3D.SHADOW_CASTING_SETTING_OFF
		add_child(ring)
		lights.append(ring)
	hide()

func begin(cup: Vector3, reduced_motion: bool) -> void:
	position=cup+Vector3(0,0.07,0)
	elapsed=0.0
	active=true
	show()
	advance(0.0,reduced_motion)

func advance(delta: float, reduced_motion: bool) -> void:
	if not active: return
	elapsed=minf(DURATION,elapsed+delta)
	var pulse := sin(PI*elapsed/DURATION)
	push=0.0 if reduced_motion else pulse
	for i in range(lights.size()):
		lights[i].scale=Vector3.ONE*(1.0 if reduced_motion else 1.0+pulse*(0.5+i*0.4))
	light_material.albedo_color=Color(1.0,0.85,0.51,0.8 if reduced_motion else 0.35+0.65*pulse)

func clear() -> void:
	active=false
	push=0.0
	hide()

static func describe(result: Dictionary, par: int) -> Dictionary:
	var strokes := int(result.Ball.Strokes)
	var relative := strokes-par
	var title := "IN THE CUP"
	if strokes==1: title="HOLE IN ONE"
	elif relative<=-3: title="ALBATROSS" if relative==-3 else "%d UNDER PAR" % -relative
	elif relative==-2: title="EAGLE"
	elif relative==-1: title="BIRDIE"
	elif relative==0: title="PAR"
	elif relative==1: title="BOGEY"
	elif relative==2: title="DOUBLE BOGEY"
	var start: Dictionary=result.Path[0]
	var end: Dictionary=result.Ball.Pos
	# Straight-line distance, not accumulated bounces or rolling distance.
	var metres := Vector2(float(end.X)-float(start.X),float(end.Z)-float(start.Z)).length()/10000.0
	return {"title":title,"relative":relative,"long_putt":metres>=12.0,
		"detail":"%d %s · PAR %d%s" % [strokes,"STROKE" if strokes==1 else "STROKES",par,
		"\nLONG PUTT · %.1f m" % metres if metres>=12.0 else ""]}

static func sound_stream(summary: Dictionary) -> AudioStreamWAV:
	var notes := [523.25,659.25]
	if summary.relative==-1: notes=[659.25,783.99,1046.5]
	elif summary.relative<=-2 or summary.title=="HOLE IN ONE": notes=[523.25,659.25,783.99,1046.5]
	if summary.long_putt: notes.append(1318.51)
	var stream := AudioStreamWAV.new()
	stream.format=AudioStreamWAV.FORMAT_16_BITS
	stream.mix_rate=22050
	var samples := int((notes.size()*0.12+0.28)*stream.mix_rate)
	var bytes := PackedByteArray()
	bytes.resize(samples*2)
	for n in range(samples):
		var time := float(n)/stream.mix_rate
		var value := 0.0
		for i in range(notes.size()):
			var age := time-i*0.12
			if age>=0.0 and age<0.4:
				var envelope := minf(age/0.008,1.0)*exp(-age*12.0)*minf((0.4-age)/0.03,1.0)
				value+=(sin(age*TAU*notes[i])+0.18*sin(age*TAU*notes[i]*2.0))*envelope
		bytes.encode_s16(n*2,int(clampf(value*4200.0,-32767,32767)))
	stream.data=bytes
	return stream
