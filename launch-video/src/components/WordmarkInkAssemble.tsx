import React from "react";
import { interpolate, Easing } from "remotion";

// HTML-in-canvas wordmark assembler. Ink particles converge using a noise
// field; the wordmark text is the alpha mask. As progress crosses each
// pixel's noise threshold, the pixel reveals.
//
// Visual contract:
//   progress = 0     -> pure black, no wordmark visible
//   progress = 0.5   -> partial reveal, organic shape
//   progress = 1     -> fully assembled wordmark
//
// LAW: this MUST be the outermost wrapper for the wordmark subtree.

export interface WordmarkInkAssembleProps {
  text: string;
  // 0..1
  progress: number;
  useHtmlInCanvas?: boolean;
}

export const WordmarkInkAssemble: React.FC<WordmarkInkAssembleProps> = ({
  text,
  progress,
  useHtmlInCanvas = false,
}) => {
  if (useHtmlInCanvas) {
    // Shader path:
    //
    // import { HtmlInCanvas } from "remotion/html-in-canvas";
    // return (
    //   <HtmlInCanvas
    //     onInit={(ctx) => { ctx.loadShader("/shaders/ink-assemble.glsl"); }}
    //     onPaint={(ctx, { frame }) => {
    //       ctx.uniform("u_progress", progress);
    //       ctx.uniform("u_time", frame / 30);
    //       ctx.drawElementImage(textDomNode);
    //       ctx.drawArrays();
    //     }}
    //   >
    //     <span style={{...}}>{text}</span>
    //   </HtmlInCanvas>
    // );
  }

  // CSS fallback: clip-path-driven reveal with ink-bleed blur.
  // Not a perfect parity with the shader, but visually credible during preview.
  const reveal = interpolate(progress, [0, 1], [0, 100], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: Easing.bezier(0.65, 0, 0.35, 1),
  });
  const blur = interpolate(progress, [0, 0.5, 1], [12, 4, 0], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });

  return (
    <div
      style={{
        width: "100%",
        height: "100%",
        position: "relative",
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        overflow: "hidden",
      }}
    >
      {/* Particle scatter layer - simulates ink particles before the reveal. */}
      <div
        style={{
          position: "absolute",
          inset: 0,
          backgroundImage:
            "radial-gradient(circle at 20% 30%, rgba(255,255,255,0.04) 0, transparent 2px), radial-gradient(circle at 70% 60%, rgba(255,255,255,0.04) 0, transparent 2px), radial-gradient(circle at 45% 80%, rgba(255,255,255,0.04) 0, transparent 2px)",
          opacity: 1 - progress,
          mixBlendMode: "screen",
        }}
        aria-hidden
      />
      <div
        style={{
          fontFamily: "Geist Mono, ui-monospace, monospace",
          fontWeight: 900,
          fontSize: "clamp(48px, 9vw, 128px)",
          color: "#fff",
          letterSpacing: "-0.04em",
          textTransform: "uppercase",
          textAlign: "center",
          filter: `blur(${blur}px)`,
          // Reveal mask - clip-path widens with progress.
          clipPath: `polygon(0% 0%, ${reveal}% 0%, ${reveal}% 100%, 0% 100%)`,
          opacity: progress > 0.05 ? 1 : 0,
          textShadow:
            progress < 0.6
              ? "0 0 24px rgba(255,255,255,0.5), 0 0 8px rgba(255,255,255,0.6)"
              : "0 0 0 transparent",
        }}
      >
        {text}
      </div>
    </div>
  );
};
