shader_type spatial;
render_mode unshaded;
uniform vec4 ocean : source_color;
varying vec3 sphere_normal;
float hash(vec3 p) { return fract(sin(dot(p,vec3(127.1,311.7,74.7)))*43758.5453); }
float noise(vec3 p) {
	vec3 i=floor(p),f=fract(p); f=f*f*(3.0-2.0*f);
	return mix(mix(mix(hash(i),hash(i+vec3(1,0,0)),f.x),mix(hash(i+vec3(0,1,0)),hash(i+vec3(1,1,0)),f.x),f.y),mix(mix(hash(i+vec3(0,0,1)),hash(i+vec3(1,0,1)),f.x),mix(hash(i+vec3(0,1,1)),hash(i+vec3(1,1,1)),f.x),f.y),f.z);
}
void vertex() { sphere_normal=NORMAL; }
void fragment() {
	// Object-space noise avoids UV seams and stretched polar texels.
	vec3 p=normalize(sphere_normal)*9.0;
	float n=noise(p)*0.7+noise(p*2.07)*0.3;
	float land=smoothstep(0.50,0.57,n);
	float clouds=smoothstep(0.54,0.72,noise(p*2.8+vec3(n*2.0))*0.7+noise(p*6.0)*0.3);
	float rim=pow(1.0-max(dot(normalize(NORMAL),normalize(VIEW)),0.0),3.0);
	float light=smoothstep(-0.25,0.8,dot(normalize(sphere_normal),normalize(vec3(-0.6,0.45,0.7))));
	vec3 surface=mix(ocean.rgb,vec3(0.07,0.13,0.12),land);
	surface=mix(surface,vec3(0.48,0.61,0.72),clouds*0.7);
	ALBEDO=surface*(0.14+light*0.86)+vec3(0.05,0.23,0.45)*rim*(0.2+light*0.6);
}
