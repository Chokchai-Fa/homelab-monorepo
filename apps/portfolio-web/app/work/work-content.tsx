"use client";

import { motion } from "framer-motion";
import { 
  Calendar, 
  MapPin, 
  Building2, 
  Code2, 
  Users,
  Zap,
  CheckCircle,
  Briefcase
} from "lucide-react";
import { getYearsOfExperience, calculateDuration } from "@/lib/utils";

// Animation variants
const fadeInUp = {
  initial: {
    opacity: 0,
    y: 60
  },
  animate: {
    opacity: 1,
    y: 0,
    transition: {
      duration: 0.6,
      ease: "easeOut"
    }
  }
};

const staggerContainer = {
  animate: {
    transition: {
      staggerChildren: 0.1
    }
  }
};

const scaleIn = {
  initial: {
    opacity: 0,
    scale: 0.8
  },
  animate: {
    opacity: 1,
    scale: 1,
    transition: {
      duration: 0.5,
      ease: "easeOut"
    }
  }
};

const slideInLeft = {
  initial: {
    opacity: 0,
    x: -60
  },
  animate: {
    opacity: 1,
    x: 0,
    transition: {
      duration: 0.6,
      ease: "easeOut"
    }
  }
};

const workExperience = [
  {
    id: "line",
    company: "LINE Company (Thailand)",
    position: "Solution Engineer", 
    duration: `Oct 2024 - Present · ${calculateDuration(10, 2024)}`,
    location: "Bangkok City, Thailand · Hybrid",
    type: "Full-time",
    description: "As a Solution Engineer at LINE, I specialize in turning complex business requirements into scalable, high-performance solutions. With experience designing large-scale architectures that support up to 50 million users, I thrive at the intersection of business and technology.",
    responsibilities: [
      "Collaborating Effectively alongside Technical Project Managers (TPMs) and Product Owners (POs) to understand and translate business requirements into clear and actionable technical designs.",
      "Design scalable architectures and infrastructures that can efficiently support high traffic loads while ensuring optimal performance.",
      "Technical Specifications Development to prepare comprehensive technical specifications that provide clear guidance for the development process, ensuring all team members are aligned.",
      "Full-Stack Development: Implement complete end-to-end solutions by coding across both frontend and backend systems, ensuring seamless integration and functionality.",
      "Collaborate with quality assurance (QA) teams to conduct thorough testing and validation, guaranteeing the delivery of high-quality software products that meet the specified requirements.",
      "Engage in regular feedback sessions to iterate on designs and implementations, ensuring that solutions remain relevant and effective in meeting evolving business needs."
    ],
    skills: ["Go (Programming Language)", "Vue.js", "Kubernetes", "Docker", "Redis", "Apache Kafka", "Software Design", "Software Infrastructure"],
    color: "from-green-500 to-emerald-600"
  },
  {
    id: "mtl-senior",
    company: "Muang Thai Life Assurance Public Company Limited",
    position: "Senior Software Engineer",
    duration: "Oct 2023 - Oct 2024 · 1 yr 1 mo", 
    location: "Bangkok City, Thailand",
    type: "Full-time",
    description: "As a seasoned Senior Software Engineer at MTL, I drive innovation and excellence in designing and developing cutting-edge solutions for the core group insurance system. Adept at leading cross-functional teams, I contribute strategic insights, mentorship, and technical expertise to ensure the seamless integration of Microservices architecture.",
    responsibilities: [
      "Leading cross-functional teams and providing strategic insights and mentorship",
      "Designing and developing cutting-edge solutions for core group insurance system",
      "Implementing Microservices architecture with seamless integration",
      "Specializing in CI/CD pipeline optimization and Kubernetes deployment",
      "Implementing Infrastructure as Code principles with advanced tooling",
      "Collaborating with key stakeholders and implementing advanced testing strategies",
      "Proactively addressing challenges and pushing technological boundaries"
    ],
    skills: ["Go (Programming Language)", "Docker", "Kubernetes", "NextJS", "SQL", "Amazon DynamoDB", "Software Infrastructure", "Software Design", "Cloud Infrastructure", "Amazon Web Services (AWS)"],
    color: "from-blue-500 to-cyan-600"
  },
  {
    id: "mtl-engineer",
    company: "Muang Thai Life Assurance Public Company Limited", 
    position: "Software Engineer",
    duration: "Jul 2023 - Sep 2023 · 3 mos",
    location: "Bangkok City, Thailand · Hybrid",
    type: "Full-time",
    description: "As a Software Engineer at MTL, I played a pivotal role in designing and developing the core group insurance system with focus on Microservices architecture and modern DevOps practices.",
    responsibilities: [
      "Collaborating with Product Owner to design technical solutions including database and cloud infrastructure",
      "Developing core group insurance system leveraging Microservices architecture",
      "Implementing robust CI/CD pipelines and deploying applications on Kubernetes",
      "Utilizing Infrastructure as Code principles with tools like Terraform",
      "Collaborating with Quality Assurance team and supporting agile testing processes",
      "Swiftly addressing defects that arose in the development cycle"
    ],
    skills: ["Go (Programming Language)", "Docker", "Kubernetes", "NextJS", "SQL", "Amazon DynamoDB"],
    color: "from-purple-500 to-indigo-600"
  },
  {
    id: "scb",
    company: "SCB – Siam Commercial Bank",
    position: "Software Engineer", 
    duration: "Jun 2022 - Jul 2023 · 1 yr 2 mos",
    location: "Bangkok, Bangkok City, Thailand",
    type: "Full-time",
    description: "As a software engineer, I was responsible for developing mobile banking applications that prioritized user experience, interface design, service delivery, and security in the banking industry.",
    responsibilities: [
      "Developing mobile banking applications with focus on user experience and interface design",
      "Ensuring seamless and intuitive experiences tailored to meet specific customer needs",
      "Implementing robust security measures protecting customer data and financial information",
      "Collaborating with cross-functional teams for optimal service delivery",
      "Contributing innovative solutions for evolving customer needs in banking industry"
    ],
    skills: ["Spring Boot", "Docker", "Jenkins", "Node.js", "React Native", "ReactJS"],
    color: "from-yellow-500 to-orange-600"
  },
  {
    id: "toyota",
    company: "TOYOTA TSUSHO NEXTY ELECTRONICS (THAILAND) CO.,LTD",
    position: "Software Engineer",
    duration: "Jun 2021 - Nov 2021 · 6 mos",
    location: "Thailand",
    type: "Internship",
    description: "As an intern software engineer, I successfully applied Rapid Control Prototype (RCP) technology in the ECU software development process by leveraging expertise in Control System Design and Implementation using MATLAB.",
    responsibilities: [
      "Applied Rapid Control Prototype (RCP) technology in ECU software development",
      "Leveraged expertise in Control System Design and Implementation using MATLAB",
      "Worked with cross-functional teams to identify areas for improvement",
      "Implemented RCP to reduce development times and improve testing efficiency",
      "Increased software quality through optimized development processes",
      "Contributed to competitive advantage through process optimization"
    ],
    skills: ["MATLAB", "Control Systems", "RCP Technology"],
    color: "from-red-500 to-pink-600"
  }
];

