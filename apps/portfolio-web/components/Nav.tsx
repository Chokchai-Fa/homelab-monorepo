"use client";

import Link from 'next/link'
import { usePathname } from "next/navigation";
import { LinkUrl } from './type';


const links: LinkUrl[] = [
    {
        name: "home",
        path: "/"
    },
    {
        name: "about",
        path: "/about"
    },
    {
        name: "work",
        path: "/work"
    },
    {
        name: "contact",
        path: "/contact"
    },
];

const Nav = (): JSX.Element => {
    const pathName: string = usePathname();
    return (
        <nav className="flex gap-8">
            {links.map((link, index) => {
                return (
                    <Link href={link.path} key={index} className={`${link.path=== pathName &&
                        "text-accent border-b-2 border-accent"
                    } capitalize font-medium hover:text-accent transition-all`}>
                        {link.name}
                    </Link>
                )
            })}
        </nav>
    )

}

export default Nav;