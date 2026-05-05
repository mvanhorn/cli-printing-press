import { Config } from "@remotion/cli/config";

// HTML-in-canvas requires Chrome Canary 149+ with chrome://flags/#canvas-draw-element enabled.
// Remotion v4.0.455+ bundles a compiled Chrome Canary with the flag pre-enabled when --gl=angle
// is set. See README.md for the full setup walkthrough.
Config.setChromiumOpenGlRenderer("angle");

// Output settings - bt709 colorspace for broadcast-safe colour, yuv420p for broad compatibility.
Config.setVideoImageFormat("png");
Config.setPixelFormat("yuv420p");
Config.setCodec("h264");
Config.setCrf(18);
Config.setColorSpace("bt709");

// Concurrency - 2 is conservative for HTML-in-canvas on a Mac. Bump only after verifying
// per-frame render time variance is acceptable on the host machine.
Config.setConcurrency(2);

// Output directory - kept inside the project so render artefacts ship with the project root.
Config.setOutputLocation("out/");

// Static assets live under assets/ (not the Remotion default public/) so the
// directory layout matches the launch-video plan. staticFile() in scenes
// resolves against this directory.
Config.setPublicDir("assets");
