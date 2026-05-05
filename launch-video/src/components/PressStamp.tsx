import React from "react";
import { useCurrentFrame, interpolate, Easing } from "remotion";

// HTML-in-canvas press-stamp transition. The wrapper pivots up from below the
// frame, captures the child DOM mid-flight via drawElementImage, applies an
// emboss + ink-bleed shader (press-stamp.glsl), and slams down at impactFrame.
//
// `impactFrame` is local to this component's mount.
//
// Visual contract:
//   t in [0, 0.6]   - paper rises and tilts back, fade-in
//   t in [0.6, 0.8] - tilt forward into the slam
//   t = impact      - peak emboss pressure, ink darkens at edges
//   t in [impact+, end] - settle (subtle bounce, ink bleed relaxes)
//
// LAW: never nest HtmlInCanvas inside another HtmlInCanvas. PressStamp is the
// outermost wrapper for the screenshot or terminal subtree it processes.

export interface PressStampProps {
  impactFrame: number;
  children: React.ReactNode;
  useHtmlInCanvas?: boolean;
}

export const PressStamp: React.FC<PressStampProps> = ({
  impactFrame,
  children,
  useHtmlInCanvas = false,
}) => {
  const frame = useCurrentFrame();

  if (useHtmlInCanvas) {
    // Shader path - activated after Canary host is wired:
    //
    // import { HtmlInCanvas } from "remotion/html-in-canvas";
    // return (
    //   <HtmlInCanvas
    //     onInit={(ctx) => { ctx.loadShader("/shaders/press-stamp.glsl"); }}
    //     onPaint={(ctx, { frame }) => {
    //       const t = frame / impactFrame;
    //       ctx.uniform("u_progress", Math.min(1, t));
    //       ctx.uniform("u_pressure", pressureAt(frame, impactFrame));
    //       ctx.drawElementImage(domNode);
    //       ctx.drawArrays();
    //     }}
    //   >
    //     {children}
    //   </HtmlInCanvas>
    // );
  }

  const t = frame / Math.max(1, impactFrame);

  // Approach: tilt back, then forward into slam, then settle.
  const rotateX = interpolate(
    frame,
    [0, impactFrame * 0.6, impactFrame * 0.95, impactFrame, impactFrame + 12],
    [70, 25, -8, 0, 0],
    { extrapolateLeft: "clamp", extrapolateRight: "clamp", easing: Easing.bezier(0.4, 0, 0.2, 1) },
  );
  const translateY = interpolate(
    frame,
    [0, impactFrame * 0.6, impactFrame, impactFrame + 8],
    [180, 30, -8, 0],
    { extrapolateLeft: "clamp", extrapolateRight: "clamp", easing: Easing.bezier(0.4, 0, 0.2, 1) },
  );
  const scale = interpolate(
    frame,
    [0, impactFrame * 0.6, impactFrame, impactFrame + 6],
    [0.9, 0.96, 1.04, 1],
    { extrapolateLeft: "clamp", extrapolateRight: "clamp" },
  );
  const opacity = interpolate(frame, [0, impactFrame * 0.3], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });

  // Ink-bleed approximation - inner shadow that peaks at impact.
  const pressure = Math.max(0, 1 - Math.abs(frame - impactFrame) / 12);
  const innerShadow = `inset 0 0 ${40 + 60 * pressure}px rgba(0,0,0,${0.15 + 0.35 * pressure})`;
  const dropShadow = `0 ${20 + 12 * pressure}px ${36 + 24 * pressure}px rgba(0,0,0,${0.35 + 0.3 * pressure})`;

  return (
    <div
      style={{
        position: "relative",
        width: "100%",
        height: "100%",
        perspective: 1200,
      }}
    >
      <div
        style={{
          width: "100%",
          height: "100%",
          opacity,
          transform: `translateY(${translateY}px) rotateX(${rotateX}deg) scale(${scale})`,
          transformOrigin: "center bottom",
          boxShadow: `${dropShadow}, ${innerShadow}`,
          borderRadius: 12,
          overflow: "hidden",
          // Subtle paper texture via overlay - replaced by emboss shader on Canary path.
          filter: t < 0.95 ? "saturate(0.92) contrast(1.05)" : "none",
        }}
      >
        {children}
      </div>
    </div>
  );
};
