import { ImageResponse } from "next/og";
import { person, site } from "@/lib/site";

// Dynamically generated 1200×630 social share image (no binary asset to keep
// in the repo). Matches the site's dark theme with the accent colour.
export const runtime = "nodejs";
export const alt = `${person.name} — ${person.jobTitle}`;
export const size = { width: 1200, height: 630 };
export const contentType = "image/png";

export default function OpengraphImage() {
  return new ImageResponse(
    (
      <div
        style={{
          width: "100%",
          height: "100%",
          display: "flex",
          flexDirection: "column",
          justifyContent: "center",
          padding: "80px",
          background: "#1c1c22",
          color: "#ffffff",
          fontFamily: "sans-serif",
        }}
      >
        <div style={{ fontSize: 30, color: "#00ff99", letterSpacing: 4 }}>
          {person.jobTitle.toUpperCase()}
        </div>
        <div style={{ fontSize: 84, fontWeight: 700, marginTop: 12, lineHeight: 1.1 }}>
          {person.name}
        </div>
        <div style={{ fontSize: 34, color: "rgba(255,255,255,0.7)", marginTop: 24, maxWidth: 900 }}>
          {`${person.jobTitle} at ${person.worksFor} · Go · Kubernetes · Cloud`}
        </div>
        <div style={{ display: "flex", marginTop: 48, alignItems: "center" }}>
          <div style={{ width: 56, height: 6, background: "#00ff99" }} />
          <div style={{ fontSize: 26, color: "rgba(255,255,255,0.55)", marginLeft: 20 }}>
            {site.name}
          </div>
        </div>
      </div>
    ),
    { ...size }
  );
}
