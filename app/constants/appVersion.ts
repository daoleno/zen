import Constants from "expo-constants";
import { resolveAppVersion } from "../services/appVersion";

export const appVersion = resolveAppVersion(Constants.expoConfig?.version);
