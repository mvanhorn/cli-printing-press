import React from "react";
import { Composition } from "remotion";
import { Hero } from "./compositions/Hero";
import { TOTAL_FRAMES, FPS } from "./timing/schedule";

export const RemotionRoot: React.FC = () => {
  return (
    <>
      <Composition
        id="Hero"
        component={Hero}
        durationInFrames={TOTAL_FRAMES}
        fps={FPS}
        width={1920}
        height={1080}
        defaultProps={{ aspect: "landscape" as const }}
      />
      <Composition
        id="HeroVertical"
        component={Hero}
        durationInFrames={TOTAL_FRAMES}
        fps={FPS}
        width={1080}
        height={1920}
        defaultProps={{ aspect: "vertical" as const }}
      />
      <Composition
        id="HeroSquare"
        component={Hero}
        durationInFrames={TOTAL_FRAMES}
        fps={FPS}
        width={1080}
        height={1080}
        defaultProps={{ aspect: "square" as const }}
      />
    </>
  );
};
