import React from "react";
import {
  AbsoluteFill,
  Series,
  useCurrentFrame,
  interpolate,
  Easing,
} from "remotion";
import { COPY } from "../copy/script";
import { TerminalEmu, type TerminalLine } from "../components/TerminalEmu";
import {
  PROOF_CUT_1_END,
  PROOF_CUT_2_END,
  PROOF_CUT_3_END,
  SOLUTION_END,
} from "../timing/schedule";
import type { AspectRatio } from "../compositions/Hero";

const CUT_1_FRAMES = PROOF_CUT_1_END - SOLUTION_END;
const CUT_2_FRAMES = PROOF_CUT_2_END - PROOF_CUT_1_END;
const CUT_3_FRAMES = PROOF_CUT_3_END - PROOF_CUT_2_END;

const FAKE_OUTPUT: Record<string, TerminalLine[]> = {
  espn: [
    { type: "comment", text: "# NBA  Tue 2026-05-04" },
    { type: "output", text: "  GSW 108  DAL 104  Q4 1:23  L: Curry 32p" },
    { type: "output", text: "  BOS  95  MIA  89  Q4 4:01  L: Tatum 30p" },
    { type: "comment", text: "# Injuries (24h)" },
    { type: "output", text: "  BOS Holiday OUT (foot)" },
    { type: "output", text: "  MIA Butler GTD (knee)" },
  ],
  flightgoat: [
    { type: "comment", text: "# SEA -> JFK  4 pax  Dec 24 - Jan 1" },
    { type: "output", text: "  $612  AS 12  6h 02m  nonstop  [kayak]" },
    { type: "output", text: "  $619  DL  72  6h 11m  nonstop  [google]" },
    { type: "output", text: "  $635  B6 824  6h 18m  nonstop  [kayak]" },
    { type: "output", text: "  $681  UA 462  6h 04m  nonstop  [google]" },
    { type: "comment", text: "# 2 sources, 1 query" },
  ],
  linear: [
    { type: "comment", text: "# Blocked >= 7d (compound query)" },
    { type: "output", text: "  ENG-2104  Webhook retries  blocked by ENG-2098 (12d)" },
    { type: "output", text: "  GROW-441  AB framework  blocked by ENG-2104 (8d)" },
    { type: "output", text: "  PLAT-89   Schema migr.  blocked by INFRA-22 (10d)" },
    { type: "output", text: "  MOB-310   Push registr. blocked by PLAT-89 (10d)" },
    { type: "output", text: "  DOC-58    Quickstart    blocked by MOB-310 (10d)" },
    { type: "comment", text: "# 50ms against local SQLite mirror" },
  ],
};

interface ProofCutProps {
  cut: typeof COPY.proof.cuts[number];
  output: TerminalLine[];
  durationInFrames: number;
}

const ProofCut: React.FC<ProofCutProps> = ({ cut, output, durationInFrames }) => {
  const frame = useCurrentFrame();

  // Lower-third stat slides in around frame 70 of each cut.
  const statStart = 70;
  const statSlide = interpolate(
    frame,
    [statStart, statStart + 18],
    [-100, 0],
    {
      extrapolateLeft: "clamp",
      extrapolateRight: "clamp",
      easing: Easing.bezier(0.4, 0, 0.2, 1),
    },
  );
  const statOpacity = interpolate(
    frame,
    [statStart, statStart + 12, durationInFrames - 8, durationInFrames],
    [0, 1, 1, 0],
    { extrapolateLeft: "clamp", extrapolateRight: "clamp" },
  );

  return (
    <AbsoluteFill style={{ backgroundColor: "#000" }}>
      <AbsoluteFill style={{ padding: 80 }}>
        <TerminalEmu
          command={cut.command}
          typingStartFrame={4}
          framesPerChar={1}
          output={output}
          fontSize={26}
        />
      </AbsoluteFill>
      <AbsoluteFill
        style={{
          alignItems: "flex-start",
          justifyContent: "flex-end",
          padding: 60,
          pointerEvents: "none",
        }}
      >
        <div
          style={{
            transform: `translateX(${statSlide}%)`,
            opacity: statOpacity,
            backgroundColor: cut.accent,
            color: "#000",
            padding: "20px 32px",
            borderRadius: 4,
            fontFamily: "Inter, system-ui, sans-serif",
            display: "flex",
            alignItems: "center",
            gap: 20,
            boxShadow: "0 12px 32px rgba(0,0,0,0.5)",
          }}
        >
          <span style={{ fontSize: 56, fontWeight: 800, letterSpacing: "-0.04em" }}>
            {cut.stat}
          </span>
          <span style={{ fontSize: 22, fontWeight: 500, maxWidth: 360 }}>
            {cut.statLine}
          </span>
        </div>
      </AbsoluteFill>
    </AbsoluteFill>
  );
};

export const Proof: React.FC<{ aspect: AspectRatio }> = ({ aspect: _aspect }) => {
  return (
    <Series>
      <Series.Sequence durationInFrames={CUT_1_FRAMES}>
        <ProofCut
          cut={COPY.proof.cuts[0]}
          output={FAKE_OUTPUT.espn}
          durationInFrames={CUT_1_FRAMES}
        />
      </Series.Sequence>
      <Series.Sequence durationInFrames={CUT_2_FRAMES}>
        <ProofCut
          cut={COPY.proof.cuts[1]}
          output={FAKE_OUTPUT.flightgoat}
          durationInFrames={CUT_2_FRAMES}
        />
      </Series.Sequence>
      <Series.Sequence durationInFrames={CUT_3_FRAMES}>
        <ProofCut
          cut={COPY.proof.cuts[2]}
          output={FAKE_OUTPUT.linear}
          durationInFrames={CUT_3_FRAMES}
        />
      </Series.Sequence>
    </Series>
  );
};
