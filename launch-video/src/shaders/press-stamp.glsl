// press-stamp.glsl - emboss + ink-bleed for the Solution scene PressStamp.
//
// Used by PressStamp (HtmlInCanvas wrapper). The DOM child (a screenshot or
// terminal pane) is captured per frame via drawElementImage. We approximate
// emboss via a Sobel-style luminance gradient, then darken edges as the press
// "presses" - u_pressure peaks at the impact frame.
//
// Uniforms:
//   u_dom        - sampler2D, the captured DOM child
//   u_progress   - float [0..1], 0 = pre-stamp, 1 = stamp settled
//   u_pressure   - float [0..1], peaks at impact frame, 0 elsewhere
//   u_resolution - vec2, canvas size in px

precision mediump float;

uniform sampler2D u_dom;
uniform float u_progress;
uniform float u_pressure;
uniform vec2 u_resolution;

varying vec2 v_uv;

float luma(vec3 c) { return dot(c, vec3(0.299, 0.587, 0.114)); }

void main() {
  vec2 px = 1.0 / u_resolution;
  vec3 base = texture2D(u_dom, v_uv).rgb;

  // 3x3 Sobel for edge magnitude.
  float tl = luma(texture2D(u_dom, v_uv + px * vec2(-1.0,  1.0)).rgb);
  float t  = luma(texture2D(u_dom, v_uv + px * vec2( 0.0,  1.0)).rgb);
  float tr = luma(texture2D(u_dom, v_uv + px * vec2( 1.0,  1.0)).rgb);
  float l  = luma(texture2D(u_dom, v_uv + px * vec2(-1.0,  0.0)).rgb);
  float r  = luma(texture2D(u_dom, v_uv + px * vec2( 1.0,  0.0)).rgb);
  float bl = luma(texture2D(u_dom, v_uv + px * vec2(-1.0, -1.0)).rgb);
  float b  = luma(texture2D(u_dom, v_uv + px * vec2( 0.0, -1.0)).rgb);
  float br = luma(texture2D(u_dom, v_uv + px * vec2( 1.0, -1.0)).rgb);

  float gx = -tl - 2.0 * l - bl + tr + 2.0 * r + br;
  float gy = -tl - 2.0 * t - tr + bl + 2.0 * b + br;
  float edge = clamp(sqrt(gx * gx + gy * gy), 0.0, 1.0);

  // Emboss highlight: brighten where the gradient points up-right, darken the
  // opposite. Modulated by progress so the emboss appears with the stamp.
  float embossDir = (gx + gy) * 0.5;
  vec3 embossed = base + embossDir * 0.18 * u_progress;

  // Ink bleed: at peak pressure, edges darken (ink pools along the seams).
  // Bleed radius proportional to pressure.
  vec3 inkBled = embossed - edge * 0.45 * u_pressure;

  // Soft vignette as the paper "settles" - subtle radial darkening at edges.
  vec2 vig = v_uv - 0.5;
  float vignette = 1.0 - dot(vig, vig) * 0.4 * u_progress;

  vec3 final = mix(base, inkBled * vignette, u_progress);

  gl_FragColor = vec4(final, 1.0);
}
