import type { Metadata } from "next";
import { pageMetadata } from "@/lib/site";
import AboutContent from "./about-content";

export const metadata: Metadata = pageMetadata({
  title: "About",
  description:
    "About Chokchai Faroongsarng — Solution Engineer at LINE with experience at Muang Thai Life Assurance and Siam Commercial Bank, an MSc in Computer Science from Chulalongkorn University, and a stack spanning Go, Kubernetes, cloud and full-stack development.",
  path: "/about",
});

export default function AboutPage(): JSX.Element {
  return <AboutContent />;
}
