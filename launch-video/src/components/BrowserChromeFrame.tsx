import React from "react";
import { staticFile, Img } from "remotion";

export interface BrowserChromeFrameProps {
  src: string;
  url?: string;
  width?: number;
  height?: number;
  showChrome?: boolean;
  staticAsset?: boolean;
}

export const BrowserChromeFrame: React.FC<BrowserChromeFrameProps> = ({
  src,
  url = "printingpress.dev",
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
        backgroundColor: "#0d0d0d",
        borderRadius: 12,
        overflow: "hidden",
        boxShadow:
          "0 24px 48px rgba(0,0,0,0.6), 0 0 80px rgba(94,106,210,0.15), inset 0 0 0 1px rgba(255,255,255,0.05)",
        display: "flex",
        flexDirection: "column",
      }}
    >
      {showChrome ? (
        <div
          style={{
            height: 44,
            backgroundColor: "#1a1a1a",
            display: "flex",
            alignItems: "center",
            paddingLeft: 16,
            paddingRight: 16,
            gap: 12,
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
          <div
            style={{
              flex: 1,
              height: 28,
              backgroundColor: "#0a0a0a",
              borderRadius: 14,
              padding: "0 16px",
              display: "flex",
              alignItems: "center",
              color: "#aaa",
              fontSize: 13,
              fontFamily: "Inter, system-ui, sans-serif",
            }}
          >
            <span style={{ color: "#666", marginRight: 8 }}>https://</span>
            {url}
          </div>
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
