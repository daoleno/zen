import type { ComponentProps } from "react";
import { TabTrigger } from "expo-router/ui";
import { Ionicons } from "@expo/vector-icons";
import { ZenTabButton } from "./ZenTabButton";

type ZenTabTriggerProps = {
  name: string;
  label: string;
  icon: ComponentProps<typeof Ionicons>["name"];
  iconFocused: ComponentProps<typeof Ionicons>["name"];
};

export function ZenTabTrigger({
  name,
  label,
  icon,
  iconFocused,
}: ZenTabTriggerProps) {
  return (
    <TabTrigger name={name} asChild>
      <ZenTabButton label={label} icon={icon} iconFocused={iconFocused} />
    </TabTrigger>
  );
}