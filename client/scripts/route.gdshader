shader_type spatial;
render_mode unshaded, cull_disabled, depth_draw_never;
uniform vec4 route_color : source_color = vec4(0.15,1.0,0.78,1.0);
uniform float motion = 1.0;
uniform float route_length = 1.0;
void fragment() {
	float across=abs(UV.x-0.5)*2.0;
	float core=1.0-smoothstep(0.14,0.40,across);
	float halo=pow(1.0-across,2.0)*0.4;
	// Procedural ribbon texture: metre-spaced light packets, flowing forward.
	// World-distance UVs keep their spacing stable as shot power changes.
	float phase=fract(UV.y*route_length*0.9-TIME*0.45*motion);
	float dash=smoothstep(0.02,0.12,phase)*(1.0-smoothstep(0.62,0.78,phase));
	float fade=pow(max(0.0,1.0-UV.y),0.85);
	ALBEDO=mix(route_color.rgb,vec3(0.90,1.0,0.98),core*0.55);
	ALPHA=(core*(0.12+0.88*dash)+halo*dash)*fade;
}
