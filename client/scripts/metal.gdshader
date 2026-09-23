shader_type spatial;
uniform vec4 base_color : source_color;
varying vec3 world_pos;
varying vec3 world_normal;
float hash(vec2 p) { return fract(sin(dot(p,vec2(12.9898,78.233)))*43758.5453); }
void vertex() {
	world_pos=(MODEL_MATRIX*vec4(VERTEX,1.0)).xyz;
	world_normal=normalize(MODEL_NORMAL_MATRIX*NORMAL);
}
void fragment() {
	vec3 n=abs(world_normal);
	vec2 p=n.y>0.6?world_pos.xz:(n.x>n.z?world_pos.zy:world_pos.xy);
	vec2 cell=abs(fract(p*0.55)-0.5);
	float seam=smoothstep(0.476,0.496,max(cell.x,cell.y));
	float grain=hash(floor(p*vec2(5.0,180.0)));
	float wear=hash(floor(p*0.55));
	ALBEDO=base_color.rgb*(0.88+wear*0.22+(grain-0.5)*0.08)*(1.0-seam*0.32);
	METALLIC=0.62;
	ROUGHNESS=0.4+wear*0.15;
}
