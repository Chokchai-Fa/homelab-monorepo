import type { MetadataRoute } from "next";
import { site } from "@/lib/site";

// Web app manifest for install/branding and better mobile presentation.
export default function manifest(): MetadataRoute.Manifest {
  return {
    name: site.name,
    short_name: site.shortName,
    description: site.description,
    start_url: "/",
    display: "standalone",
    background_color: "#1c1c22",
    theme_color: "#1c1c22",
    icons: [{ src: "/favicon.ico", sizes: "any", type: "image/x-icon" }],
  };
}
