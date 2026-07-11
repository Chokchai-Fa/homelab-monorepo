import { Button } from "@/components/ui/button";
import { FiDownload } from "react-icons/fi"
import Social from "@/components/Social";
import Photo from "@/components/Photo";
import Link from "next/link";
import { getYearsOfExperience } from "@/lib/utils";

const Home = (): JSX.Element => {
  return (
    <section className="h-full">
      <div className="container mx-auto h-full">
        <div className="flex flex-col xl:flex-row items-center justify-between 
      xl:pt-8 xl:pb-24">
          <div className="text-center xl:text-left order-2 xl:order-none">
            <span className="text-xl">Solution Engineer</span>
            <h1 className="h1">
              Hello I&apos;m <br /> <span className="text-accent">Chokchai Faroongsarng</span>
            </h1>
            <p className="max-w-[500px] mb-9 text-white/80">
              Experienced Software Engineer with {getYearsOfExperience()}+ years of expertise across financial, insurance,
              and social network domains. I specialize in turning complex business requirements into
              scalable, high-performance solutions that serve millions of users. Currently working at
              LINE Company, I excel in full-stack development, cloud infrastructure, and coordinating
              cross-functional teams to deliver innovative technological solutions.
            </p>
            <div className="flex flex-col xl:flex-row items-center gap-8">
              <Link
                href="/assets/resume.pdf"
                download="Chokchai_Faroongsarng_Resume.pdf"
                target="_blank"
              >
                <Button
                  variant="outline"
                  size="lg"
                  className="uppercase flex items-center gap-2"
                >
                  <span>Download CV</span>
                  <FiDownload className="text-xl" />
                </Button>
              </Link>
              <div className="mb-8 xl:mb-0">
                <Social
                  containerStyles="flex gap-6"
                  iconStyles="w-9 h-9 py-2 border border-accent rounded-full flex justify-center 
              item-center text-accent text-base hover:bg-accent hover:text-primary 
              hover:transition-all duration-500"
                />
              </div>
            </div>
          </div>
          <div className="order-1 xl:order-none mb-8 xl:mb-0">
            <Photo />
          </div>
        </div>
      </div>
    </section>
  );
}

export default Home;
