import { Check, ChevronDown } from "lucide-react";
import { useEffect, useId, useLayoutEffect, useRef, useState } from "react";

interface SelectOption {
  value: string;
  label: string;
  disabled?: boolean;
}

interface SelectFieldProps {
  value: string;
  options: readonly SelectOption[];
  onChange: (value: string) => void;
  /** The accessible name. Required: the trigger shows only the current value. */
  label: string;
  id?: string;
  className?: string;
  /** Rendered inside the trigger before the value, e.g. the theme's icon. */
  leading?: React.ReactNode;
  /** Hides the value text, for the 72px icon rail. */
  compact?: boolean;
}

/**
 * A select whose open list is ours to style.
 *
 * A native <select> cannot do this: the popup is drawn by the operating system
 * and CSS cannot reach it, so on macOS it appeared as a system control in the
 * middle of a UI that looks nothing like one. That is the only reason this
 * exists -- a native select is otherwise the better element, and everything
 * below is the cost of replacing it.
 *
 * Implements the ARIA combobox pattern with aria-activedescendant: focus stays
 * on the trigger while the active option is named by id. Moving real DOM focus
 * into the list is the other legal shape, but it means restoring focus on every
 * close path, which is easier to get wrong.
 *
 * role="combobox" is kept so getByRole("combobox", { name }) still finds this,
 * as it did for the native element. userEvent.selectOptions does not work here;
 * tests click the trigger and then the option.
 */
