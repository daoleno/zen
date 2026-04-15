import React from "react";
import { StyleSheet, Text, TouchableOpacity, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { Colors, Typography } from "../../constants/tokens";
import type { Attachment } from "../../store/tasks";

function getAttachmentLabel(attachment: Attachment) {
  const name = attachment.name?.trim();
  if (name) {
    return name;
  }

  const normalizedPath = attachment.path.trim().replace(/\/+$/, "");
  const parts = normalizedPath.split("/").filter(Boolean);
  return parts[parts.length - 1] || normalizedPath || "Attachment";
}

export function AttachmentStack({
  attachments,
  emptyLabel = "No files attached",
  addLabel = "Add file",
  compact = false,
  addDisabled = false,
  onAdd,
  onRemove,
}: {
  attachments: Attachment[];
  emptyLabel?: string;
  addLabel?: string;
  compact?: boolean;
  addDisabled?: boolean;
  onAdd?: () => void;
  onRemove?: (attachment: Attachment) => void;
}) {
  return (
    <View style={styles.root}>
      {attachments.length === 0 ? (
        <Text style={[styles.emptyLabel, compact && styles.emptyLabelCompact]}>
          {emptyLabel}
        </Text>
      ) : (
        <View style={styles.list}>
          {attachments.map((attachment) => (
            <View
              key={`${attachment.path}:${attachment.name}`}
              style={[
                styles.row,
                compact ? styles.rowCompact : null,
                !compact ? styles.rowCard : null,
              ]}
            >
              <View style={styles.iconWrap}>
                <Ionicons
                  name="document-attach-outline"
                  size={16}
                  color={Colors.textSecondary}
                />
              </View>

              <View style={styles.copy}>
                <Text
                  style={[styles.name, compact && styles.nameCompact]}
                  numberOfLines={1}
                >
                  {getAttachmentLabel(attachment)}
                </Text>
                <Text style={styles.path} numberOfLines={2}>
                  {attachment.path}
                </Text>
              </View>

              {onRemove ? (
                <TouchableOpacity
                  style={styles.removeButton}
                  onPress={() => onRemove(attachment)}
                  activeOpacity={0.82}
                >
                  <Ionicons
                    name="close"
                    size={14}
                    color={Colors.textSecondary}
                  />
                </TouchableOpacity>
              ) : null}
            </View>
          ))}
        </View>
      )}

      {onAdd ? (
        <TouchableOpacity
          style={[
            styles.addButton,
            compact ? styles.addButtonCompact : null,
            addDisabled ? styles.addButtonDisabled : null,
          ]}
          onPress={onAdd}
          activeOpacity={0.82}
          disabled={addDisabled}
        >
          <Ionicons
            name="attach-outline"
            size={15}
            color={addDisabled ? Colors.textSecondary : Colors.textPrimary}
          />
          <Text
            style={[
              styles.addButtonText,
              addDisabled ? styles.addButtonTextDisabled : null,
            ]}
          >
            {addLabel}
          </Text>
        </TouchableOpacity>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  root: {
    gap: 10,
  },
  list: {
    gap: 8,
  },
  emptyLabel: {
    color: Colors.textSecondary,
    fontSize: 13,
    lineHeight: 20,
    fontFamily: Typography.uiFont,
  },
  emptyLabelCompact: {
    fontSize: 12,
    lineHeight: 18,
  },
  row: {
    flexDirection: "row",
    alignItems: "flex-start",
    gap: 10,
  },
  rowCard: {
    paddingHorizontal: 12,
    paddingVertical: 12,
    borderRadius: 16,
    backgroundColor: "rgba(255,255,255,0.03)",
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(255,255,255,0.08)",
  },
  rowCompact: {
    paddingVertical: 2,
  },
  iconWrap: {
    width: 20,
    paddingTop: 2,
    alignItems: "center",
  },
  copy: {
    flex: 1,
    gap: 4,
  },
  name: {
    color: Colors.textPrimary,
    fontSize: 13,
    fontFamily: Typography.uiFontMedium,
  },
  nameCompact: {
    fontSize: 12,
  },
  path: {
    color: Colors.textSecondary,
    fontSize: 12,
    lineHeight: 18,
    fontFamily: Typography.uiFont,
  },
  removeButton: {
    width: 26,
    height: 26,
    marginTop: -2,
    borderRadius: 13,
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: "rgba(255,255,255,0.04)",
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(255,255,255,0.08)",
  },
  addButton: {
    minHeight: 38,
    alignSelf: "flex-start",
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
    paddingHorizontal: 12,
    borderRadius: 19,
    backgroundColor: "rgba(255,255,255,0.04)",
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(255,255,255,0.08)",
  },
  addButtonCompact: {
    minHeight: 34,
  },
  addButtonDisabled: {
    opacity: 0.45,
  },
  addButtonText: {
    color: Colors.textPrimary,
    fontSize: 12,
    fontFamily: Typography.uiFontMedium,
  },
  addButtonTextDisabled: {
    color: Colors.textSecondary,
  },
});
