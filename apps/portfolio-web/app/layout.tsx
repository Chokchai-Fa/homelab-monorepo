import type { Metadata } from "next";
import { JetBrains_Mono } from "next/font/google";
import "./globals.css";
import Header from "@/components/Header";
import ChatWidget from "@/components/chat/ChatWidget";

import PageTransition from "@/components/PageTransition";
import StairTransition from "@/components/StairTransition";

const jetbrainsMono = JetBrains_Mono({
  subsets: ["latin"],
  weight: ["100", "200", "300", "400", "500", "600", "700", "800"],
  variable: '--font-jetbrainsMono'
})

export const metadata: Metadata = {
  title: "Chokchai Portfolio",
  description: "Software Engineer specializing in scalable solutions across financial, insurance, and social network domains",
};

export const viewport = {
  width: 'device-width',
  initialScale: 1,
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