export function SelectField({ value, options, onChange, label, id, className = "", leading, compact = false }: SelectFieldProps) {
  const generatedId = useId();
  const listId = `${id ?? generatedId}-listbox`;
  const [open, setOpen] = useState(false);
  // Which option the keyboard is on. Separate from `value`: arrowing through the
  // list must not commit a change, or a user browsing options with the keyboard
  // would apply each one in passing.
  const [activeIndex, setActiveIndex] = useState(-1);
  const [dropUp, setDropUp] = useState(false);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const listRef = useRef<HTMLUListElement>(null);
  const typeahead = useRef({ query: "", at: 0 });

  const selectedIndex = options.findIndex((option) => option.value === value);
  const selected = selectedIndex >= 0 ? options[selectedIndex] : undefined;

  const close = (returnFocus: boolean) => {
    setOpen(false);
    setActiveIndex(-1);
    if (returnFocus) triggerRef.current?.focus();
  };

  const commit = (index: number) => {
    const option = options[index];
    if (option && !option.disabled) onChange(option.value);
    close(true);
  };

  // Opens upward when there is not room below. The list is positioned by CSS
  // relative to the trigger; .app-window sets overflow: hidden, so a list that
  // ran past the sidebar's bottom edge would be clipped rather than scrolled.
  useLayoutEffect(() => {
    if (!open) return;
    const trigger = triggerRef.current;
    const list = listRef.current;
    if (!trigger || !list) return;
    const box = trigger.getBoundingClientRect();
    // The margin covers the 5px CSS offset plus room for the shadow, and gives
    // the sidebar pickers -- which sit near the bottom edge -- a reason to flip
    // before the list is merely technically on screen with nothing to spare.
    setDropUp(box.bottom + list.offsetHeight + 16 > window.innerHeight);
  }, [open]);

  useEffect(() => {
    if (!open) return;
    // Pointer down, not click: a click listener would fire after the trigger's
    // own handler had already toggled, reopening what the user meant to close.
    //
    // Not capturing, and bubbling from document: on the capture phase this runs
    // before React's synthetic handlers, so the pointerdown that opened the list
    // was still in flight and closed it again on the way through. The contains()
    // checks are what make it safe to listen on the bubble phase instead.
    const onPointerDown = (event: PointerEvent) => {
      const target = event.target as Node;
      if (!triggerRef.current?.contains(target) && !listRef.current?.contains(target)) close(false);
    };
    // Any scroll or resize invalidates the measured position above.
    const onReflow = () => close(false);
    document.addEventListener("pointerdown", onPointerDown);
    window.addEventListener("resize", onReflow);
    window.addEventListener("scroll", onReflow, true);
    return () => {
      document.removeEventListener("pointerdown", onPointerDown);
      window.removeEventListener("resize", onReflow);
      window.removeEventListener("scroll", onReflow, true);
    };
  }, [open]);

  const openWith = (index: number) => {
    setOpen(true);
    setActiveIndex(index < 0 ? 0 : index);
  };

  const step = (delta: number) => {
    const from = activeIndex < 0 ? selectedIndex : activeIndex;
    const next = Math.min(options.length - 1, Math.max(0, (from < 0 ? 0 : from) + delta));
    setActiveIndex(next);
  };

  const onKeyDown = (event: React.KeyboardEvent) => {
    switch (event.key) {
      case "ArrowDown":
      case "ArrowUp":
        event.preventDefault();
        if (!open) openWith(selectedIndex);
        else step(event.key === "ArrowDown" ? 1 : -1);
        return;
      case "Home":
      case "End":
        if (!open) return;
        event.preventDefault();
        setActiveIndex(event.key === "Home" ? 0 : options.length - 1);
        return;
      case "Enter":
        event.preventDefault();
        if (!open) openWith(selectedIndex);
        else commit(activeIndex < 0 ? selectedIndex : activeIndex);
        return;
      case " ":
        // Space opens, but must not also select: it is the character a typeahead
        // query can contain, so it only commits when the list is already open.
        event.preventDefault();
        if (!open) openWith(selectedIndex);
        else commit(activeIndex < 0 ? selectedIndex : activeIndex);
        return;
      case "Escape":
        if (!open) return;
        event.preventDefault();
        close(true);
        return;
      case "Tab":
        // Tab commits nothing and moves on, matching a native select.
        if (open) close(false);
        return;
      default:
        break;
    }
    if (event.key.length !== 1 || event.metaKey || event.ctrlKey || event.altKey) return;
    // Typeahead: consecutive keystrokes within a second extend the query, so
    // "de" reaches "深色" rather than restarting at every letter.
    const now = Date.now();
    typeahead.current.query = now - typeahead.current.at < 1000 ? typeahead.current.query + event.key : event.key;
    typeahead.current.at = now;
    const query = typeahead.current.query.toLowerCase();
    const hit = options.findIndex((option) => option.label.toLowerCase().startsWith(query));
    if (hit < 0) return;
    if (open) setActiveIndex(hit);
    else onChange(options[hit].value);
  };

  return (
    <div className={`select-field ${className}`.trim()}>
      <button
        ref={triggerRef}
        type="button"
        id={id}
        className="select-field-trigger"
        role="combobox"
        aria-expanded={open}
        aria-controls={listId}
        aria-haspopup="listbox"
        aria-label={label}
        aria-activedescendant={open && activeIndex >= 0 ? `${listId}-${activeIndex}` : undefined}
        onClick={() => (open ? close(false) : openWith(selectedIndex))}
        onKeyDown={onKeyDown}
      >
        {leading}
        {compact ? null : <span className="select-field-value">{selected?.label ?? ""}</span>}
        <ChevronDown size={13} aria-hidden="true" className="select-field-arrow" />
      </button>
      {open ? (
        <ul
          ref={listRef}
          id={listId}
          className={`select-field-list${dropUp ? " is-above" : ""}`}
          role="listbox"
          aria-label={label}
        >
          {options.map((option, index) => (
            <li
              key={option.value}
              id={`${listId}-${index}`}
              role="option"
              aria-selected={option.value === value}
              className={`select-field-option${index === activeIndex ? " is-active" : ""}${option.disabled ? " is-disabled" : ""}`}
              aria-disabled={option.disabled || undefined}
              // Mouse move rather than hover in CSS, so the keyboard's active
              // option and the pointer's cannot disagree.
              onMouseMove={() => setActiveIndex(index)}
              onClick={() => commit(index)}
            >
              <Check size={13} aria-hidden="true" className="select-field-check" />
              <span>{option.label}</span>
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}
