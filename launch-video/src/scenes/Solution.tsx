import React from "react";
import {
  AbsoluteFill,
  useCurrentFrame,
  interpolate,
  Easing,
  Sequence,
} from "remotion";
import { COPY } from "../copy/script";
import { ClaudeDesktopFrame } from "../components/ClaudeDesktopFrame";
import { PressStamp } from "../components/PressStamp";
import { TerminalEmu } from "../components/TerminalEmu";
import {
  COMMAND_TYPE_START,
  STAMP_1_FRAME,
  STAMP_2_FRAME,
  SOLUTION_TEXT_FRAME,
  PROBLEM_END,
} from "../timing/schedule";
import type { AspectRatio } from "../compositions/Hero";

// All frames in this scene are LOCAL to the scene's mount, since we mount
// inside <Series.Sequence>. Convert from absolute composition frames to local.
const localFrame = (absolute: number) => absolute - PROBLEM_END;

const PHASE_LINE_DURATION = 12;

const SolutionCommand: React.FC = () => {
  const frame = useCurrentFrame();
  const startLocal = localFrame(COMMAND_TYPE_START);
  const localF = frame - startLocal;
  const charsTyped = Math.max(0, Math.min(COPY.solution.command.length, Math.floor(localF / 1.6)));
  const visible = COPY.solution.command.slice(0, charsTyped);

  const blink = (frame % 18) < 9;
  const enterPressed = localF > 50;

  return (
    <AbsoluteFill style={{ alignItems: "center", justifyContent: "center" }}>
      <div
        style={{
          fontFamily: "Geist Mono, ui-monospace, monospace",
          fontSize: 92,
          fontWeight: 700,
          color: "#fff",
          letterSpacing: "-0.02em",
          textShadow: "0 0 40px rgba(126,231,135,0.25)",
        }}
      >
        <span style={{ color: "#7ee787" }}>$</span> {visible}
        {!enterPressed && blink ? (
          <span style={{ background: "#fff", marginLeft: 6, padding: "0 8px" }}>&nbsp;</span>
        ) : null}
      </div>
    </AbsoluteFill>
  );
};

const PressPhases: React.FC = () => {
  const frame = useCurrentFrame();
  const start = localFrame(STAMP_1_FRAME) + 8;

  return (
    <AbsoluteFill style={{ alignItems: "center", justifyContent: "center" }}>
      <div
        style={{
          width: "60%",
          fontFamily: "Geist Mono, ui-monospace, monospace",
          fontSize: 28,
          color: "#e6edf3",
          backgroundColor: "rgba(13,17,23,0.85)",
          padding: 32,
          borderRadius: 12,
          border: "1px solid #1f2937",
          backdropFilter: "blur(8px)",
        }}
      >
        {COPY.solution.phases.map((phase, i) => {
          const lineFrame = frame - (start + i * PHASE_LINE_DURATION);
          const opacity = interpolate(lineFrame, [0, 8], [0, 1], {
            extrapolateLeft: "clamp",
            extrapolateRight: "clamp",
          });
          const offset = interpolate(lineFrame, [0, 12], [12, 0], {
            extrapolateLeft: "clamp",
            extrapolateRight: "clamp",
            easing: Easing.bezier(0.4, 0, 0.2, 1),
          });
          return (
            <div
              key={phase}
              style={{
                opacity,
                transform: `translateY(${offset}px)`,
                marginBottom: 8,
                display: "flex",
                gap: 16,
              }}
            >
              <span style={{ color: "#7ee787" }}>{i < 5 ? "✓" : "→"}</span>
              <span>{phase}</span>
            </div>
          );
        })}
      </div>
    </AbsoluteFill>
  );
};

