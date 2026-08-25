import type { AnchorHTMLAttributes, MouseEvent } from "react";

import { api } from "../backend/api";

type Props = Omit<AnchorHTMLAttributes<HTMLAnchorElement>, "href" | "target" | "rel"> & {
  href: string;
};

/** Opens marketplace links in the OS browser instead of the tab-less webview. */
export function MarketplaceExternalLink({ href, onClick, ...props }: Props) {
  const handleClick = (event: MouseEvent<HTMLAnchorElement>) => {
    onClick?.(event);
    if (event.defaultPrevented) return;
    event.preventDefault();
    void api.openMarketplaceExternal(href).catch(() => undefined);
  };

  return <a {...props} href={href} onClick={handleClick} />;
}
