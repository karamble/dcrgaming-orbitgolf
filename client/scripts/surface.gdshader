shader_type spatial;
render_mode diffuse_burley, specular_schlick_ggx;
uniform vec4 base_color : source_color = vec4(0.035,0.15,0.18,1.0);
uniform vec4 trim_color : source_color = vec4(0.1,0.9,0.7,1.0);
uniform vec2 deck_center;
uniform vec2 deck_size;
varying vec3 world_pos;
float hash(vec2 p) { return fract(sin(dot(p,vec2(127.1,311.7)))*43758.5453); }
float noise(vec2 p) {
	vec2 i=floor(p), f=fract(p); f=f*f*(3.0-2.0*f);
	return mix(mix(hash(i),hash(i+vec2(1,0)),f.x),mix(hash(i+vec2(0,1)),hash(i+vec2(1,1)),f.x),f.y);
}
void vertex() { world_pos=(MODEL_MATRIX*vec4(VERTEX,1.0)).xyz; }
void fragment() {
	vec2 p=world_pos.xz;
	vec2 grid=vec2(1.0,1.7320508);
	vec2 a=mod(p*2.5,grid)-grid*0.5;
	vec2 b=mod(p*2.5-grid*0.5,grid)-grid*0.5;
	vec2 h=dot(a,a)<dot(b,b)?a:b;
	float edge=0.5-max(abs(h.x),dot(abs(h),vec2(0.5,0.8660254)));
	float aa=max(fwidth(edge),0.003);
	float seam=1.0-smoothstep(0.008,0.008+aa,edge);
	float weave=noise(p*170.0);
	float weave_visibility=1.0-smoothstep(0.012,0.055,max(length(dFdx(p)),length(dFdy(p))));
	vec2 border=deck_size*0.5-abs(p-deck_center);
	float inset=min(border.x,border.y);
	float rim=1.0-smoothstep(0.13,0.24,inset);
	float pinstripe=1.0-smoothstep(0.018,0.018+max(fwidth(inset),0.005),abs(inset-0.32));
	float panels=noise(floor(p*0.6))*0.08;
	vec3 turf=base_color.rgb*(0.86+noise(p*1.5)*0.2+panels+(weave-0.5)*0.18*weave_visibility);
	turf=mix(turf,turf*0.58,seam*0.28);
	ALBEDO=mix(turf,vec3(0.055,0.075,0.095),rim);
	ALBEDO=mix(ALBEDO,trim_color.rgb*0.45,pinstripe*0.65);
	METALLIC=mix(0.12,0.65,rim);
	ROUGHNESS=mix(0.88,0.38,rim)+(weave-0.5)*0.05*weave_visibility;
	EMISSION=trim_color.rgb*pinstripe*0.08;
}