const SolutionHeadline: React.FC = () => {
  const frame = useCurrentFrame();
  const start = localFrame(SOLUTION_TEXT_FRAME);
  const localF = frame - start;
  const opacity = interpolate(localF, [0, 14, 70, 80], [0, 1, 1, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: Easing.bezier(0.4, 0, 0.2, 1),
  });
  const lift = interpolate(localF, [0, 24], [22, 0], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: Easing.bezier(0.4, 0, 0.2, 1),
  });
  return (
    <AbsoluteFill style={{ alignItems: "center", justifyContent: "center" }}>
      <div
        style={{
          opacity,
          transform: `translateY(${lift}px)`,
          fontFamily: "Inter, system-ui, sans-serif",
          fontSize: 88,
          fontWeight: 800,
          color: "#fff",
          letterSpacing: "-0.025em",
          textAlign: "center",
          textShadow: "0 0 40px rgba(0,0,0,0.6)",
          padding: "0 60px",
        }}
      >
        {COPY.solution.headline}
      </div>
    </AbsoluteFill>
  );
};

export const Solution: React.FC<{ aspect: AspectRatio }> = ({ aspect: _aspect }) => {
  // Stamp #1 lands at STAMP_1_FRAME (composition-absolute 450 = local 150).
  // Stamp #2 lands at STAMP_2_FRAME (composition-absolute 630 = local 330).
  // Scene-local schedule (frames):
  //   0-30   black
  //   0-30   command types out
  //   30-150 PressStamp #1 transition (paper pivots up, captures screenshot, slams down)
  //   150-330 phase replay
  //   330-420 PressStamp #2 transition (second screenshot stamps in)
  //   330-450 second screenshot holds, "One command" headline lands
  //   ~370+ headline holds to scene end (local 450 = absolute 750)
  const stamp1Start = localFrame(STAMP_1_FRAME) - 90; // start animation 90 frames before impact
  const stamp1End = localFrame(STAMP_1_FRAME);
  const stamp2Start = localFrame(STAMP_2_FRAME) - 90;
  const stamp2End = localFrame(STAMP_2_FRAME);

  return (
    <AbsoluteFill style={{ backgroundColor: "#000" }}>
      <Sequence from={0} durationInFrames={localFrame(STAMP_1_FRAME) + 30}>
        <SolutionCommand />
      </Sequence>

      <Sequence from={stamp1Start} durationInFrames={120}>
        <AbsoluteFill style={{ alignItems: "center", justifyContent: "center" }}>
          <div style={{ width: "70%", aspectRatio: "16/10" }}>
            <PressStamp impactFrame={stamp1End - stamp1Start}>
              <ClaudeDesktopFrame src="captures/claude-desktop-empty.png" />
            </PressStamp>
          </div>
        </AbsoluteFill>
      </Sequence>

      <Sequence from={localFrame(STAMP_1_FRAME) + 8} durationInFrames={STAMP_2_FRAME - STAMP_1_FRAME - 8}>
        <PressPhases />
      </Sequence>

      <Sequence from={stamp2Start} durationInFrames={120}>
        <AbsoluteFill style={{ alignItems: "center", justifyContent: "center" }}>
          <div style={{ width: "70%", aspectRatio: "16/10" }}>
            <PressStamp impactFrame={stamp2End - stamp2Start}>
              <TerminalEmu
                command="espn-pp-cli live --sport nba"
                typingStartFrame={0}
                framesPerChar={2}
                fontSize={26}
                output={[
                  { type: "comment", text: "# Tonight - 2026-05-04" },
                  { type: "output", text: "  GSW vs DAL  108-104  Q4 1:23  Curry 32p 7a, Doncic 28p 9r" },
                  { type: "output", text: "  BOS vs MIA   95-89  Q4 4:01  Tatum 30p 8r, Adebayo 22p 11r" },
                  { type: "comment", text: "# Injuries (last 24h)" },
                  { type: "output", text: "  Boston: Holiday OUT (foot)" },
                  { type: "output", text: "  Miami: Butler GTD (knee)" },
                ]}
              />
            </PressStamp>
          </div>
        </AbsoluteFill>
      </Sequence>

      <Sequence from={localFrame(SOLUTION_TEXT_FRAME)} durationInFrames={120}>
        <SolutionHeadline />
      </Sequence>
    </AbsoluteFill>
  );
};
