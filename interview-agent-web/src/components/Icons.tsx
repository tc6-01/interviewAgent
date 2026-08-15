import type { SVGProps } from "react";

type IconProps = SVGProps<SVGSVGElement>;

function IconBase({ children, ...props }: IconProps) {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      {...props}
    >
      {children}
    </svg>
  );
}

export const ArrowRightIcon = (props: IconProps) => (
  <IconBase {...props}><path d="M5 12h14M13 6l6 6-6 6" /></IconBase>
);
export const ArrowLeftIcon = (props: IconProps) => (
  <IconBase {...props}><path d="m19 12-14 0M11 18l-6-6 6-6" /></IconBase>
);
export const CheckIcon = (props: IconProps) => (
  <IconBase {...props}><path d="m5 12 4 4L19 6" /></IconBase>
);
export const ChevronDownIcon = (props: IconProps) => (
  <IconBase {...props}><path d="m6 9 6 6 6-6" /></IconBase>
);
export const FileIcon = (props: IconProps) => (
  <IconBase {...props}><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" /><path d="M14 2v6h6M8 13h8M8 17h6" /></IconBase>
);
export const LinkIcon = (props: IconProps) => (
  <IconBase {...props}><path d="M10 13a5 5 0 0 0 7.5.5l2-2a5 5 0 0 0-7-7l-1.1 1.1" /><path d="M14 11a5 5 0 0 0-7.5-.5l-2 2a5 5 0 0 0 7 7l1.1-1.1" /></IconBase>
);
export const UploadIcon = (props: IconProps) => (
  <IconBase {...props}><path d="M12 16V4M7 9l5-5 5 5M5 20h14" /></IconBase>
);
export const SparkIcon = (props: IconProps) => (
  <IconBase {...props}><path d="m12 3 1.1 3.2a6 6 0 0 0 3.7 3.7L20 11l-3.2 1.1a6 6 0 0 0-3.7 3.7L12 19l-1.1-3.2a6 6 0 0 0-3.7-3.7L4 11l3.2-1.1a6 6 0 0 0 3.7-3.7L12 3Z" /></IconBase>
);
export const SendIcon = (props: IconProps) => (
  <IconBase {...props}><path d="m22 2-7 20-4-9-9-4Z" /><path d="M22 2 11 13" /></IconBase>
);
export const ClockIcon = (props: IconProps) => (
  <IconBase {...props}><circle cx="12" cy="12" r="9" /><path d="M12 7v5l3 2" /></IconBase>
);
export const SignalIcon = (props: IconProps) => (
  <IconBase {...props}><path d="M5 12.5a10 10 0 0 1 14 0M8 16a6 6 0 0 1 8 0M11 19.5a2 2 0 0 1 2 0" /></IconBase>
);
export const CloseIcon = (props: IconProps) => (
  <IconBase {...props}><path d="m6 6 12 12M18 6 6 18" /></IconBase>
);
export const DownloadIcon = (props: IconProps) => (
  <IconBase {...props}><path d="M12 3v12M7 10l5 5 5-5M5 21h14" /></IconBase>
);
export const RefreshIcon = (props: IconProps) => (
  <IconBase {...props}><path d="M20 6v5h-5M4 18v-5h5" /><path d="M18.5 9A7 7 0 0 0 6.1 6.1L4 11M5.5 15A7 7 0 0 0 17.9 17.9L20 13" /></IconBase>
);
export const TargetIcon = (props: IconProps) => (
  <IconBase {...props}><circle cx="12" cy="12" r="9" /><circle cx="12" cy="12" r="4" /><path d="m15 9 6-6M17 3h4v4" /></IconBase>
);
export const BookIcon = (props: IconProps) => (
  <IconBase {...props}><path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20V4H6.5A2.5 2.5 0 0 0 4 6.5Z" /><path d="M4 6.5v13" /></IconBase>
);
export const QuoteIcon = (props: IconProps) => (
  <IconBase {...props}><path d="M8 11H4a4 4 0 0 1 4-4v8a4 4 0 0 1-4 4M18 11h-4a4 4 0 0 1 4-4v8a4 4 0 0 1-4 4" /></IconBase>
);
