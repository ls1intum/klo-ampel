import type { ReactNode } from "react";
import { navigate } from "./hooks";

export function Link({ to, className, children }: { to: string; className?: string; children: ReactNode }) {
  return (
    <a
      href={to}
      className={className}
      onClick={(e) => {
        if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return; // let "open in new tab" work
        e.preventDefault();
        navigate(to);
      }}
    >
      {children}
    </a>
  );
}

export function ConnectionBanner() {
  return (
    <div className="banner" role="status">
      Connection lost. Reconnecting…
    </div>
  );
}
