"use client";

import React from "react";
import { motion } from "framer-motion";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { 
  Mail, 
  MessageSquare, 
  MapPin,
  Send,
  Facebook,
  Instagram,
  Linkedin,
  Github
} from "lucide-react";

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

// Contact information
const contactInfo = [
  {
    icon: <Mail className="h-6 w-6" />,
    title: "Email",
    description: "chokchai.fa@outlook.com",
    link: "mailto:chokchai.fa@outlook.com"
  },
  {
    icon: <MapPin className="h-6 w-6" />,
    title: "Location",
    description: "Bangkok, Thailand",
    link: null
  },
  {
    icon: <MessageSquare className="h-6 w-6" />,
    title: "Let's Connect",
    description: "Available for opportunities",
    link: null
  }
];

// Social links
const socialLinks = [
  {
    name: "Facebook",
    username: "Chokchai Faroongsarng",
    url: "https://www.facebook.com/Chokchai0770/",
    icon: <Facebook className="h-5 w-5" />,
    color: "hover:text-blue-500"
  },
  {
    name: "Instagram", 
    username: "phukao.fa",
    url: "https://www.instagram.com/phukao.fa/",
    icon: <Instagram className="h-5 w-5" />,
    color: "hover:text-pink-500"
  },
  {
    name: "LinkedIn",
    username: "chokchai-faroongsarng",
    url: "https://www.linkedin.com/in/chokchai-faroongsarng-519957218/",
    icon: <Linkedin className="h-5 w-5" />,
    color: "hover:text-blue-600"
  },
  {
    name: "GitHub",
    username: "Chokchai-Fa",
    url: "https://github.com/Chokchai-Fa",
    icon: <Github className="h-5 w-5" />,
    color: "hover:text-gray-400"
  }
];

const Contact = (): JSX.Element => {
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
        <div className="flex flex-col gap-[60px]">
          
          {/* Header Section */}
          <motion.section 
            className="flex flex-col gap-[30px] text-center xl:text-left"
            variants={fadeInUp}
            initial="initial"
            whileInView="animate"
            viewport={{ once: true, amount: 0.3 }}
          >
            <motion.h1 
              className="text-4xl font-bold"
              variants={fadeInUp}
            >
              Let&apos;s <span className="text-accent">Connect</span>
            </motion.h1>
            <motion.p 
              className="text-white/60 max-w-[600px] mx-auto xl:mx-0"
              variants={fadeInUp}
            >
              Ready to collaborate on your next project? I&apos;m always excited to discuss new opportunities,
              innovative ideas, and how we can create something amazing together.
            </motion.p>
          </motion.section>

          <div className="grid grid-cols-1 xl:grid-cols-2 gap-[60px]">
            
            {/* Contact Form */}
            <motion.section
              variants={fadeInUp}
              initial="initial"
              whileInView="animate"
              viewport={{ once: true, amount: 0.3 }}
            >
              <motion.div 
                className="bg-[#27272c] rounded-xl p-8 border border-accent/10 hover:border-accent/30 transition-colors"
                variants={scaleIn}
              >
                <h3 className="text-2xl font-semibold mb-6 flex items-center gap-3">
                  <Send className="h-6 w-6 text-accent" />
                  Send me a message
                </h3>
                
                <form className="space-y-6">
                  <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                    <div>
                      <label className="text-sm text-white/60 mb-2 block">First Name</label>
                      <Input 
                        placeholder="John"
                        className="bg-[#1c1c22] border-white/10 focus:border-accent"
                      />
                    </div>
                    <div>
                      <label className="text-sm text-white/60 mb-2 block">Last Name</label>
                      <Input 
                        placeholder="Doe"
                        className="bg-[#1c1c22] border-white/10 focus:border-accent"
                      />
                    </div>
                  </div>
                  
                  <div>
                    <label className="text-sm text-white/60 mb-2 block">Email Address</label>
                    <Input 
                      type="email"
                      placeholder="john.doe@example.com"
                      className="bg-[#1c1c22] border-white/10 focus:border-accent"
                    />
                  </div>
                  
                  <div>
                    <label className="text-sm text-white/60 mb-2 block">Subject</label>
                    <Input 
                      placeholder="Let's discuss a project"
                      className="bg-[#1c1c22] border-white/10 focus:border-accent"
                    />
                  </div>
                  
                  <div>
                    <label className="text-sm text-white/60 mb-2 block">Message</label>
                    <Textarea 
                      placeholder="Tell me about your project or idea..."
                      className="bg-[#1c1c22] border-white/10 focus:border-accent min-h-[120px] resize-none"
                    />
                  </div>
                  
                  <Button 
                    size="lg" 
                    className="w-full bg-accent hover:bg-accent/90 text-primary font-semibold"
                  >
                    <Send className="h-4 w-4 mr-2" />
                    Send Message
                  </Button>
                </form>
              </motion.div>
            </motion.section>

            {/* Contact Info & Social */}
            <motion.section
              className="space-y-8"
              variants={staggerContainer}
              initial="initial"
              whileInView="animate"
              viewport={{ once: true, amount: 0.3 }}
            >
              
              {/* Contact Information */}
              <motion.div 
                className="space-y-6"
                variants={fadeInUp}
              >
                <h3 className="text-2xl font-semibold mb-6">Contact Information</h3>
                
                <div className="space-y-4">
                  {contactInfo.map((item, index) => (
                    <motion.div
                      key={index}
                      className="bg-[#27272c] rounded-xl p-6 border border-accent/10 hover:border-accent/30 transition-all duration-300 hover:bg-[#2a2a30]"
                      variants={scaleIn}
                    >
                      <div className="flex items-start gap-4">
                        <div className="w-12 h-12 bg-accent/20 rounded-lg flex items-center justify-center text-accent flex-shrink-0">
                          {item.icon}
                        </div>
                        <div className="flex-1 min-w-0">
                          <h4 className="font-semibold text-white mb-1">{item.title}</h4>
                          {item.link ? (
                            <a 
                              href={item.link}
                              className="text-white/60 hover:text-accent transition-colors break-all"
                            >
                              {item.description}
                            </a>
                          ) : (
                            <p className="text-white/60">{item.description}</p>
                          )}
                        </div>
                      </div>
                    </motion.div>
                  ))}
                </div>
              </motion.div>

              {/* Social Links */}
              <motion.div 
                className="space-y-6"
                variants={fadeInUp}
              >
                <h3 className="text-2xl font-semibold mb-6">Follow me</h3>
                
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                  {socialLinks.map((social, index) => (
                    <motion.a
                      key={index}
                      href={social.url}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="bg-[#27272c] rounded-xl p-4 border border-accent/10 hover:border-accent/30 transition-all duration-300 hover:bg-[#2a2a30] group"
                      variants={scaleIn}
                    >
                      <div className="flex items-center gap-3">
                        <div className={`text-white/60 group-hover:text-accent transition-colors ${social.color}`}>
                          {social.icon}
                        </div>
                        <div className="flex-1 min-w-0">
                          <p className="font-medium text-white group-hover:text-accent transition-colors">
                            {social.name}
                          </p>
                          <p className="text-sm text-white/60 truncate">
                            @{social.username}
                          </p>
                        </div>
                      </div>
                    </motion.a>
                  ))}
                </div>
              </motion.div>
              
            </motion.section>
          </div>
        </div>
      </div>
    </motion.div>
  );
};

export default Contact;