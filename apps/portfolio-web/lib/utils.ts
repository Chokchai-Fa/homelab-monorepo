import { clsx, type ClassValue } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

// Calculate duration between two dates
export const calculateDuration = (startMonth: number, startYear: number, endMonth?: number, endYear?: number): string => {
  const start = new Date(startYear, startMonth - 1, 1);
  const end = endMonth && endYear ? new Date(endYear, endMonth - 1, 1) : new Date();
  
  let years = end.getFullYear() - start.getFullYear();
  let months = end.getMonth() - start.getMonth();
  
  if (months < 0) {
    years--;
    months += 12;
  }
  
  if (years > 0 && months > 0) {
    return `${years} yr${years > 1 ? 's' : ''} ${months} mo`;
  } else if (years > 0) {
    return `${years} yr${years > 1 ? 's' : ''}`;
  } else {
    return `${months} mo`;
  }
};

// Calculate years of experience from June 1, 2022
export const getYearsOfExperience = (): number => {
  const startDate = new Date(2022, 5, 1); // June 1, 2022
  const now = new Date();
  const years = (now.getTime() - startDate.getTime()) / (365.25 * 24 * 60 * 60 * 1000);
  return Math.floor(years);
};
