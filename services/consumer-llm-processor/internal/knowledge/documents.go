// Package knowledge implements retrieval-augmented generation (RAG) for the
// portfolio web chat: a small curated corpus about Chokchai is embedded into
// pgvector, and the most relevant chunks are retrieved and injected into the
// prompt at query time so answers stay grounded and can cite a source.
//
// The whole package is optional. When RAG is disabled or its dependencies
// (embeddings API, pgvector) are unavailable, the caller simply skips
// retrieval and the chat falls back to the persona's prompt-embedded facts.
package knowledge

import (
	"crypto/sha256"
	"encoding/hex"
)

// Document is one retrievable chunk of the corpus. ID is stable so re-ingest
// is idempotent; Source/Title are shown to the model so it can cite where a
// fact came from.
type Document struct {
	ID      string
	Source  string
	Title   string
	Content string
}

// Hash returns a content fingerprint used to skip re-embedding unchanged docs.
func (d Document) Hash() string {
	sum := sha256.Sum256([]byte(d.Source + "\x00" + d.Title + "\x00" + d.Content))
	return hex.EncodeToString(sum[:])
}

// Documents is the curated knowledge base about Chokchai, kept in sync with
// the résumé (apps/portfolio-web/public/assets/resume.pdf) and the portfolio
// site. Chunks are intentionally small and single-topic so retrieval matches
// tightly. Add a chunk when new content lands on the site.
func Documents() []Document {
	return []Document{
		{
			ID:      "identity",
			Source:  "profile",
			Title:   "Who Chokchai is",
			Content: "Chokchai Faroongsarng (Thai: โชคชัย ฟ้ารุ่งสาง) is a software engineer based in Bangkok, Thailand, with over 3 years of experience across social network, financial and insurance domains. Languages: Thai (native) and English (proficient). His Thai name is spelled exactly โชคชัย ฟ้ารุ่งสาง.",
		},
		{
			ID:      "role-line",
			Source:  "résumé",
			Title:   "Solution Engineer at LINE (current)",
			Content: "Since October 2024 Chokchai is a Solution Engineer at LINE Corporation (Thailand). He works with Technical Project Managers and Product Owners to translate business requirements into technical designs, designs scalable architectures supporting millions of users, does full-stack development end to end, and collaborates with QA on testing and validation.",
		},
		{
			ID:      "role-mtl",
			Source:  "résumé",
			Title:   "Senior Software Engineer at Muang Thai Life Assurance",
			Content: "From July 2023 to September 2024 Chokchai was a Senior Software Engineer at Muang Thai Life Assurance. He designed technical solutions for the group insurance core system (software architecture, database design, cloud infrastructure), owned the core policy module (the most critical component), built it on a microservices architecture, and implemented CI/CD pipelines deployed on Kubernetes using Infrastructure as Code.",
		},
		{
			ID:      "role-scb",
			Source:  "résumé",
			Title:   "Software Engineer at Siam Commercial Bank",
			Content: "From June 2022 to June 2023 Chokchai was a Software Engineer at Siam Commercial Bank. He developed mobile banking applications (MaeManee and CBDC) and microservices serving millions of users with a focus on user experience, API services and security, and built CI/CD pipelines automating the build and release of the MaeManee, CBDC and WPlan mobile apps.",
		},
		{
			ID:      "role-toyota",
			Source:  "résumé",
			Title:   "Intern Software Engineer at Toyota Tsusho Denso Electronics",
			Content: "From June to November 2021 Chokchai interned at Toyota Tsusho Denso Electronics (Thailand), applying Rapid Control Prototype technology to the ECU software development process to cut cost and development time, and building a simulation engine model for testing.",
		},
		{
			ID:      "education",
			Source:  "résumé",
			Title:   "Education",
			Content: "Chokchai holds a Master of Science in Computer Science from Chulalongkorn University (July 2023 - December 2025, GPA 4.00) and a Bachelor of Engineering in Electronic and Telecommunication Engineering from King Mongkut's University of Technology Thonburi (August 2018 - May 2022, GPAX 3.59).",
		},
		{
			ID:      "research-gitcofl",
			Source:  "InCIT 2025 paper",
			Title:   "Research: GitCoFL federated learning framework",
			Content: "Chokchai presented the paper \"GitCoFL: Design and Implementation of a Git-Based Federated Learning Framework Utilizing Container-Based Technology\" in the proceedings of the 9th International Conference on Information Technology (InCIT 2025). GitCoFL is a Git-based federated learning framework that uses container technology. This is his research / publication (งานวิจัย).",
		},
		{
			ID:      "skills",
			Source:  "résumé",
			Title:   "Technical skills",
			Content: "Programming: Go, Python, SQL, JavaScript, TypeScript, Java, C, C++. Distributed systems: microservices, event-driven architecture, Apache Kafka. Data stores: relational databases, MongoDB, Redis, DynamoDB, Apache Druid, Apache HBase. Frontend: React, React Native, Vue.js, Next.js. Backend: Gin, Echo, Fiber, Django, FastAPI, Express.js, Spring Boot, Node.js. Containers & infra: Docker, Kubernetes, Terraform, Crossplane. CI/CD: Jenkins, GitHub Actions, GitLab CI, Flux CD, Fastlane, Firebase Distribution. Cloud: AWS, Google Cloud Platform.",
		},
		{
			ID:      "certs-awards",
			Source:  "résumé",
			Title:   "Certifications and awards",
			Content: "Certifications: AWS Certified Solutions Architect - Associate (SAA-C03) exam prep; Complete Web Development (Udemy). Awards: 4th prize in the National Software Contest 2017 (application program category); 3rd prize in the AI Race at Bangkok Maker Faire 2020 for a mini self-driving racing car.",
		},
		{
			ID:      "activities",
			Source:  "résumé",
			Title:   "Activities and leadership",
			Content: "Chokchai took part in the Robo Innovator Challenge 2020 (Software Park Thailand, a self-driving car for logistics) and the DevDisrupt Hackathon Thailand 2020 (an education-focused application). He was a Teaching Assistant at KMUTT and led the Computer Programming tutor team for freshmen and the Digital Circuit and Logic Design tutor team.",
		},
		{
			ID:      "homelab",
			Source:  "this website",
			Title:   "The homelab that runs this chatbot",
			Content: "This very chatbot runs on Chokchai's self-hosted k3s homelab on a Raspberry Pi. It is an event-driven platform: a Next.js portfolio site and Go microservices connected by NATS, deployed with Flux GitOps. Answers come from an LLM difficulty router (Gemini / Groq / OpenRouter) with conversation memory, and this web channel adds retrieval-augmented generation over his résumé so answers are grounded and cite their source.",
		},
	}
}
