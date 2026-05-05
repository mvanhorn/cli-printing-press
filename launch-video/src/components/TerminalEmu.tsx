import React from "react";
import { useCurrentFrame, interpolate } from "remotion";

export interface TerminalLine {
  type: "prompt" | "output" | "comment";
  text: string;
}

export interface TerminalEmuProps {
  prompt?: string;
  command: string;
  output?: TerminalLine[];
  // Frame at which the command starts typing (relative to this component's mount)
  typingStartFrame?: number;
  // Frames per character; default 2 (~60ms at 30fps)
  framesPerChar?: number;
  // Frames after typing completes before output begins streaming
  outputDelayFrames?: number;
  width?: number;
  height?: number;
  fontSize?: number;
  showCursor?: boolean;
}

const DEFAULT_OUTPUT_LINE_DURATION = 6;

export const TerminalEmu: React.FC<TerminalEmuProps> = ({
  prompt = "$",
  command,
  output = [],
  typingStartFrame = 0,
  framesPerChar = 2,
  outputDelayFrames = 8,
  width,
  height,
  fontSize = 22,
  showCursor = true,
}) => {
  const frame = useCurrentFrame();
  const localFrame = frame - typingStartFrame;

  const charsTyped = Math.max(
    0,
    Math.min(command.length, Math.floor(localFrame / framesPerChar)),
  );
  const typingDoneFrame = command.length * framesPerChar;
  const outputStartFrame = typingDoneFrame + outputDelayFrames;

  const visibleCommand = command.slice(0, charsTyped);
  const typingDone = charsTyped >= command.length;

  const blinkPeriod = 18;
  const cursorVisible = (frame % blinkPeriod) < blinkPeriod / 2;

  const visibleOutput = output.filter(
    (_, idx) => localFrame >= outputStartFrame + idx * DEFAULT_OUTPUT_LINE_DURATION,
  );

  const fadeIn = interpolate(localFrame, [0, 12], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });

  return (
    <div
      style={{
        width: width ?? "100%",
        height: height ?? "100%",
        backgroundColor: "#0d1117",
        border: "1px solid #1f2937",
        borderRadius: 8,
        padding: 24,
        fontFamily: "Geist Mono, ui-monospace, SF Mono, Menlo, monospace",
        fontSize,
        color: "#e6edf3",
        lineHeight: 1.4,
        opacity: fadeIn,
        overflow: "hidden",
      }}
    >
      <div>
        <span style={{ color: "#7ee787" }}>{prompt}</span>{" "}
        <span>{visibleCommand}</span>
        {showCursor && !typingDone && cursorVisible ? (
          <span style={{ backgroundColor: "#e6edf3", marginLeft: 2 }}>
            &nbsp;
          </span>
        ) : null}
      </div>
      {visibleOutput.map((line, idx) => (
        <div
          key={idx}
          style={{
            marginTop: 8,
            color:
              line.type === "comment"
                ? "#8b949e"
                : line.type === "prompt"
                  ? "#7ee787"
                  : "#e6edf3",
          }}
        >
          {line.text}
        </div>
      ))}
    </div>
  );
};
