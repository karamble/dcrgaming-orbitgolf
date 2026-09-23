shader_type spatial;
render_mode diffuse_burley, specular_schlick_ggx;
uniform float rail_height = 1.0;
uniform vec4 lamp_color : source_color = vec4(0.13,0.78,1.0,1.0);
varying vec3 local_pos;
varying vec3 local_normal;

float rounded_box(vec2 p, vec2 size, float radius) {
	vec2 q=abs(p)-size+radius;
	return length(max(q,vec2(0.0)))+min(max(q.x,q.y),0.0)-radius;
}
void vertex() { local_pos=VERTEX; local_normal=NORMAL; }
void fragment() {
	float side=step(0.85,abs(local_normal.x));
	float top=step(0.3,local_normal.y);
	float end=step(0.85,abs(local_normal.z));
	float y=local_pos.y/rail_height;
	float segment=mod(local_pos.z+1.2,2.4)-1.2;
	vec2 panel=vec2(segment,y-0.57);
	float aa=max(fwidth(panel.y),0.004);
	float recess=1.0-smoothstep(-aa,aa,rounded_box(panel,vec2(0.99,0.19),0.085));
	float lens=1.0-smoothstep(-aa,aa,rounded_box(panel,vec2(0.88,0.112),0.062));
	float joint=1.0-smoothstep(0.055,0.085,abs(abs(segment)-1.2));
	float bolt=1.0-smoothstep(0.023,0.033,length(vec2(abs(segment)-1.08,y-0.79)));
	float bumper=1.0-smoothstep(0.12,0.20,y);
	vec3 gunmetal=vec3(0.075,0.105,0.13);
	vec3 titanium=vec3(0.34,0.39,0.42);
	ALBEDO=mix(gunmetal,titanium,max(top,end)*0.85);
	ALBEDO=mix(ALBEDO,titanium,joint*0.75);
	ALBEDO=mix(ALBEDO,vec3(0.009,0.018,0.026),side*recess);
	ALBEDO=mix(ALBEDO,vec3(0.012,0.021,0.026),side*bumper);
	ALBEDO=mix(ALBEDO,vec3(0.022),side*bolt);
	ALBEDO=mix(ALBEDO,lamp_color.rgb*0.65,side*lens);
	// Dark bezel and bright inset lens, not an emissive slab on top.
	float halo=exp(-max(rounded_box(panel,vec2(0.92,0.14),0.07),0.0)*18.0)*0.10;
	EMISSION=lamp_color.rgb*side*(lens*1.55+halo);
	METALLIC=mix(0.68,0.10,side*max(lens,bumper));
	ROUGHNESS=mix(0.36,0.23,side*lens);
}