const Work = (): JSX.Element => {
  return (
    <motion.div
      initial={{ opacity: 0 }}
      animate={{ 
        opacity: 1,
        transition: { delay: 2.4, duration: 0.4, ease: "easeIn" },
      }}
      className="min-h-[80vh] flex items-center justify-center py-12 xl:py-0"
    >
      <div className="container mx-auto">
        <div className="flex flex-col gap-[60px]">
          {/* Overview Section */}
          <motion.section 
            className="flex flex-col gap-[30px] text-center xl:text-left"
            variants={fadeInUp}
            initial="initial"
            whileInView="animate"
            viewport={{ once: true, amount: 0.1 }}
          >
            <motion.h1
              className="text-4xl font-bold"
              variants={fadeInUp}
            >
              Career <span className="text-accent">Overview</span>
            </motion.h1>
            
            <motion.div 
              className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6"
              variants={staggerContainer}
              initial="initial"
              whileInView="animate"
              viewport={{ once: true, amount: 0.1 }}
            >
              <motion.div
                className="bg-[#27272c] rounded-xl p-6 hover:bg-[#2a2a30] transition-colors border border-accent/10 hover:border-accent/30"
                variants={scaleIn}
              >
                <div className="flex items-center gap-3 mb-4">
                  <div className="w-12 h-12 bg-accent/20 rounded-lg flex items-center justify-center">
                    <Zap className="h-6 w-6 text-accent" />
                  </div>
                  <h4 className="text-lg font-semibold">Experience</h4>
                </div>
                <p className="text-2xl font-bold text-accent mb-2">{getYearsOfExperience()}+ Years</p>
                <p className="text-white/60 text-sm">
                  Professional software development experience across multiple industries
                </p>
              </motion.div>

              <motion.div
                className="bg-[#27272c] rounded-xl p-6 hover:bg-[#2a2a30] transition-colors border border-accent/10 hover:border-accent/30"
                variants={scaleIn}
              >
                <div className="flex items-center gap-3 mb-4">
                  <div className="w-12 h-12 bg-accent/20 rounded-lg flex items-center justify-center">
                    <Users className="h-6 w-6 text-accent" />
                  </div>
                  <h4 className="text-lg font-semibold">Scale</h4>
                </div>
                <p className="text-2xl font-bold text-accent mb-2">50M+ Users</p>
                <p className="text-white/60 text-sm">
                  Experience building solutions that serve millions of users
                </p>
              </motion.div>

              <motion.div
                className="bg-[#27272c] rounded-xl p-6 hover:bg-[#2a2a30] transition-colors border border-accent/10 hover:border-accent/30"
                variants={scaleIn}
              >
                <div className="flex items-center gap-3 mb-4">
                  <div className="w-12 h-12 bg-accent/20 rounded-lg flex items-center justify-center">
                    <Code2 className="h-6 w-6 text-accent" />
                  </div>
                  <h4 className="text-lg font-semibold">Expertise</h4>
                </div>
                <p className="text-2xl font-bold text-accent mb-2">Full-Stack</p>
                <p className="text-white/60 text-sm">
                  End-to-end development with modern technologies and practices
                </p>
              </motion.div>
            </motion.div>

            <motion.div
              className="bg-[#27272c] rounded-xl p-8 border border-accent/10 hover:bg-[#2a2a30] transition-colors hover:border-accent/30"
              variants={fadeInUp}
              initial="initial"
              whileInView="animate"
              viewport={{ once: true, amount: 0.1 }}
            >
              <h4 className="text-xl font-semibold mb-6 flex items-center justify-center gap-2">
                <Briefcase className="h-5 w-5 text-accent" />
                Industries & <span className="text-accent">Skills</span>
              </h4>
              <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                <div className="space-y-4 group">
                  <h5 className="font-semibold text-accent group-hover:text-accent/90 transition-colors">Industrial Field</h5>
                  <ul className="space-y-2 text-white/70">
                    <li className="hover:text-white/90 transition-colors hover:translate-x-1 transform duration-200">• Financial & Banking</li>
                    <li className="hover:text-white/90 transition-colors hover:translate-x-1 transform duration-200">• Insurance Systems</li>
                    <li className="hover:text-white/90 transition-colors hover:translate-x-1 transform duration-200">• Social Media Applications</li>
                  </ul>
                </div>
                <div className="space-y-4 group">
                  <h5 className="font-semibold text-accent group-hover:text-accent/90 transition-colors">Skills</h5>
                  <ul className="space-y-2 text-white/70">
                    <li className="hover:text-white/90 transition-colors hover:translate-x-1 transform duration-200">• Scalable System Architecture Design (50M+ Users)</li>
                    <li className="hover:text-white/90 transition-colors hover:translate-x-1 transform duration-200">• Full-Stack Development & Integration</li>
                    <li className="hover:text-white/90 transition-colors hover:translate-x-1 transform duration-200">• Cloud Infrastructure & DevOps</li>
                  </ul>
                </div>
              </div>
            </motion.div>
          </motion.section>

          {/* Experience Section */}
          <motion.section 
            className="flex flex-col gap-[30px] text-center xl:text-left"
            variants={fadeInUp}
            initial="initial"
            whileInView="animate"
            viewport={{ once: true, amount: 0.1 }}
          >
            <motion.h3 
              className="text-4xl font-bold"
              variants={fadeInUp}
            >
              My <span className="text-accent">Journey</span>
            </motion.h3>
            <motion.p 
              className="text-white/60 mx-auto xl:mx-0"
              variants={fadeInUp}
            >
              A comprehensive overview of my professional experience, showcasing 
              my growth from intern to solution engineer at leading technology companies.
            </motion.p>
            
            <div className="relative">
              {/* Timeline line */}
              <div className="absolute left-8 top-0 bottom-0 w-px bg-accent/20 hidden md:block"></div>
              
              <motion.div 
                className="space-y-8"
                variants={staggerContainer}
                initial="initial"
                whileInView="animate"
                viewport={{ once: true, amount: 0.1 }}
              >
                {workExperience.map((job) => (
                  <motion.div
                    key={job.id}
                    className="relative group"
                    variants={slideInLeft}
                  >
                    {/* Timeline dot */}
                    <div className="absolute left-6 top-6 w-4 h-4 bg-accent rounded-full border-4 border-primary z-10 hidden md:block group-hover:scale-125 transition-transform"></div>
                    
                    <div className="bg-[#27272c] rounded-xl p-6 ml-0 md:ml-16 hover:bg-[#2a2a30] transition-colors border border-accent/10 hover:border-accent/30 text-left">
                      <div className="flex flex-col lg:flex-row lg:items-start gap-4">
                        <div className="flex-1">
                          <div className="flex flex-col sm:flex-row sm:items-center gap-2 mb-2">
                            <h4 className="text-xl font-semibold text-accent">{job.position}</h4>
                            <span className="text-xs bg-accent/20 text-accent px-2 py-1 rounded-full w-fit">
                              {job.type}
                            </span>
                          </div>
                          
                          <div className="flex items-start gap-2 mb-2 text-white/60">
                            <Building2 className="h-4 w-4 mt-1.5 flex-shrink-0" />
                            <span className="font-medium">{job.company}</span>
                          </div>
                          
                          <div className="flex flex-col sm:flex-row gap-4 mb-4 text-sm text-white/60">
                            <div className="flex items-center gap-2">
                              <Calendar className="h-4 w-4" />
                              <span>{job.duration}</span>
                            </div>
                            <div className="flex items-center gap-2">
                              <MapPin className="h-4 w-4" />
                              <span>{job.location}</span>
                            </div>
                          </div>
                          
                          <p className="text-white/80 mb-4 leading-relaxed">
                            {job.description}
                          </p>
                          
                          <div className="space-y-2 mb-4">
                            <h5 className="text-sm font-semibold text-accent flex items-center gap-2">
                              <CheckCircle className="h-4 w-4" />
                              Key Responsibilities
                            </h5>
                            <ul className="space-y-1">
                              {job.responsibilities.slice(0, 3).map((resp, idx) => (
                                <li key={idx} className="text-sm text-white/70 flex items-start gap-2">
                                  <div className="w-1.5 h-1.5 bg-accent rounded-full mt-2 flex-shrink-0"></div>
                                  <span>{resp}</span>
                                </li>
                              ))}
                            </ul>
                          </div>
                          
                          <div className="flex flex-wrap gap-2">
                            {job.skills.map((skill, idx) => (
                              <span 
                                key={idx}
                                className="text-xs bg-white/10 text-white/80 px-3 py-1 rounded-full hover:bg-accent/20 transition-colors"
                              >
                                {skill}
                              </span>
                            ))}
                          </div>
                        </div>
                      </div>
                    </div>
                  </motion.div>
                ))}
              </motion.div>
            </div>
          </motion.section>
        </div>
      </div>
    </motion.div>
  );
};

export default Work;