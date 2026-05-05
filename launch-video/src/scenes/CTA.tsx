import React from "react";
import { AbsoluteFill, useCurrentFrame, interpolate, Easing } from "remotion";
import { COPY } from "../copy/script";
import { WordmarkInkAssemble } from "../components/WordmarkInkAssemble";
import {
  CTA_FRAMES,
  WORDMARK_ASSEMBLE_END,
  URL_FADE_END,
  PROOF_END,
} from "../timing/schedule";
import type { AspectRatio } from "../compositions/Hero";

const localFrame = (absolute: number) => absolute - PROOF_END;

export const CTA: React.FC<{ aspect: AspectRatio }> = ({ aspect }) => {
  const frame = useCurrentFrame();

  const wordmarkProgress = interpolate(
    frame,
    [0, localFrame(WORDMARK_ASSEMBLE_END)],
    [0, 1],
    {
      extrapolateLeft: "clamp",
      extrapolateRight: "clamp",
      easing: Easing.bezier(0.4, 0, 0.2, 1),
    },
  );

  const urlOpacity = interpolate(
    frame,
    [localFrame(WORDMARK_ASSEMBLE_END), localFrame(URL_FADE_END)],
    [0, 1],
    { extrapolateLeft: "clamp", extrapolateRight: "clamp", easing: Easing.bezier(0.4, 0, 0.2, 1) },
  );

  const installOpacity = interpolate(
    frame,
    [localFrame(URL_FADE_END), CTA_FRAMES],
    [0, 1],
    { extrapolateLeft: "clamp", extrapolateRight: "clamp", easing: Easing.bezier(0.4, 0, 0.2, 1) },
  );

  const isVertical = aspect === "vertical";

  return (
    <AbsoluteFill style={{ backgroundColor: "#000" }}>
      <AbsoluteFill
        style={{
          alignItems: "center",
          justifyContent: "center",
          flexDirection: "column",
          gap: 32,
        }}
      >
        <div
          style={{
            width: isVertical ? "90%" : "70%",
            height: isVertical ? "20%" : "30%",
            position: "relative",
          }}
        >
          <WordmarkInkAssemble text={COPY.cta.wordmark} progress={wordmarkProgress} />
        </div>

        <div
          style={{
            opacity: urlOpacity,
            fontFamily: "Geist Mono, ui-monospace, monospace",
            fontSize: isVertical ? 56 : 80,
            fontWeight: 700,
            color: "#7ee787",
            letterSpacing: "-0.02em",
            textShadow: "0 0 40px rgba(126,231,135,0.4)",
          }}
        >
          {COPY.cta.url}
        </div>

        <div
          style={{
            opacity: installOpacity,
            fontFamily: "Geist Mono, ui-monospace, monospace",
            fontSize: isVertical ? 18 : 26,
            color: "#aaa",
            backgroundColor: "rgba(255,255,255,0.04)",
            padding: "14px 24px",
            borderRadius: 6,
            border: "1px solid rgba(255,255,255,0.08)",
            maxWidth: "90%",
            overflow: "hidden",
            whiteSpace: isVertical ? "normal" : "nowrap",
            textOverflow: "ellipsis",
            wordBreak: isVertical ? "break-all" : "normal",
            textAlign: "center",
          }}
        >
          <span style={{ color: "#666" }}>$</span> {COPY.cta.install}
        </div>
      </AbsoluteFill>
    </AbsoluteFill>
  );
};
