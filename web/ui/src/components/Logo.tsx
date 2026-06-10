// KSailMark is the KSail brand mark — the twin-sail sloop on its navy badge, matching
// docs/src/assets/logo.svg and the desktop app icon — inlined so the SPA chrome (sidebar brand,
// login screen) carries the real product identity on every surface without an asset fetch.
export function KSailMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 512 512" className={className} aria-hidden="true" focusable="false">
      <rect x="24" y="24" width="464" height="464" rx="108" fill="#143150" />
      <g transform="translate(256 258) scale(1.12) translate(-256 -258)">
        <path d="M266 340 L266 142 Q360 224 364 340 Z" fill="#f3f9fd" />
        <path d="M246 340 L246 172 Q178 244 150 340 Z" fill="#2ec4e6" />
        <path
          d="M124 352 C170 338 202 366 256 352 C310 338 342 366 388 352 L388 374 C342 388 310 360 256 374 C202 388 170 360 124 374 Z"
          fill="#2ec4e6"
        />
      </g>
    </svg>
  );
}
