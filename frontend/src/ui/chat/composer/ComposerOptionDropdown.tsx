import type { JSX } from "preact";
import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import { ChevronDown } from "../../primitives/icons";

export interface ComposerOption<T extends string> {
  value: T;
  label: string;
}

export function ComposerOptionDropdown<T extends string>({
  label,
  value,
  options,
  disabled = false,
  Icon,
  onChange,
}: {
  label: string;
  value: T;
  options: readonly ComposerOption<T>[];
  disabled?: boolean;
  Icon: (props: JSX.SVGAttributes<SVGSVGElement>) => JSX.Element;
  onChange: (value: T) => void;
}) {
  const [open, setOpen] = useState(false);
  const [menuAlignment, setMenuAlignment] = useState<"start" | "end">("start");
  const rootRef = useRef<HTMLDivElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const selected = options.find((option) => option.value === value) || options[0];

  useEffect(() => {
    if (!open) return;
    function closeOnOutsideClick(event: MouseEvent) {
      const target = event.target as Node | null;
      if (target && !rootRef.current?.contains(target)) setOpen(false);
    }
    window.addEventListener("mousedown", closeOnOutsideClick);
    return () => window.removeEventListener("mousedown", closeOnOutsideClick);
  }, [open]);

  useLayoutEffect(() => {
    if (!open) return;

    function placeMenuWithinViewport() {
      const rootBounds = rootRef.current?.getBoundingClientRect();
      const menuWidth = menuRef.current?.offsetWidth;
      if (!rootBounds || !menuWidth) return;

      const viewportGutter = 12;
      const availableWidthAfterTrigger = window.innerWidth - viewportGutter - rootBounds.left;
      setMenuAlignment(menuWidth > availableWidthAfterTrigger ? "end" : "start");
    }

    placeMenuWithinViewport();
    window.addEventListener("resize", placeMenuWithinViewport);
    return () => window.removeEventListener("resize", placeMenuWithinViewport);
  }, [open]);

  function pick(nextValue: T) {
    setOpen(false);
    if (nextValue !== value) onChange(nextValue);
  }

  return (
    <div ref={rootRef} class="codex-option-control relative flex-none">
      <button
        type="button"
        onClick={() => setOpen((current) => !current)}
        class={`composer-pill ${open ? "composer-pill-active" : ""}`}
        disabled={disabled}
        title={`${label}: ${selected?.label || "Auto"}`}
        aria-haspopup="listbox"
        aria-expanded={open}
      >
        <Icon class="h-4 w-4 flex-none text-ink-400" aria-hidden="true" />
        <span class="sr-only">{label}</span>
        <span class="max-w-[6rem] truncate font-semibold text-ink-100">{selected?.label || "Auto"}</span>
        <ChevronDown class="h-4 w-4 flex-none text-ink-400" aria-hidden="true" />
      </button>

      {open && (
        <div
          ref={menuRef}
          class={`popover-surface theme-menu-surface absolute bottom-full z-40 mb-1.5 w-[min(10rem,calc(100vw-1.5rem))] p-1
                  ${menuAlignment === "end" ? "right-0" : "left-0"}`}
          role="listbox"
        >
          {options.map((option) => {
            const active = option.value === value;
            return (
              <button
                key={option.value || "auto"}
                type="button"
                onClick={() => pick(option.value)}
                class={`w-full rounded-md px-2.5 py-2 text-left text-[12px] font-medium transition
                        ${active ? "bg-accent-blue/[0.14] text-accent-blue" : "text-ink-100 hover:bg-white/[0.07]"}`}
                role="option"
                aria-selected={active}
              >
                {option.label}
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
}
