import type { Metadata } from "next";
import { pageMetadata } from "@/lib/site";
import WorkContent from "./work-content";

export const metadata: Metadata = pageMetadata({
  title: "Work",
  description:
    "Work experience of Chokchai Faroongsarng — Solution Engineer at LINE, Senior Software Engineer at Muang Thai Life Assurance, and Software Engineer at Siam Commercial Bank, building scalable systems for millions of users.",
  path: "/work",
});

export default function WorkPage(): JSX.Element {
  return <WorkContent />;
}
