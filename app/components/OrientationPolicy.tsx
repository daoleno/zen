import { useEffect } from "react";
import { AppState } from "react-native";
import { usePathname } from "expo-router";
import { applyOrientationPolicy, orientationPolicyFor } from "../services/screenOrientation";

/**
 * One route-aware orientation owner. Mounted once by the root layout so the
 * Remote Desktop route can follow a landscape desktop while every other route
 * keeps the product portrait lock, including after backgrounding.
 */
export function OrientationPolicy() {
  const pathname = usePathname();
  useEffect(() => {
    void applyOrientationPolicy(orientationPolicyFor(pathname));
  }, [pathname]);
  useEffect(() => {
    const subscription = AppState.addEventListener("change", (state) => {
      void applyOrientationPolicy(state === "active" ? orientationPolicyFor(pathname) : "portrait");
    });
    return () => subscription.remove();
  }, [pathname]);
  return null;
}
