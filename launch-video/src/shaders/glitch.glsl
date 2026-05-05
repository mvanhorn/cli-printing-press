// glitch.glsl - RGB channel split + scanline jitter for the Hook scene.
//
// Used by GlitchOverlay (HtmlInCanvas wrapper). The DOM child is captured
// per frame via drawElementImage and stored in u_dom. We sample three
// horizontally-shifted copies of u_dom (red/green/blue) and composite them
// with a vertical scanline streak whose Y position scrolls with u_time.
//
// Uniforms:
//   u_dom        - sampler2D, the DOM child captured by drawElementImage
//   u_time       - float, seconds since scene start
//   u_intensity  - float [0..1], glitch ramp (subtle -> peak)
//   u_resolution - vec2, canvas size in px

precision mediump float;

uniform sampler2D u_dom;
uniform float u_time;
uniform float u_intensity;
uniform vec2 u_resolution;

varying vec2 v_uv;

float rand(vec2 p) {
  return fract(sin(dot(p, vec2(12.9898, 78.233))) * 43758.5453);
}

void main() {
  vec2 uv = v_uv;

  // Horizontal jitter that increases with intensity. Each scanline (8px tall)
  // gets its own random offset.
  float bandY = floor(uv.y * u_resolution.y / 8.0) / (u_resolution.y / 8.0);
  float jitter = (rand(vec2(bandY, floor(u_time * 6.0))) - 0.5) * 0.04 * u_intensity;
  uv.x += jitter;

  // Channel split - red shifts left, blue shifts right; magnitude scales
  // with intensity and oscillates with u_time.
  float split = 0.006 * u_intensity * (1.0 + 0.4 * sin(u_time * 9.0));
  float r = texture2D(u_dom, vec2(uv.x - split, uv.y)).r;
  float g = texture2D(u_dom, uv).g;
  float b = texture2D(u_dom, vec2(uv.x + split, uv.y)).b;

  vec3 colour = vec3(r, g, b);

  // Scanline streak - bright horizontal band scrolling vertically.
  float streakY = mod(u_time * 0.3, 1.0);
  float streakDist = abs(uv.y - streakY);
  float streak = smoothstep(0.012, 0.0, streakDist) * 0.18 * u_intensity;
  colour += streak;

  // Subtle vignette to land the eye on the centre.
  vec2 vig = uv - 0.5;
  float vignette = 1.0 - dot(vig, vig) * 0.6;
  colour *= vignette;

  gl_FragColor = vec4(colour, 1.0);
}
