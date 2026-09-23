shader_type spatial;
render_mode unshaded, blend_add, cull_disabled, depth_draw_never;
uniform float opacity = 0.0;
void fragment() {
	float edge=pow(max(0.0,1.0-abs(UV.x*2.0-1.0)),2.0);
	float fade=pow(1.0-UV.y,2.2);
	ALBEDO=mix(vec3(0.7,0.92,1.0),vec3(0.12,0.5,0.85),UV.y);
	ALPHA=edge*fade*opacity*0.85;
}
