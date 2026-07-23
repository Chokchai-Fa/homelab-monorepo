import type { Metadata } from "next";
import { pageMetadata } from "@/lib/site";
import ContactContent from "./contact-content";

export const metadata: Metadata = pageMetadata({
  title: "Contact",
  description:
    "Get in touch with Chokchai Faroongsarng — Solution Engineer based in Bangkok, Thailand. Reach out about roles, collaborations or his projects.",
  path: "/contact",
});

export default function ContactPage(): JSX.Element {
  return <ContactContent />;
}
