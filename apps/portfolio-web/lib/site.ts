// Central SEO/site constants so metadata, sitemap, robots, manifest and
// structured data all stay in sync. Override the canonical origin per
// environment with NEXT_PUBLIC_SITE_URL (defaults to the production domain).

import type { Metadata } from "next";

export const siteUrl = (
  process.env.NEXT_PUBLIC_SITE_URL ?? "https://portfolio.chokchai-dev.xyz"
).replace(/\/$/, "");

export const person = {
  name: "Chokchai Faroongsarng",
  nameTh: "โชคชัย ฟ้ารุ่งสาง",
  jobTitle: "Solution Engineer",
  worksFor: "LINE Corporation",
  email: "chokchai.fa@outlook.com",
  location: "Bangkok, Thailand",
} as const;

// Public profiles — used for OpenGraph, and as `sameAs` in the Person schema
// so search engines can connect the site to the same real person.
export const socials = {
  github: "https://github.com/Chokchai-Fa",
  linkedin: "https://www.linkedin.com/in/chokchai-faroongsarng-519957218/",
  facebook: "https://www.facebook.com/Chokchai0770/",
  instagram: "https://www.instagram.com/phukao.fa/",
} as const;

export const site = {
  name: `${person.name} — Portfolio`,
  shortName: "Chokchai",
  title: `${person.name} | Solution Engineer`,
  description:
    "Chokchai Faroongsarng — Solution Engineer at LINE with 3+ years across financial, insurance and social-network domains. Specializing in scalable, high-performance systems, full-stack development, cloud and Kubernetes.",
  keywords: [
    "Chokchai Faroongsarng",
    "โชคชัย ฟ้ารุ่งสาง",
    "Solution Engineer",
    "Software Engineer",
    "Go developer",
    "Kubernetes",
    "microservices",
    "full-stack developer",
    "Bangkok",
    "portfolio",
  ],
  locale: "en_US",
} as const;

// pageMetadata builds a full Metadata object for a sub-page: page-specific
// title/description, a canonical URL, and OpenGraph + Twitter cards that both
// reference the site's generated OG image. (File-based opengraph-image is not
// inherited by child routes that set their own openGraph, so each page links
// it explicitly.)
export function pageMetadata(opts: {
  title: string;
  description: string;
  path: string;
}): Metadata {
  const { title, description, path } = opts;
  const url = `${siteUrl}${path}`;
  const ogTitle = `${title} | ${person.name}`;
  return {
    title,
    description,
    alternates: { canonical: path },
    openGraph: {
      type: "website",
      title: ogTitle,
      description,
      url,
      siteName: site.name,
      locale: site.locale,
      images: ["/opengraph-image"],
    },
    twitter: {
      card: "summary_large_image",
      title: ogTitle,
      description,
      images: ["/opengraph-image"],
    },
  };
}

// JSON-LD structured data injected on every page. Person makes the site
// eligible for rich results / a knowledge panel; WebSite names the site.
export function structuredData() {
  return [
    {
      "@context": "https://schema.org",
      "@type": "Person",
      name: person.name,
      alternateName: person.nameTh,
      url: siteUrl,
      jobTitle: person.jobTitle,
      email: `mailto:${person.email}`,
      worksFor: { "@type": "Organization", name: person.worksFor },
      address: { "@type": "PostalAddress", addressLocality: "Bangkok", addressCountry: "TH" },
      alumniOf: [
        { "@type": "CollegeOrUniversity", name: "Chulalongkorn University" },
        { "@type": "CollegeOrUniversity", name: "King Mongkut's University of Technology Thonburi" },
      ],
      knowsAbout: ["Go", "Kubernetes", "Microservices", "Event-driven architecture", "Cloud infrastructure", "Full-stack development"],
      sameAs: [socials.github, socials.linkedin, socials.facebook, socials.instagram],
    },
    {
      "@context": "https://schema.org",
      "@type": "WebSite",
      name: site.name,
      url: siteUrl,
      inLanguage: "en",
      author: { "@type": "Person", name: person.name },
    },
  ];
}
