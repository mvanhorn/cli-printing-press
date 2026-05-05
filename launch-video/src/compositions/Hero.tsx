import React from "react";
import { AbsoluteFill, Audio, Sequence, Series, staticFile } from "remotion";
import {
  HOOK_FRAMES,
  PROBLEM_FRAMES,
  SOLUTION_FRAMES,
  PROOF_FRAMES,
  CTA_FRAMES,
  COMMAND_TYPE_START,
  STAMP_1_FRAME,
  STAMP_2_FRAME,
  PROOF_CUT_1_END,
  PROOF_CUT_2_END,
  CTA_START,
} from "../timing/schedule";
import { Hook } from "../scenes/Hook";
import { Problem } from "../scenes/Problem";
import { Solution } from "../scenes/Solution";
import { Proof } from "../scenes/Proof";
import { CTA } from "../scenes/CTA";

export type AspectRatio = "landscape" | "vertical" | "square";

// Asset existence is a render-time concern. When an asset is missing, Remotion
// will throw at render. The Audio components below are commented out by
// default so the project renders silent until U9 (music + SFX) lands. Toggle
// AUDIO_ENABLED to true once you have placed the licensed track and SFX
// files in assets/audio/.
const AUDIO_ENABLED = false;

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

      {AUDIO_ENABLED ? (
        <>
          {/* Music bed: starts at frame 0 with quiet ambience, drop locked to
              frame 300 (Solution scene start). The track must be pre-edited
              so the drop sample-aligns with t=10s. */}
          <Audio src={staticFile("audio/music.mp3")} volume={0.85} />

          {/* Glitch swell at Hook peak (frame 60). */}
          <Sequence from={60} durationInFrames={36}>
            <Audio src={staticFile("audio/sfx-glitch.wav")} volume={0.7} />
          </Sequence>

          {/* Six keystrokes during command type-out (Solution).
              Type cadence in Solution.tsx is 1.6 frames/char on a 19-char
              command, so the typing window is roughly frames 300-330. We
              fire one keystroke per ~5 frames over that window. */}
          {[0, 5, 10, 15, 20, 25].map((offset) => (
            <Sequence
              key={offset}
              from={COMMAND_TYPE_START + offset}
              durationInFrames={6}
            >
              <Audio src={staticFile("audio/sfx-keystroke.wav")} volume={0.5} />
            </Sequence>
          ))}

          {/* Press-stamp impact #1 (Solution). */}
          <Sequence from={STAMP_1_FRAME - 2} durationInFrames={30}>
            <Audio src={staticFile("audio/sfx-stamp.wav")} volume={0.95} />
          </Sequence>

          {/* Press-stamp impact #2 (Solution). */}
          <Sequence from={STAMP_2_FRAME - 2} durationInFrames={30}>
            <Audio src={staticFile("audio/sfx-stamp.wav")} volume={0.95} />
          </Sequence>

          {/* Subtle hits at Proof cut boundaries. */}
          <Sequence from={PROOF_CUT_1_END - 2} durationInFrames={12}>
            <Audio src={staticFile("audio/sfx-keystroke.wav")} volume={0.4} />
          </Sequence>
          <Sequence from={PROOF_CUT_2_END - 2} durationInFrames={12}>
            <Audio src={staticFile("audio/sfx-keystroke.wav")} volume={0.4} />
          </Sequence>

          {/* Ink-assemble whoosh at CTA start. */}
          <Sequence from={CTA_START - 4} durationInFrames={45}>
            <Audio src={staticFile("audio/sfx-ink-assemble.wav")} volume={0.8} />
          </Sequence>
        </>
      ) : null}
    </AbsoluteFill>
  );
};
