import React from "react";
import { staticFile, Img } from "remotion";

export interface ClaudeDesktopFrameProps {
  src: string;
  width?: number;
  height?: number;
  // Render the chrome (traffic-light buttons + title bar). Default true.
  showChrome?: boolean;
  // The src path is treated as static unless it is already an absolute URL.
  staticAsset?: boolean;
}

export const ClaudeDesktopFrame: React.FC<ClaudeDesktopFrameProps> = ({
  src,
  width,
  height,
  showChrome = true,
  staticAsset = true,
}) => {
  const resolvedSrc = staticAsset ? staticFile(src) : src;

  return (
    <div
      style={{
        width: width ?? "100%",
        height: height ?? "100%",
        backgroundColor: "#1a1a1a",
        borderRadius: 12,
        overflow: "hidden",
        boxShadow:
          "0 24px 48px rgba(0,0,0,0.6), 0 8px 16px rgba(0,0,0,0.4), inset 0 0 0 1px rgba(255,255,255,0.05)",
        display: "flex",
        flexDirection: "column",
      }}
    >
      {showChrome ? (
        <div
          style={{
            height: 36,
            backgroundColor: "#2a2a2a",
            display: "flex",
            alignItems: "center",
            paddingLeft: 16,
            gap: 8,
            flexShrink: 0,
          }}
        >
          <span
            style={{
              width: 12,
              height: 12,
              borderRadius: "50%",
              backgroundColor: "#ff5f57",
            }}
          />
          <span
            style={{
              width: 12,
              height: 12,
              borderRadius: "50%",
              backgroundColor: "#febc2e",
            }}
          />
          <span
            style={{
              width: 12,
              height: 12,
              borderRadius: "50%",
              backgroundColor: "#28c840",
            }}
          />
          <span
            style={{
              flex: 1,
              textAlign: "center",
              color: "#999",
              fontSize: 13,
              fontFamily: "Inter, system-ui, sans-serif",
              marginRight: 60,
            }}
          >
            Claude Desktop
          </span>
        </div>
      ) : null}
      <div style={{ flex: 1, position: "relative" }}>
        <Img
          src={resolvedSrc}
          style={{
            width: "100%",
            height: "100%",
            objectFit: "cover",
            display: "block",
          }}
        />
      </div>
    </div>
  );
};
