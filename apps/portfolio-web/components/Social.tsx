import Link from "next/link";
import { FaGithub, FaLinkedinIn, FaFacebook, FaInstagram } from "react-icons/fa";
import { SocialIcon } from "./type";


interface Props {
    containerStyles: string | undefined
    iconStyles: string | undefined
}

const socials: SocialIcon[] = [
    { icon: <FaGithub />, path: 'https://github.com/Chokchai-Fa' },
    { icon: <FaLinkedinIn />, path: 'https://www.linkedin.com/in/chokchai-faroongsarng-519957218/' },
    { icon: <FaFacebook />, path: 'https://www.facebook.com/Chokchai0770/' },
    { icon: <FaInstagram />, path: 'https://www.instagram.com/phukao.fa/' },
]


const Social = ({ containerStyles, iconStyles }: Props): JSX.Element => {
    return (
        <div className={containerStyles}>
            {socials.map((item, index) => {
                return (
                    <Link 
                        key={index} 
                        href={item.path} 
                        className={iconStyles}
                        target="_blank"
                        rel="noopener noreferrer"
                    >
                        {item.icon}
                    </Link>
                )
            })}
        </div>
    )
}

export default Social;