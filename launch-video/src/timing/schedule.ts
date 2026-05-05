// Single source of truth for scene boundaries.
// Frame counts at 30fps for the 45-second hero composition.
// Touching any of these constants ripples through every scene; do not duplicate
// the literals in scene files.

export const FPS = 30;

export const HOOK_END = 90; // 3.0s - tab thrash glitch
export const PROBLEM_END = 300; // 10.0s - agent confusion
export const SOLUTION_END = 750; // 25.0s - press prints
export const PROOF_END = 1140; // 38.0s - three CLIs
export const CTA_END = 1350; // 45.0s - wordmark + URL + install

export const TOTAL_FRAMES = CTA_END;

// Scene start frames - derived for clarity at call sites.
export const HOOK_START = 0;
export const PROBLEM_START = HOOK_END;
export const SOLUTION_START = PROBLEM_END;
export const PROOF_START = SOLUTION_END;
export const CTA_START = PROOF_END;

// Per-scene durations, computed.
export const HOOK_FRAMES = HOOK_END - HOOK_START;
export const PROBLEM_FRAMES = PROBLEM_END - PROBLEM_START;
export const SOLUTION_FRAMES = SOLUTION_END - SOLUTION_START;
export const PROOF_FRAMES = PROOF_END - PROOF_START;
export const CTA_FRAMES = CTA_END - CTA_START;

// Solution-scene internal beats (frames are absolute in the composition).
export const COMMAND_TYPE_START = 300; // typing begins
export const COMMAND_TYPE_END = 330; // command fully typed
export const COMMAND_HOLD_END = 345; // [enter] pressed
export const STAMP_1_FRAME = 450; // first press-stamp impact
export const STAMP_2_FRAME = 630; // second press-stamp impact
export const SOLUTION_TEXT_FRAME = 670; // "One command. Every endpoint. Every insight."

// Proof-scene internal cuts.
export const PROOF_CUT_1_END = 880;
export const PROOF_CUT_2_END = 1010;
export const PROOF_CUT_3_END = 1140;

// CTA-scene internal beats.
export const WORDMARK_ASSEMBLE_END = 1290;
export const URL_FADE_END = 1320;

// Cutdown frame range - exported for documentation and future-proofing render scripts.
export const CUTDOWN_START = 270;
export const CUTDOWN_END = 720;
