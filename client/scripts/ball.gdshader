shader_type spatial;
uniform vec4 stripe_color : source_color;
void fragment() {
	vec2 p=UV*vec2(32.0,16.0);
	p.x+=mod(floor(p.y),2.0)*0.5;
	float d=length(fract(p)-0.5);
	float dimples=1.0-smoothstep(0.20,0.39,d);
	float band=1.0-smoothstep(0.065,0.09,abs(UV.y-0.5));
	ALBEDO=mix(vec3(0.88,0.94,0.98),stripe_color.rgb,band)*(1.0-dimples*0.16);
	ROUGHNESS=0.25+dimples*0.13;
	METALLIC=0.12;
}
