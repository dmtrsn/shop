import type { SVGProps } from "react";

type IconProps = SVGProps<SVGSVGElement>;

function Icon({ children, ...props }: IconProps) {
  return (
    <svg
      aria-hidden="true"
      fill="none"
      focusable="false"
      height="20"
      viewBox="0 0 24 24"
      width="20"
      {...props}
    >
      {children}
    </svg>
  );
}

export function SearchIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <circle cx="11" cy="11" r="7" stroke="currentColor" strokeWidth="1.7" />
      <path d="m16 16 4 4" stroke="currentColor" strokeLinecap="round" strokeWidth="1.7" />
    </Icon>
  );
}

export function HeartIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path
        d="M20.8 5.9a5 5 0 0 0-7.1 0L12 7.6l-1.7-1.7a5 5 0 0 0-7.1 7.1L12 21l8.8-8a5 5 0 0 0 0-7.1Z"
        stroke="currentColor"
        strokeLinejoin="round"
        strokeWidth="1.7"
      />
    </Icon>
  );
}

export function UserIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <circle cx="12" cy="8" r="4" stroke="currentColor" strokeWidth="1.7" />
      <path d="M4.5 21a7.5 7.5 0 0 1 15 0" stroke="currentColor" strokeLinecap="round" strokeWidth="1.7" />
    </Icon>
  );
}

export function CartIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M3 4h2l2 11h10l2-8H6" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth="1.7" />
      <circle cx="9" cy="19" r="1.3" fill="currentColor" />
      <circle cx="17" cy="19" r="1.3" fill="currentColor" />
    </Icon>
  );
}

// Legacy sprite geometry and stroke language: 1.7 stroke, round caps and joins.
function StrokeIcon({ children, ...props }: IconProps) {
  return (
    <Icon stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth="1.7" {...props}>
      {children}
    </Icon>
  );
}

export function GridIcon(props: IconProps) {
  return (
    <StrokeIcon {...props}>
      <rect x="4" y="4" width="6" height="6" rx="1.5" />
      <rect x="14" y="4" width="6" height="6" rx="1.5" />
      <rect x="4" y="14" width="6" height="6" rx="1.5" />
      <rect x="14" y="14" width="6" height="6" rx="1.5" />
    </StrokeIcon>
  );
}

export function HomeIcon(props: IconProps) {
  return <StrokeIcon {...props}><path d="m3.5 11 8.5-7 8.5 7v9H15v-6H9v6H3.5v-9Z" /></StrokeIcon>;
}

export function BagIcon(props: IconProps) {
  return <StrokeIcon {...props}><path d="M5 8.5h14l-1 12H6l-1-12Z" /><path d="M9 9V6.8a3 3 0 0 1 6 0V9" /></StrokeIcon>;
}

export function ReceiptIcon(props: IconProps) {
  return <StrokeIcon {...props}><path d="M6 3h12v18l-3-2-3 2-3-2-3 2V3Z" /><path d="M9 8h6M9 12h6" /></StrokeIcon>;
}

export function CloseIcon(props: IconProps) {
  return <StrokeIcon {...props}><path d="m6 6 12 12M18 6 6 18" /></StrokeIcon>;
}

export function ChevronDownIcon(props: IconProps) {
  return <StrokeIcon {...props}><path d="m7.5 9.5 4.5 4.5 4.5-4.5" /></StrokeIcon>;
}

export function ArrowRightIcon(props: IconProps) {
  return <StrokeIcon {...props}><path d="M5 12h14m-5-5 5 5-5 5" /></StrokeIcon>;
}

export function PlusIcon(props: IconProps) {
  return <StrokeIcon {...props}><path d="M12 5v14M5 12h14" /></StrokeIcon>;
}

export function MinusIcon(props: IconProps) {
  return <StrokeIcon {...props}><path d="M5 12h14" /></StrokeIcon>;
}

export function CheckIcon(props: IconProps) {
  return <StrokeIcon {...props}><path d="m5 12 4.5 4.5L19 7" /></StrokeIcon>;
}

export function FilterIcon(props: IconProps) {
  return <StrokeIcon {...props}><path d="M4 6h16M7 12h10M10 18h4" /></StrokeIcon>;
}

export function TrashIcon(props: IconProps) {
  return <StrokeIcon {...props}><path d="M4 7h16M9 3h6l1 4H8l1-4Zm-2 4 1 14h8l1-14M10 11v6M14 11v6" /></StrokeIcon>;
}

export function InfoIcon(props: IconProps) {
  return <StrokeIcon {...props}><circle cx="12" cy="12" r="9" /><path d="M12 11v6M12 7.5v.2" /></StrokeIcon>;
}

export function RepeatIcon(props: IconProps) {
  return (
    <StrokeIcon {...props}>
      <rect x="8" y="8" width="11" height="12" rx="2" />
      <path d="M16 8V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v9a2 2 0 0 0 2 2h2" />
    </StrokeIcon>
  );
}
