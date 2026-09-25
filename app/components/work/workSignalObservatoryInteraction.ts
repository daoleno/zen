export function resolveWorkObservatoryMotion(reducedMotion: boolean): {
  modalAnimationType: "none" | "fade";
} {
  return {
    modalAnimationType: reducedMotion ? "none" : "fade",
  };
}
