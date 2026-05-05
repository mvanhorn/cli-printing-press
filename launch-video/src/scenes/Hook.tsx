import React from "react";
import { AbsoluteFill, useCurrentFrame, interpolate, Easing } from "remotion";
import { COPY } from "../copy/script";
import { TerminalEmu } from "../components/TerminalEmu";
import { GlitchOverlay } from "../components/GlitchOverlay";
import { HOOK_FRAMES } from "../timing/schedule";
import type { AspectRatio } from "../compositions/Hero";

export const Hook: React.FC<{ aspect: AspectRatio }> = ({ aspect }) => {
  const frame = useCurrentFrame();

  const textOpacity = interpolate(
    frame,
    [20, 30, 75, 85],
    [0, 1, 1, 0],
    { extrapolateLeft: "clamp", extrapolateRight: "clamp", easing: Easing.bezier(0.4, 0, 0.2, 1) },
  );

  // Glitch intensity ramp: subtle at 0, peaking at frame 60, hard cut at 90.
  const glitchIntensity = interpolate(
    frame,
    [0, 60, HOOK_FRAMES],
    [0.05, 0.85, 0.4],
    { extrapolateLeft: "clamp", extrapolateRight: "clamp" },
  );

  const isVertical = aspect === "vertical";
  const cols = isVertical ? 2 : 3;
  const rows = isVertical ? 3 : 2;
  const gap = 16;

  return (
    <AbsoluteFill style={{ backgroundColor: "#000" }}>
      <GlitchOverlay intensity={glitchIntensity}>
        <div
          style={{
            width: "100%",
            height: "100%",
            display: "grid",
            gridTemplateColumns: `repeat(${cols}, 1fr)`,
            gridTemplateRows: `repeat(${rows}, 1fr)`,
            gap,
            padding: 40,
            boxSizing: "border-box",
          }}
        >
          {COPY.hook.terminals.map((t, i) => (
            <TerminalEmu
              key={t.tool}
              prompt="$"
              command={t.command}
              typingStartFrame={i * 4}
              framesPerChar={1}
              fontSize={isVertical ? 16 : 18}
            />
          ))}
        </div>
      </GlitchOverlay>
      <AbsoluteFill
        style={{
          alignItems: "center",
          justifyContent: "center",
          pointerEvents: "none",
        }}
      >
        <div
          style={{
            opacity: textOpacity,
            fontFamily: "Geist Mono, ui-monospace, monospace",
            fontWeight: 800,
            fontSize: isVertical ? 56 : 72,
            color: "#fff",
            textAlign: "center",
            textShadow: "0 0 30px rgba(0,0,0,0.9), 0 0 8px rgba(0,0,0,0.95)",
            letterSpacing: "-0.02em",
            padding: "0 60px",
            mixBlendMode: "normal",
          }}
        >
          {COPY.hook.line}
        </div>
      </AbsoluteFill>
    </AbsoluteFill>
  );
};
