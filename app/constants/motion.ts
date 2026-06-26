import type { WithSpringConfig } from "react-native-reanimated";

export const Spring = {
  press: {
    stiffness: 420,
    damping: 30,
    mass: 0.9,
  } as WithSpringConfig,

  card: {
    stiffness: 340,
    damping: 28,
    mass: 1,
  } as WithSpringConfig,

  rise: {
    stiffness: 260,
    damping: 26,
    mass: 1,
  } as WithSpringConfig,
} as const;

export const PRESSED_SCALE = 0.96;
