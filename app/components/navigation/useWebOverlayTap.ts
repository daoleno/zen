import { useEffect, useRef, type RefObject } from "react";
import { Platform, type View } from "react-native";

interface UseWebOverlayTapOptions {
  enabled: boolean;
  onActivate(): void;
  onBegin(): boolean;
  onCancel(): void;
  overlayRef: RefObject<View | null>;
}

interface PointerOrigin {
  exceededDistance: boolean;
  pointerId: number;
  x: number;
  y: number;
}

const MAX_TAP_DISTANCE = 8;

export function useWebOverlayTap({
  enabled,
  onActivate,
  onBegin,
  onCancel,
  overlayRef,
}: UseWebOverlayTapOptions): void {
  const originRef = useRef<PointerOrigin | null>(null);

  useEffect(() => {
    if (Platform.OS !== "web" || !enabled) {
      return;
    }
    const overlay = overlayRef.current as unknown as HTMLElement | null;
    if (overlay == null) {
      return;
    }

    const exceedsDistance = (event: PointerEvent) => {
      const origin = originRef.current;
      if (origin == null || origin.pointerId !== event.pointerId) {
        return true;
      }
      return (
        Math.hypot(event.clientX - origin.x, event.clientY - origin.y) >
        MAX_TAP_DISTANCE
      );
    };
    const handlePointerDown = (event: PointerEvent) => {
      if (event.button !== 0) {
        return;
      }
      if (!onBegin()) {
        return;
      }
      event.preventDefault();
      originRef.current = {
        exceededDistance: false,
        pointerId: event.pointerId,
        x: event.clientX,
        y: event.clientY,
      };
      overlay.setPointerCapture?.(event.pointerId);
    };
    const handlePointerMove = (event: PointerEvent) => {
      const origin = originRef.current;
      if (
        origin == null ||
        origin.pointerId !== event.pointerId ||
        origin.exceededDistance ||
        !exceedsDistance(event)
      ) {
        return;
      }
      origin.exceededDistance = true;
      onCancel();
    };
    const handlePointerUp = (event: PointerEvent) => {
      const origin = originRef.current;
      if (origin == null || origin.pointerId !== event.pointerId) {
        return;
      }
      const shouldActivate =
        !origin.exceededDistance && !exceedsDistance(event);
      originRef.current = null;
      overlay.releasePointerCapture?.(event.pointerId);
      if (shouldActivate) {
        onActivate();
      } else {
        onCancel();
      }
    };
    const handlePointerCancel = (event: PointerEvent) => {
      if (originRef.current?.pointerId !== event.pointerId) {
        return;
      }
      originRef.current = null;
      onCancel();
    };

    overlay.addEventListener("pointerdown", handlePointerDown);
    overlay.addEventListener("pointermove", handlePointerMove);
    overlay.addEventListener("pointerup", handlePointerUp);
    overlay.addEventListener("pointercancel", handlePointerCancel);
    return () => {
      overlay.removeEventListener("pointerdown", handlePointerDown);
      overlay.removeEventListener("pointermove", handlePointerMove);
      overlay.removeEventListener("pointerup", handlePointerUp);
      overlay.removeEventListener("pointercancel", handlePointerCancel);
      if (originRef.current != null) {
        originRef.current = null;
        onCancel();
      }
    };
  }, [enabled, onActivate, onBegin, onCancel, overlayRef]);
}
