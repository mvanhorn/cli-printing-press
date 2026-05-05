import React from "react";
import {
  AbsoluteFill,
  useCurrentFrame,
  interpolate,
  Easing,
  spring,
  useVideoConfig,
  Sequence,
} from "remotion";
import { COPY } from "../copy/script";
import { ClaudeDesktopFrame } from "../components/ClaudeDesktopFrame";
import type { AspectRatio } from "../compositions/Hero";

const ToastBubble: React.FC<{ text: string; delay: number; x: number; y: number; accent: string }> = ({
  text,
  delay,
  x,
  y,
  accent,
}) => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();
  const localFrame = frame - delay;
  const enter = spring({ frame: localFrame, fps, config: { damping: 14, stiffness: 110 } });
  const opacity = interpolate(localFrame, [0, 6], [0, 1], { extrapolateLeft: "clamp", extrapolateRight: "clamp" });
  if (localFrame < 0) return null;

  return (
    <div
      style={{
        position: "absolute",
        left: `${x}%`,
        top: `${y}%`,
        transform: `translate(-50%, -50%) scale(${enter})`,
        opacity,
        backgroundColor: "#1a0e0e",
        border: `1px solid ${accent}`,
        color: accent,
        padding: "10px 16px",
        borderRadius: 10,
        fontFamily: "Geist Mono, ui-monospace, monospace",
        fontSize: 18,
        boxShadow: `0 12px 32px rgba(0,0,0,0.6), 0 0 24px ${accent}33`,
        whiteSpace: "nowrap",
      }}
    >
      {text}
    </div>
  );
};

const TextFrame: React.FC<{ text: string; emphasised?: boolean }> = ({ text, emphasised }) => {
  const frame = useCurrentFrame();
  const opacity = interpolate(frame, [0, 8, 50, 60], [0, 1, 1, 0], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: Easing.bezier(0.4, 0, 0.2, 1),
  });
  return (
    <div
      style={{
        opacity,
        fontFamily: "Inter, system-ui, sans-serif",
        fontWeight: emphasised ? 800 : 500,
        fontSize: emphasised ? 88 : 64,
        color: emphasised ? "#ff5f57" : "#fff",
        textAlign: "center",
        textShadow: "0 0 24px rgba(0,0,0,0.7)",
        letterSpacing: "-0.02em",
      }}
    >
      {text}
    </div>
  );
};

export const Problem: React.FC<{ aspect: AspectRatio }> = ({ aspect }) => {
  const frame = useCurrentFrame();
  const isVertical = aspect === "vertical";

  // Screenshot gradually pushes back with blur; clutter intensifies into the cut.
  const blur = interpolate(frame, [0, 200, 210], [2, 6, 14], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });
  const screenshotScale = interpolate(frame, [0, 210], [0.62, 0.55], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });

  return (
    <AbsoluteFill style={{ backgroundColor: "#0a0a0a" }}>
      <AbsoluteFill style={{ alignItems: "center", justifyContent: "center", filter: `blur(${blur}px)` }}>
        <div
          style={{
            width: isVertical ? "85%" : "70%",
            aspectRatio: isVertical ? "9 / 14" : "16 / 10",
            transform: `scale(${screenshotScale})`,
          }}
        >
          <ClaudeDesktopFrame src="captures/claude-desktop-printing-press-running.png" />
        </div>
      </AbsoluteFill>

      {COPY.problem.toasts.map((t, i) => {
        const accentColors = ["#ff5f57", "#febc2e", "#ff5f57", "#febc2e"];
        const positions = [
          { x: 22, y: 32 },
          { x: 78, y: 28 },
          { x: 18, y: 70 },
          { x: 82, y: 75 },
        ];
        return (
          <ToastBubble
            key={t}
            text={t}
            delay={20 + i * 18}
            x={positions[i].x}
            y={positions[i].y}
            accent={accentColors[i]}
          />
        );
      })}

      <Sequence from={20} durationInFrames={60}>
        <AbsoluteFill style={{ alignItems: "center", justifyContent: "center" }}>
          <TextFrame text={COPY.problem.text1} emphasised />
        </AbsoluteFill>
      </Sequence>
      <Sequence from={90} durationInFrames={60}>
        <AbsoluteFill style={{ alignItems: "center", justifyContent: "center" }}>
          <TextFrame text={COPY.problem.text2} />
        </AbsoluteFill>
      </Sequence>
      <Sequence from={160} durationInFrames={50}>
        <AbsoluteFill style={{ alignItems: "center", justifyContent: "center" }}>
          <TextFrame text={COPY.problem.text3} />
        </AbsoluteFill>
      </Sequence>
    </AbsoluteFill>
  );
};
