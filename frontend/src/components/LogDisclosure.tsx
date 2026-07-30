import { ChevronDown, TerminalSquare } from "lucide-react";
import { useEffect, useState } from "react";

export function LogDisclosure({ log, open: openByParent = false }: { log: string; open?: boolean }) {
  const [open, setOpen] = useState(openByParent);
  useEffect(() => {
    if (openByParent) setOpen(true);
  }, [openByParent]);
  if (!log) return null;
  return (
    <section className={`log-disclosure${open ? " is-open" : ""}`}>
      <button type="button" onClick={() => setOpen((value) => !value)} aria-expanded={open}>
        <TerminalSquare size={17} />
        查看安装日志
        <ChevronDown size={17} />
      </button>
      {open ? <pre>{log}</pre> : null}
    </section>
  );
}
