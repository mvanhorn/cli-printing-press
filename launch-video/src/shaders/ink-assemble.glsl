// ink-assemble.glsl - noise-driven particle gather for the CTA wordmark.
//
// Used by WordmarkInkAssemble (HtmlInCanvas wrapper). The wordmark is rendered
// as a DOM <span>; we capture it as the alpha mask. A 2D noise field decides
// per-pixel reveal threshold; as u_progress crosses each pixel's threshold,
// the pixel reveals. Produces an organic, ink-bleed-style assemble rather
// than a generic fade.
//
// Uniforms:
//   u_dom        - sampler2D, captured wordmark text
//   u_progress   - float [0..1]
//   u_time       - float, seconds since scene start (drives subtle particle drift)
//   u_resolution - vec2

precision mediump float;

uniform sampler2D u_dom;
uniform float u_progress;
uniform float u_time;
uniform vec2 u_resolution;

varying vec2 v_uv;

float hash(vec2 p) {
  return fract(sin(dot(p, vec2(127.1, 311.7))) * 43758.5453123);
}

float noise(vec2 p) {
  vec2 i = floor(p);
  vec2 f = fract(p);
  vec2 u = f * f * (3.0 - 2.0 * f);
  return mix(
    mix(hash(i + vec2(0.0, 0.0)), hash(i + vec2(1.0, 0.0)), u.x),
    mix(hash(i + vec2(0.0, 1.0)), hash(i + vec2(1.0, 1.0)), u.x),
    u.y
  );
}

void main() {
  vec4 dom = texture2D(u_dom, v_uv);
  float mask = dom.a; // 1 inside wordmark text, 0 elsewhere

  // Per-pixel reveal threshold derived from noise. Higher noise = revealed
  // later. Noise pattern drifts slowly with u_time for ink-flow texture.
  float threshold = noise(v_uv * 6.0 + vec2(u_time * 0.05, 0.0));

  // Pixel revealed if u_progress has crossed its threshold AND we're inside
  // the text mask. Smoothstep gives a soft ink-bleed edge.
  float reveal = smoothstep(threshold, threshold + 0.08, u_progress);
  float visible = reveal * mask;

  // Bleed colour: pure white at full reveal, slight off-white at the edge to
  // suggest fresh ink darkening as it dries.
  vec3 ink = mix(vec3(0.92, 0.94, 0.96), vec3(1.0), reveal);

  // Outside the wordmark mask, render the ambient particle field that
  // shrinks with progress (the "scatter").
  float ambientNoise = noise(v_uv * 24.0 + vec2(u_time * 0.4, u_time * 0.2));
  float ambient = step(0.96, ambientNoise) * (1.0 - u_progress) * 0.18;

  vec3 colour = ink * visible + vec3(ambient);

  gl_FragColor = vec4(colour, 1.0);
}
