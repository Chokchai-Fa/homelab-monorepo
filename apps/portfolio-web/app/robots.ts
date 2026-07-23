import type { MetadataRoute } from "next";
import { siteUrl } from "@/lib/site";

// Emits /robots.txt: allow all crawlers and point them at the sitemap.
export default function robots(): MetadataRoute.Robots {
  return {
    rules: { userAgent: "*", allow: "/" },
    sitemap: `${siteUrl}/sitemap.xml`,
    host: siteUrl,
  };
}
