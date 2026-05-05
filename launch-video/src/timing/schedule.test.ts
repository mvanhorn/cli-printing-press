import { describe, it, expect } from "vitest";
import {
  FPS,
  HOOK_END,
  PROBLEM_END,
  SOLUTION_END,
  PROOF_END,
  CTA_END,
  TOTAL_FRAMES,
  CUTDOWN_START,
  CUTDOWN_END,
  STAMP_1_FRAME,
  STAMP_2_FRAME,
} from "./schedule";

describe("schedule", () => {
  it("scene boundaries are strictly increasing", () => {
    expect(HOOK_END).toBeLessThan(PROBLEM_END);
    expect(PROBLEM_END).toBeLessThan(SOLUTION_END);
    expect(SOLUTION_END).toBeLessThan(PROOF_END);
    expect(PROOF_END).toBeLessThan(CTA_END);
  });

  it("total frames is 45 seconds at 30fps", () => {
    expect(CTA_END).toBe(45 * FPS);
    expect(TOTAL_FRAMES).toBe(45 * 30);
  });

  it("cutdown range is exactly 15 seconds and falls inside the composition", () => {
    expect(CUTDOWN_END - CUTDOWN_START).toBe(15 * FPS);
    expect(CUTDOWN_START).toBeGreaterThanOrEqual(0);
    expect(CUTDOWN_END).toBeLessThanOrEqual(TOTAL_FRAMES);
  });

  it("solution-scene stamp impacts fall inside the solution range", () => {
    expect(STAMP_1_FRAME).toBeGreaterThan(PROBLEM_END);
    expect(STAMP_1_FRAME).toBeLessThan(STAMP_2_FRAME);
    expect(STAMP_2_FRAME).toBeLessThan(SOLUTION_END);
  });
});
