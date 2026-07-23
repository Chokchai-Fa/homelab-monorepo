import type { Metadata } from "next";
import { JetBrains_Mono } from "next/font/google";
import "./globals.css";
import Header from "@/components/Header";
import ChatWidget from "@/components/chat/ChatWidget";

import PageTransition from "@/components/PageTransition";
import StairTransition from "@/components/StairTransition";
import { site, siteUrl, structuredData } from "@/lib/site";

const jetbrainsMono = JetBrains_Mono({
  subsets: ["latin"],
  // Only the weights the UI actually uses: 400 (body), 500 (font-medium),
  // 600 (font-semibold, incl. .h1/.h2/.h3), 700 (font-bold). Dropping the
  // unused 100/200/300/800 halves the font payload.
  weight: ["400", "500", "600", "700"],
  display: 'swap',
  variable: '--font-jetbrainsMono'
})

export const metadata: Metadata = {
  metadataBase: new URL(siteUrl),
  title: {
    default: site.title,
    // Per-page titles render as "About | Chokchai Faroongsarng".
    template: `%s | ${site.shortName === "Chokchai" ? "Chokchai Faroongsarng" : site.shortName}`,
  },
  description: site.description,
  keywords: [...site.keywords],
  authors: [{ name: "Chokchai Faroongsarng", url: siteUrl }],
  creator: "Chokchai Faroongsarng",
  applicationName: site.name,
  alternates: { canonical: "/" },
  openGraph: {
    type: "website",
    url: siteUrl,
    siteName: site.name,
    title: site.title,
    description: site.description,
    locale: site.locale,
  },
  twitter: {
    card: "summary_large_image",
    title: site.title,
    description: site.description,
  },
  robots: {
    index: true,
    follow: true,
    googleBot: { index: true, follow: true, "max-image-preview": "large", "max-snippet": -1 },
  },
  // Set GOOGLE_SITE_VERIFICATION in the deployment to auto-emit the Search
  // Console verification meta tag (no code change needed).
  verification: process.env.GOOGLE_SITE_VERIFICATION
    ? { google: process.env.GOOGLE_SITE_VERIFICATION }
    : undefined,
};

export const viewport = {
  width: 'device-width',
  initialScale: 1,
  themeColor: "#1c1c22",
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en">
      <body
        className={jetbrainsMono.variable}
      >
        {/* Structured data (Person + WebSite) for rich results. */}
        <script
          type="application/ld+json"
          dangerouslySetInnerHTML={{ __html: JSON.stringify(structuredData()) }}
        />
        <Header />
        <StairTransition />
        <PageTransition>
          {children}
        </PageTransition>
        <ChatWidget />
      </body>
    </html>
  );
}
