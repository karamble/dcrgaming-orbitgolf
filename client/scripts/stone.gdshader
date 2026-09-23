shader_type spatial;
varying vec3 local_pos;
float hash(vec3 p) { return fract(sin(dot(p,vec3(127.1,311.7,74.7)))*43758.5453); }
void vertex() { local_pos=VERTEX; }
void fragment() {
	float grain=hash(floor(local_pos*65.0));
	float coarse=hash(floor(local_pos*2.0));
	ALBEDO=vec3(0.19,0.20,0.25)*(0.9+grain*0.16+coarse*0.12);
	ROUGHNESS=0.98;
}
