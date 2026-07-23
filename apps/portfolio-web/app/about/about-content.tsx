"use client";

import React from "react";
import { motion } from "framer-motion";
import { 
  FaJs, 
  FaPython, 
  FaJava, 
  FaReact, 
  FaDocker, 
  FaAws, 
  FaGitlab,
  FaVuejs,
  FaNodeJs,
  FaGithub
} from "react-icons/fa";
import { 
  SiTypescript, 
  SiGo, 
  SiDjango, 
  SiFastapi, 
  SiMongodb, 
  SiPostgresql, 
  SiKubernetes, 
  SiJenkins, 
  SiTerraform, 
  SiApachekafka, 
  SiGooglecloud,
  SiSpringboot,
  SiExpress,
  SiReacttable,
  SiAmazondynamodb,
  SiRedis,
  SiNextdotjs
} from "react-icons/si";
import { GiGraduateCap } from "react-icons/gi";
import { getYearsOfExperience } from "@/lib/utils";

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

const fadeInLeft = {
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

const fadeInRight = {
  initial: {
    opacity: 0,
    x: 60
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

// About data
const about = {
  title: "About me",
  description: `Passionate Software Engineer with over ${getYearsOfExperience()}+ years of experience across financial, insurance, and social network domains. I excel in communication, time management, and rapid project execution while providing valuable consultation to optimize processes, improve solutions, and enhance team performance. I maintain a positive attitude and am always open to feedback to continuously grow and resolve challenges.`,
  info: [
    {
      fieldName: "Name",
      fieldValue: "Chokchai"
    },
    {
      fieldName: "Experience",
      fieldValue: `${getYearsOfExperience()}+ Years`
    },
    {
      fieldName: "Nationality",
      fieldValue: "Thai"
    },
    {
      fieldName: "Languages",
      fieldValue: "Thai, English"
    },
  ]
};

// Education data
const education = {
  icon: <GiGraduateCap />,
  title: "My education",
  description: "Strong academic foundation in Computer Science and Engineering with excellent academic performance.",
  items: [
    {
      institution: "Chulalongkorn University",
      degree: "Master of Science in Computer Science",
      duration: "Jul 2023 - Dec 2025",
      location: "Bangkok, Thailand",
      gpa: "GPA 4.00",
      department: "Computer Engineering"
    },
    {
      institution: "King Mongkut's University of Technology Thonburi",
      degree: "Bachelor of Engineering",
      duration: "Aug 2018 - May 2022",
      location: "Bangkok, Thailand",
      gpa: "GPAX 3.59",
      department: "Electronic and Telecommunication Engineering"
    }
  ]
};

// Skills data
const skills = {
  title: "Technology Stack",
  description: "Comprehensive expertise across modern technologies and frameworks",
  skillList: [
    {
      category: "Programming Languages",
      items: [
        { name: "Go", icon: <SiGo /> },
        { name: "Python", icon: <FaPython /> },
        { name: "JavaScript", icon: <FaJs /> },
        { name: "TypeScript", icon: <SiTypescript /> },
        { name: "Java", icon: <FaJava /> },
        { name: "SQL", icon: <SiPostgresql /> }
      ]
    },
    {
      category: "Frontend Frameworks",
      items: [
        { name: "React", icon: <FaReact /> },
        { name: "React Native", icon: <SiReacttable /> },
        { name: "Vue.js", icon: <FaVuejs /> },
        { name: "Next.js", icon: <SiNextdotjs /> }
      ]
    },
    {
      category: "Backend Frameworks",
      items: [
        { name: "Gin", icon: <SiGo /> },
        { name: "Echo", icon: <SiGo /> },
        { name: "Fiber", icon: <SiGo /> },
        { name: "Django", icon: <SiDjango /> },
        { name: "FastAPI", icon: <SiFastapi /> },
        { name: "Express.js", icon: <SiExpress /> },
        { name: "Spring Boot", icon: <SiSpringboot /> },
        { name: "Node.js", icon: <FaNodeJs /> }
      ]
    },
    {
      category: "Databases",
      items: [
        { name: "PostgreSQL", icon: <SiPostgresql /> },
        { name: "MongoDB", icon: <SiMongodb /> },
        { name: "DynamoDB", icon: <SiAmazondynamodb /> },
        { name: "Redis", icon: <SiRedis /> }
      ]
    },
    {
      category: "Cloud & Infrastructure",
      items: [
        { name: "AWS", icon: <FaAws /> },
        { name: "GCP", icon: <SiGooglecloud /> },
        { name: "Docker", icon: <FaDocker /> },
        { name: "Kubernetes", icon: <SiKubernetes /> },
        { name: "Apache Kafka", icon: <SiApachekafka /> }
      ]
    },
    {
      category: "DevOps & Tools",
      items: [
        { name: "GitHub", icon: <FaGithub /> },
        { name: "Jenkins", icon: <SiJenkins /> },
        { name: "GitLab CI", icon: <FaGitlab /> },
        { name: "Terraform", icon: <SiTerraform /> }
      ]
    }
  ]
};

// Competencies data
const competencies = {
  title: "Key Competencies",
  description: "Core skills that drive my professional excellence",
  items: [
    "Communication", "System Design", "Creativity",
    "Multitasking", "Collaboration", "Database Management",
    "Problem Solving", "Flexibility", "Adaptability",
    "Fast Learning", "Time Management", "Critical Thinking"
  ]
};

const About = (): JSX.Element => {
  return (
    <motion.div
      initial={{ opacity: 0 }}
      animate={{
        opacity: 1,
        transition: { delay: 2.4, duration: 0.4, ease: "easeIn" }
      }}
      className="min-h-screen py-12 xl:py-16"
    >
      <div className="container mx-auto">
        <div className="flex flex-col gap-[80px]">
          
          {/* About Section */}
          <motion.section 
            className="w-full text-center xl:text-left"
            variants={fadeInUp}
            initial="initial"
            whileInView="animate"
            viewport={{ once: true, amount: 0.3 }}
          >
            <div className="flex flex-col gap-[30px]">
              <motion.div 
                className="flex flex-col gap-4"
                variants={fadeInUp}
              >
                <h1 className="h2 text-accent">About Me</h1>
                <div className="w-[100px] h-[2px] bg-accent mx-auto xl:mx-0"></div>
              </motion.div>
              <div className="flex flex-col xl:flex-row gap-[30px] items-center xl:items-start">
                <motion.div 
                  className="flex-1"
                  variants={fadeInLeft}
                  initial="initial"
                  whileInView="animate"
                  viewport={{ once: true, amount: 0.3 }}
                >
                  <p className="text-white/60 text-lg leading-relaxed">
                    {about.description}
                  </p>
                </motion.div>
                <motion.div 
                  className="flex-1 w-full flex justify-center xl:justify-start"
                  variants={fadeInRight}
                  initial="initial"
                  whileInView="animate"
                  viewport={{ once: true, amount: 0.3 }}
                >
                  <motion.ul 
                    className="flex flex-col gap-y-6 max-w-[400px] mx-auto xl:mx-0 w-full items-center xl:items-start"
                    variants={staggerContainer}
                    initial="initial"
                    whileInView="animate"
                    viewport={{ once: true, amount: 0.3 }}
                  >
                    {about.info.map((item, index) => {
                      return (
                        <motion.li
                          key={index}
                          className="flex flex-col items-center xl:flex-row xl:items-center xl:justify-start xl:gap-4 text-center xl:text-left w-full"
                          variants={fadeInUp}
                        >
                          <span className="text-white/60 xl:min-w-[100px]">{item.fieldName}:</span>
                          <span className="text-xl text-accent">{item.fieldValue}</span>
                        </motion.li>
                      );
                    })}
                  </motion.ul>
                </motion.div>
              </div>
            </div>
          </motion.section>

          {/* Education Section */}
          <motion.section 
            className="w-full"
            variants={fadeInUp}
            initial="initial"
            whileInView="animate"
            viewport={{ once: true, amount: 0.3 }}
          >
            <div className="flex flex-col gap-[30px] text-center xl:text-left">
              <motion.div 
                className="flex flex-col gap-4"
                variants={fadeInUp}
              >
                <h2 className="h2 text-accent">Education</h2>
                <div className="w-[100px] h-[2px] bg-accent mx-auto xl:mx-0"></div>
              </motion.div>
              <motion.p 
                className=" text-white/60 mx-auto xl:mx-0"
                variants={fadeInUp}
              >
                {education.description}
              </motion.p>
              <motion.div 
                className="grid grid-cols-1 lg:grid-cols-2 gap-[30px]"
                variants={staggerContainer}
                initial="initial"
                whileInView="animate"
                viewport={{ once: true, amount: 0.2 }}
              >
                {education.items.map((item, index) => {
                  return (
                    <motion.div
                      key={index}
                      className="bg-[#232329] h-auto py-6 px-10 rounded-xl flex flex-col justify-center items-center lg:items-start gap-1 cursor-pointer"
                      variants={scaleIn}
                      whileHover={{ 
                        y: -5,
                        scale: 1.02,
                        boxShadow: "0 10px 25px rgba(0,0,0,0.3)",
                        backgroundColor: "#2a2a30",
                        transition: { 
                          duration: 0.3,
                          ease: "easeOut" 
                        }
                      }}
                      whileTap={{ scale: 0.98 }}
                    >
                      <span className="text-accent">{item.duration}</span>
                      <h3 className="text-xl text-center lg:text-left">
                        {item.degree}
                      </h3>
                      <div className="flex items-center gap-3 lg:justify-start justify-center w-full">
                        <span className="hidden lg:block w-[6px] h-[6px] rounded-full bg-accent"></span>
                        <p className="text-white/60 text-center lg:text-left">{item.institution}</p>
                      </div>
                      <p className="text-white/60 text-sm">{item.location}</p>
                      <p className="text-accent font-semibold">{item.gpa}</p>
                      {item.department && (
                        <p className="text-white/60 text-sm text-center lg:text-left">
                          {item.department}
                        </p>
                      )}
                    </motion.div>
                  );
                })}
              </motion.div>
            </div>
          </motion.section>

          {/* Skills Section */}
          <motion.section 
            className="w-full"
            variants={fadeInUp}
            initial="initial"
            whileInView="animate"
            viewport={{ once: true, amount: 0.2 }}
          >
            <div className="flex flex-col gap-[30px]">
              <motion.div 
                className="flex flex-col gap-4 text-center xl:text-left"
                variants={fadeInUp}
              >
                <h2 className="h2 text-accent">Technology Stack</h2>
                <div className="w-[100px] h-[2px] bg-accent mx-auto xl:mx-0"></div>
                <p className=" text-white/60 mx-auto xl:mx-0">
                  {skills.description}
                </p>
              </motion.div>
              <motion.div 
                className="grid grid-cols-1 md:grid-cols-2 gap-8"
                variants={staggerContainer}
                initial="initial"
                whileInView="animate"
                viewport={{ once: true, amount: 0.2 }}
              >
                {skills.skillList.map((category, categoryIndex) => {
                  return (
                    <motion.div 
                      key={categoryIndex} 
                      className="space-y-4"
                      variants={fadeInUp}
                    >
                      <h4 className="text-xl font-semibold text-accent text-center md:text-left">
                        {category.category}
                      </h4>
                      <motion.div 
                        className="grid grid-cols-2 sm:grid-cols-3 gap-4"
                        variants={staggerContainer}
                      >
                        {category.items.map((skill, skillIndex) => {
                          return (
                            <motion.div
                              key={skillIndex}
                              className="bg-[#232329] h-[80px] rounded-xl flex flex-col justify-center items-center gap-2 group hover:bg-accent transition-all duration-300"
                              variants={scaleIn}
                            >
                              <div className="text-2xl text-accent group-hover:text-primary transition-all duration-300">
                                {skill.icon}
                              </div>
                              <p className="text-xs text-white/60 group-hover:text-primary transition-all duration-300">
                                {skill.name}
                              </p>
                            </motion.div>
                          );
                        })}
                      </motion.div>
                    </motion.div>
                  );
                })}
              </motion.div>
            </div>
          </motion.section>

          {/* Competencies Section */}
          <motion.section 
            className="w-full"
            variants={fadeInUp}
            initial="initial"
            whileInView="animate"
            viewport={{ once: true, amount: 0.2 }}
          >
            <div className="flex flex-col gap-[30px]">
              <motion.div 
                className="flex flex-col gap-4 text-center xl:text-left"
                variants={fadeInUp}
              >
                <h2 className="h2 text-accent">Key Competencies</h2>
                <div className="w-[100px] h-[2px] bg-accent mx-auto xl:mx-0"></div>
                <p className=" text-white/60 mx-auto xl:mx-0">
                  {competencies.description}
                </p>
              </motion.div>
              <motion.div 
                className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4"
                variants={staggerContainer}
                initial="initial"
                whileInView="animate"
                viewport={{ once: true, amount: 0.2 }}
              >
                {competencies.items.map((competency, index) => {
                  return (
                    <motion.div
                      key={index}
                      className="bg-[#232329] h-[60px] rounded-xl flex justify-center items-center group hover:bg-accent transition-all duration-300"
                      variants={scaleIn}
                    >
                      <p className="text-white group-hover:text-primary transition-all duration-300 font-medium">
                        {competency}
                      </p>
                    </motion.div>
                  );
                })}
              </motion.div>
            </div>
          </motion.section>

        </div>
      </div>
    </motion.div>
  );
};

export default About;