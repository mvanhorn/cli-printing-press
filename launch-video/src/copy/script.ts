// All on-screen text strings. Centralised so copy edits diff cleanly without
// touching scene layouts.

export const COPY = {
  hook: {
    line: "Six tools. Six syntaxes. One agent.",
    terminals: [
      { tool: "gh", command: "gh issue list --state open --limit 5" },
      { tool: "stripe", command: "stripe charges list --limit 5" },
      { tool: "linear", command: "linear issues list --assignee me" },
      { tool: "hub", command: "hub pr list" },
      { tool: "slack", command: "slack message list --channel general" },
      { tool: "sendgrid", command: "sendgrid stats global" },
    ],
  },
  problem: {
    text1: "60% of agent tokens",
    text2: "wasted on doc lookups",
    text3: "and wrong syntax",
    toasts: [
      "401 unauthorized",
      "rate limited (429)",
      "retry 3 of 5",
      "stale token",
    ],
  },
  solution: {
    command: "/printing-press espn",
    phases: [
      "Phase 0  Resolve + Reuse",
      "Phase 1  Research Brief",
      "Phase 1.5  Ecosystem Absorb",
      "Phase 2  Generate",
      "Phase 3  Build the GOAT",
      "Phase 4  Shipcheck",
    ],
    headline: "One command. Every endpoint. Every insight.",
  },
  proof: {
    cuts: [
      {
        id: "espn",
        command: "espn-pp-cli live --sport nba",
        stat: "1 call",
        statLine: "Live scores + injuries + lineup news",
        accent: "#00d9ff", // cyan
      },
      {
        id: "flightgoat",
        command:
          "flight-goat sea-jfk --pax 4 --depart 2026-12-24 --return 2027-01-01",
        stat: "2 sources",
        statLine: "Kayak nonstop + Google Flights, one query",
        accent: "#ff8c42", // orange
      },
      {
        id: "linear",
        command: "linear-pp-cli blocked --since 7d",
        stat: "50ms",
        statLine: "Compound queries no API can answer",
        accent: "#5e6ad2", // Linear brand magenta-blue
      },
    ],
  },
  cta: {
    wordmark: "THE PRINTING PRESS",
    url: "printingpress.dev",
    install: "go install github.com/mvanhorn/cli-printing-press/v3/cmd/printing-press@latest",
  },
} as const;
