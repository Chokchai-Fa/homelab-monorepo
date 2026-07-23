import type { MetadataRoute } from "next";
import { siteUrl } from "@/lib/site";

// Emits /sitemap.xml listing the public routes for search engines.
export default function sitemap(): MetadataRoute.Sitemap {
  const lastModified = new Date();
  const routes: { path: string; priority: number }[] = [
    { path: "", priority: 1 },
    { path: "/about", priority: 0.8 },
    { path: "/work", priority: 0.8 },
    { path: "/contact", priority: 0.6 },
  ];
  return routes.map(({ path, priority }) => ({
    url: `${siteUrl}${path}`,
    lastModified,
    changeFrequency: "monthly",
    priority,
  }));
}
