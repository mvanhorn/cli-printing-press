import React from "react";
import { useCurrentFrame } from "remotion";

// HTML-in-canvas overlay applying RGB-channel-split + scanline jitter.
// Implementation note: we keep the API shape compatible with `<HtmlInCanvas>`
// from Remotion v4.0.455+, but in v0 we render as a styled wrapper with
// CSS-driven RGB split fallbacks so the project is previewable in regular
// Chrome before HTML-in-canvas-on-Canary is wired. The shader path is
// activated via the `useHtmlInCanvas` flag (set true after the first
// successful Canary render) and runs `glitch.glsl`.
//
// LAW: never nest HtmlInCanvas inside another HtmlInCanvas (Chrome only
// renders the outer wrapper). This component MUST be the outermost wrapper
// for any subtree it processes.

export interface GlitchOverlayProps {
  // 0..1; ramps subtle to peak.
  intensity: number;
  children: React.ReactNode;
  useHtmlInCanvas?: boolean;
}

export const GlitchOverlay: React.FC<GlitchOverlayProps> = ({
  intensity,
  children,
  useHtmlInCanvas = false,
}) => {
  const frame = useCurrentFrame();

  if (useHtmlInCanvas) {
    // When Chrome Canary is the render target, swap in the HtmlInCanvas
    // primitive. Lifecycle: onInit loads `glitch.glsl` once; onPaint runs
    // per frame, updates u_time + u_intensity, then drawElementImage.
    // See src/shaders/glitch.glsl for the shader source.
    //
    // Pseudocode lives here; the v4.0.455 import is added once the host
    // machine has the bundled Canary runtime available.
    //
    // import { HtmlInCanvas } from "remotion/html-in-canvas";
    // return (
    //   <HtmlInCanvas
    //     onInit={(ctx) => { ctx.loadShader("/shaders/glitch.glsl"); }}
    //     onPaint={(ctx, { frame }) => {
    //       ctx.uniform("u_time", frame / 30);
    //       ctx.uniform("u_intensity", intensity);
    //       ctx.drawElementImage(domNode);
    //       ctx.drawArrays();
    //     }}
    //   >
    //     {children}
    //   </HtmlInCanvas>
    // );
  }

  // CSS fallback - good enough for non-Canary preview, kept as the visual
  // contract the shader path must match.
  const offset = Math.sin(frame * 0.4) * 4 * intensity;
  const scanlineY = (frame * 12) % 100;

  return (
    <div style={{ position: "relative", width: "100%", height: "100%" }}>
      {/* Red channel offset */}
      <div
        style={{
          position: "absolute",
          inset: 0,
          transform: `translateX(${offset}px)`,
          mixBlendMode: "screen",
          filter: "drop-shadow(0 0 0 #ff0040)",
          opacity: 0.5 * intensity,
          pointerEvents: "none",
        }}
        aria-hidden
      >
        {children}
      </div>
      {/* Cyan channel offset */}
      <div
        style={{
          position: "absolute",
          inset: 0,
          transform: `translateX(${-offset}px)`,
          mixBlendMode: "screen",
          opacity: 0.5 * intensity,
          pointerEvents: "none",
        }}
        aria-hidden
      >
        {children}
      </div>
      {/* Base */}
      <div style={{ position: "relative", width: "100%", height: "100%" }}>{children}</div>
      {/* Scanline streak */}
      <div
        style={{
          position: "absolute",
          left: 0,
          right: 0,
          top: `${scanlineY}%`,
          height: 2,
          backgroundColor: "rgba(255,255,255,0.15)",
          opacity: intensity,
          pointerEvents: "none",
        }}
        aria-hidden
      />
    </div>
  );
};
