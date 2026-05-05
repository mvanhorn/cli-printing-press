import React from "react";
import { AbsoluteFill, Series } from "remotion";
import {
  HOOK_FRAMES,
  PROBLEM_FRAMES,
  SOLUTION_FRAMES,
  PROOF_FRAMES,
  CTA_FRAMES,
} from "../timing/schedule";
import { Hook } from "../scenes/Hook";
import { Problem } from "../scenes/Problem";
import { Solution } from "../scenes/Solution";
import { Proof } from "../scenes/Proof";
import { CTA } from "../scenes/CTA";

export type AspectRatio = "landscape" | "vertical" | "square";

export const Hero: React.FC<{ aspect: AspectRatio }> = ({ aspect }) => {
  return (
    <AbsoluteFill style={{ backgroundColor: "#0a0a0a" }}>
      <Series>
        <Series.Sequence durationInFrames={HOOK_FRAMES}>
          <Hook aspect={aspect} />
        </Series.Sequence>
        <Series.Sequence durationInFrames={PROBLEM_FRAMES}>
          <Problem aspect={aspect} />
        </Series.Sequence>
        <Series.Sequence durationInFrames={SOLUTION_FRAMES}>
          <Solution aspect={aspect} />
        </Series.Sequence>
        <Series.Sequence durationInFrames={PROOF_FRAMES}>
          <Proof aspect={aspect} />
        </Series.Sequence>
        <Series.Sequence durationInFrames={CTA_FRAMES}>
          <CTA aspect={aspect} />
        </Series.Sequence>
      </Series>
    </AbsoluteFill>
  );
};
