"use client";

import React, { useState } from 'react'
import Link from 'next/link'
import { Sheet, SheetContent, SheetTrigger } from './ui/sheet'
import { CiMenuFries } from 'react-icons/ci'
import { usePathname } from 'next/navigation';
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

const MobileNav = (): JSX.Element => {
    const pathName: string = usePathname();
    const [open, setOpen] = useState(false);

    const handleLinkClick = () => {
        setOpen(false);
    };

    return (
        <Sheet open={open} onOpenChange={setOpen}>
            <SheetTrigger className='flex justify-center items-center'>
                <CiMenuFries className='text-[32px] text-accent' />
            </SheetTrigger>
            
            <SheetContent className='flex flex-col'>
                <div className='mt-32 mb-40 text-center text-2xl'>
                    <Link href="/" onClick={handleLinkClick} aria-label="Chokchai — home">
                        {/* Brand wordmark, not a page heading. */}
                        <span className='block text-4xl font-semibold'>
                            Chokchai<span className='text-accent'>.</span>
                        </span>
                    </Link>
                </div>

                <nav className='flex flex-col justify-center items-center gap-8'>
                    {links.map((link, index) => {
                        return (
                            <Link
                                href={link.path}
                                key={index}
                                onClick={handleLinkClick}
                                className={`${link.path === pathName &&
                                    "text-accent border-b-2 border-accent"} text-xl 
                                    capitalize hover:text-accent transition-all`}
                            >
                                {link.name}
                            </Link>
                        )
                    })}
                </nav>
            </SheetContent>
        </Sheet>
    );
}

export default MobileNav;