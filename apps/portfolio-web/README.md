# 🚀 Chokchai Portfolio

A modern, responsive portfolio website showcasing software engineering expertise and professional experience. Built with Next.js, TypeScript, and Tailwind CSS.

## ✨ Features

- **🎨 Modern Design**: Clean, professional interface with smooth animations
- **📱 Fully Responsive**: Optimized for all devices and screen sizes
- **⚡ Performance Optimized**: Fast loading with Next.js optimizations
- **🎭 Interactive Animations**: Framer Motion powered scroll animations
- **🌙 Dark Theme**: Professional dark mode design
- **📧 Contact Integration**: Interactive contact form and social media links
- **🔍 SEO Optimized**: Meta tags and structured data for better visibility

## 🛠️ Tech Stack

### Core Technologies
- **Next.js 14.2.11** - React framework with App Router
- **TypeScript** - Type-safe development
- **Tailwind CSS** - Utility-first CSS framework
- **Framer Motion** - Animation library for smooth interactions

### UI Components
- **Radix UI** - Accessible component primitives
- **Lucide React** - Beautiful, customizable icons
- **React Icons** - Popular icon library

### Development Tools
- **ESLint** - Code linting and formatting
- **PostCSS** - CSS processing
- **Docker** - Containerization for deployment

## 📁 Project Structure

```
chokchai-portfolio/
├── app/                    # Next.js App Router pages
│   ├── about/             # About page with education & skills
│   ├── contact/           # Contact page with form & social links
│   ├── work/              # Work experience timeline
│   ├── layout.tsx         # Root layout with navigation
│   └── page.tsx           # Home page
├── components/            # Reusable React components
│   ├── ui/                # Shadcn/ui components
│   ├── Header.tsx         # Navigation header
│   ├── MobileNav.tsx      # Mobile navigation
│   ├── Social.tsx         # Social media links
│   └── ...
├── lib/                   # Utility functions
├── public/               # Static assets
│   └── assets/           # Images, resume, etc.
├── Dockerfile            # Docker configuration
└── .github/workflows/    # CI/CD pipelines
```

## 🚀 Getting Started

### Prerequisites
- Node.js 18+ 
- npm or yarn

### Development Setup

1. **Clone the repository**
   ```bash
   git clone https://github.com/Chokchai-Fa/chokchai-portfolio.git
   cd chokchai-portfolio
   ```

2. **Install dependencies**
   ```bash
   npm install
   ```

3. **Run development server**
   ```bash
   npm run dev
   ```

4. **Open in browser**
   Navigate to [http://localhost:3000](http://localhost:3000)

### Available Scripts

```bash
npm run dev      # Start development server
npm run build    # Build for production
npm run start    # Start production server
npm run lint     # Run ESLint
```

## 🐳 Docker Deployment

### Quick Start with Docker

```bash
# Build the image
docker build -t chokchai-portfolio .

# Run the container
docker run -p 3000:3000 chokchai-portfolio
```

### Docker Compose (Optional)

```yaml
version: '3.8'
services:
  portfolio:
    build: .
    ports:
      - "3000:3000"
    environment:
      - NODE_ENV=production
```

## 🔄 CI/CD Pipeline

### GitHub Actions Workflow

Automated Docker image building and publishing to Docker Hub:

- **Triggers**: Push to main branch, Pull requests
- **Multi-platform**: Builds for `linux/amd64` and `linux/arm64`
- **Caching**: Optimized with GitHub Actions cache
- **Security**: Automatic vulnerability scanning

### Setup Repository Secrets

Configure these secrets in your GitHub repository:

| Secret Name | Description |
|-------------|-------------|
| `DOCKER_USERNAME` | Your Docker Hub username |
| `DOCKER_PASSWORD` | Docker Hub access token |

### Image Tags

Images are tagged with semantic versioning:
- `v1.0.{run_number}-{short_sha}`
- `latest` (for main branch)

## 📄 Pages Overview

### 🏠 Home (`/`)
- Professional introduction
- Key statistics and expertise
- Download CV functionality
- Social media links

### 👨‍💻 About (`/about`)
- Personal background and philosophy
- Educational achievements
- Technology stack (31+ technologies)
- Key competencies

### 💼 Work (`/work`)
- Career overview with metrics
- Detailed experience timeline
- 5 positions across 4+ companies
- Skills and responsibilities

### 📧 Contact (`/contact`)
- Contact information
- Interactive contact form
- Social media profiles
- Professional availability

## 🎨 Key Features Details

### Animations
- Scroll-triggered animations with Framer Motion
- Hover effects on interactive elements
- Smooth page transitions
- Mobile-optimized animations

### Responsive Design
- Mobile-first approach
- Breakpoints: `sm` (640px), `md` (768px), `lg` (1024px), `xl` (1280px)
- Optimized navigation for all screen sizes

### Performance
- Next.js optimization features
- Image optimization ready
- Bundle size optimization
- SEO meta tags

## 🔧 Configuration

### Environment Variables
```bash
NODE_ENV=production          # Production environment
NEXT_TELEMETRY_DISABLED=1   # Disable Next.js telemetry
```

### Next.js Configuration
- Standalone output for Docker
- SWC minification
- Package import optimization
- Base path: `/portfolio`

## 📱 Contact Information

- **Email**: [chokchai.fa@outlook.com](mailto:chokchai.fa@outlook.com)
- **LinkedIn**: [chokchai-faroongsarng](https://www.linkedin.com/in/chokchai-faroongsarng-519957218/)
- **GitHub**: [Chokchai-Fa](https://github.com/Chokchai-Fa)
- **Facebook**: [Chokchai Faroongsarng](https://www.facebook.com/Chokchai0770/)
- **Instagram**: [@phukao.fa](https://www.instagram.com/phukao.fa/)

## 📈 Performance Metrics

- **Lighthouse Score**: 95+ (Performance, Accessibility, Best Practices, SEO)
- **Bundle Size**: ~136kB First Load JS
- **Build Time**: ~4-6 minutes (optimized)
- **Docker Image**: Multi-stage optimized build

## 🤝 Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit changes (`git commit -m 'Add amazing feature'`)
4. Push to branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## 📄 License

This project is personal portfolio software. All rights reserved.

## 🙏 Acknowledgments

- **Next.js Team** - Amazing React framework
- **Vercel** - Deployment platform and optimizations
- **Tailwind CSS** - Utility-first CSS framework
- **Framer Motion** - Smooth animations
- **Radix UI** - Accessible components

---

**Built with ❤️ by Chokchai Faroongsarng | Solution Engineer at LINE Company**
