import Link from 'next/link'

import Nav from './Nav'
import MobileNav from './MobileNav'

const Header = (): JSX.Element => {
  return (
    <header className='py-8 xl:py-12 text-white'>
      <div className='container mx-auto flex justify-between items-center'>
        {/* logo */}
        <Link href='/' aria-label='Chokchai — home'>
          {/* Brand wordmark, not a page heading (keeps one <h1> per page). */}
          <span className='block text-4xl font-semibold'>
            Chokchai<span className='text-accent'>.</span>
          </span>
        </Link>
        {/* desktop nav */}
        <div className='hidden xl:flex item-center gap-8'>
          <Nav />
        </div>
        {/* mobile nav */}
        <div className='xl:hidden'>
          <MobileNav />
          </div>
      </div>
    </header>
  )
}


export default Header;