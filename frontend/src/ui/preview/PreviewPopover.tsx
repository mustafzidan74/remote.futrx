import type { ComponentChildren, RefObject } from "preact";
import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import { createPortal } from "preact/compat";
import { X } from "../primitives/icons";

const PANEL_WIDTH = 304;
const GUTTER = 8;
const MAX_HEIGHT = 380;

interface PanelBox {
  left: number;
  width: number;
  maxHeight: number;
  top?: number;
  bottom?: number;
}

/**
 * A popover pinned to its trigger with fixed positioning. The sidebar and the
 * chat header both scroll and clip their overflow, so an absolutely positioned
 * panel would be cut off in either place.
 */
export function PreviewPopover({
  anchorRef,
  title,
  onClose,
  children,
}: {
  anchorRef: RefObject<HTMLElement>;
  title: string;
  onClose: () => void;
  children: ComponentChildren;
}) {
  const panelRef = useRef<HTMLDivElement>(null);
  const [box, setBox] = useState<PanelBox | null>(null);

  useLayoutEffect(() => {
    function place() {
      const anchor = anchorRef.current?.getBoundingClientRect();
      if (!anchor) return;
      const width = Math.min(PANEL_WIDTH, window.innerWidth - 2 * GUTTER);
      const left = Math.max(
        GUTTER,
        Math.min(anchor.left, window.innerWidth - width - GUTTER),
      );
      const roomBelow = window.innerHeight - anchor.bottom - GUTTER;
      const roomAbove = anchor.top - GUTTER;
      if (roomBelow < 180 && roomAbove > roomBelow) {
        setBox({
          left,
          width,
          maxHeight: Math.min(MAX_HEIGHT, roomAbove),
          bottom: window.innerHeight - anchor.top + 6,
        });
        return;
      }
      setBox({
        left,
        width,
        maxHeight: Math.min(MAX_HEIGHT, roomBelow),
        top: anchor.bottom + 6,
      });
    }

    place();
    window.addEventListener("resize", place);
    window.addEventListener("scroll", place, true);
    return () => {
      window.removeEventListener("resize", place);
      window.removeEventListener("scroll", place, true);
    };
  }, [anchorRef]);

  useEffect(() => {
    function closeOnOutsideClick(event: MouseEvent) {
      const target = event.target as Node | null;
      if (!target) return;
      if (panelRef.current?.contains(target)) return;
      if (anchorRef.current?.contains(target)) return;
      onClose();
    }
    // Captured, and the event stops here: an open popover must swallow Escape
    // so it does not also reach the chat's "Escape cancels the run" shortcut.
    function closeOnEscape(event: KeyboardEvent) {
      if (event.key !== "Escape") return;
      event.stopPropagation();
      onClose();
    }
    window.addEventListener("mousedown", closeOnOutsideClick);
    window.addEventListener("keydown", closeOnEscape, true);
    return () => {
      window.removeEventListener("mousedown", closeOnOutsideClick);
      window.removeEventListener("keydown", closeOnEscape, true);
    };
  }, [anchorRef, onClose]);

  const style = box
    ? `left:${box.left}px;width:${box.width}px;max-height:${box.maxHeight}px;` +
      (box.top === undefined ? `bottom:${box.bottom}px` : `top:${box.top}px`)
    : "left:-9999px;top:-9999px";

  // The sidebar is a transformed element (slide-in drawer), which turns it into
  // the containing block for fixed-position descendants and clips them at its
  // edge; rendering into <body> keeps "fixed" meaning the viewport.
  return createPortal(
    <div
      ref={panelRef}
      role="dialog"
      aria-label={title}
      style={style}
      class="theme-menu-surface fixed z-50 flex flex-col overflow-hidden rounded-lg border border-white/10
             bg-[#14161d] shadow-2xl"
    >
      <div class="flex flex-none items-center gap-2 border-b border-white/[0.07] px-3 py-2">
        <div class="min-w-0 flex-1 truncate text-[12px] font-semibold text-ink-100">{title}</div>
        <button
          type="button"
          onClick={onClose}
          class="grid h-6 w-6 flex-none place-items-center rounded text-ink-300 hover:bg-white/[0.08] hover:text-ink-50"
          aria-label="Close preview links"
          title="Close"
        >
          <X class="h-3.5 w-3.5" />
        </button>
      </div>
      <div class="min-h-0 flex-1 overflow-y-auto touch-scroll scrollbar-thin p-2">{children}</div>
    </div>,
    document.body,
  );
}
