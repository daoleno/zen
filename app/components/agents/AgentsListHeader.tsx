import React, { useMemo, useState } from 'react';
import {
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  TouchableOpacity,
  View,
} from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import {
  Colors,
  TypeScale,
  UiTextMetrics,
  useAppColors,
} from '../../constants/tokens';
import { IconButton } from '../ui/IconButton';

export type AgentsStatusFilter = 'all' | 'running' | 'brain';

interface FilterOption {
  key: AgentsStatusFilter;
  label: string;
  count: number;
}

interface AgentsListHeaderProps {
  searchQuery: string;
  statusFilter: AgentsStatusFilter;
  filterOptions: FilterOption[];
  onSearchChange: (value: string) => void;
  onFilterChange: (filter: AgentsStatusFilter) => void;
  onOpenMenu: () => void;
}

export function AgentsListHeader({
  searchQuery,
  statusFilter,
  filterOptions,
  onSearchChange,
  onFilterChange,
  onOpenMenu,
}: AgentsListHeaderProps) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const [searchFocused, setSearchFocused] = useState(false);

  return (
    <View style={styles.root}>
      <View style={styles.inner}>
        <View style={styles.searchRow}>
          <View style={[styles.searchBar, searchFocused && styles.searchBarFocused]}>
            <Ionicons name="search" size={18} color={colors.textTertiary} />
            <TextInput
              style={styles.searchInput}
              value={searchQuery}
              onChangeText={onSearchChange}
              onFocus={() => setSearchFocused(true)}
              onBlur={() => setSearchFocused(false)}
              placeholder="Search sessions"
              placeholderTextColor={colors.textTertiary}
              autoCapitalize="none"
              autoCorrect={false}
              returnKeyType="search"
              clearButtonMode="never"
              accessibilityLabel="Search sessions"
            />
            {searchQuery ? (
              <TouchableOpacity
                style={styles.clearButton}
                onPress={() => onSearchChange('')}
                activeOpacity={0.72}
                accessibilityRole="button"
                accessibilityLabel="Clear search"
              >
                <Ionicons name="close-circle" size={18} color={colors.textTertiary} />
              </TouchableOpacity>
            ) : null}
          </View>
          <IconButton
            icon="ellipsis-vertical"
            tone="ghost"
            size={44}
            iconSize={20}
            color={colors.textSecondary}
            accessibilityLabel="List options"
            onPress={onOpenMenu}
          />
        </View>

        <ScrollView
          horizontal
          showsHorizontalScrollIndicator={false}
          contentContainerStyle={styles.filterScroll}
          keyboardShouldPersistTaps="handled"
        >
          {filterOptions.map((option) => {
            const selected = statusFilter === option.key;
            return (
              <TouchableOpacity
                key={option.key}
                style={[styles.filterControl, selected && styles.filterControlActive]}
                onPress={() => onFilterChange(option.key)}
                activeOpacity={0.72}
                accessibilityRole="button"
                accessibilityState={{ selected }}
                accessibilityLabel={`${option.label}, ${option.count} sessions`}
              >
                <Text style={[styles.filterText, selected && styles.filterTextActive]}>
                  {option.label}
                </Text>
                <Text style={[styles.filterCount, selected && styles.filterCountActive]}>
                  {option.count > 99 ? '99+' : option.count}
                </Text>
              </TouchableOpacity>
            );
          })}
        </ScrollView>
      </View>
    </View>
  );
}

function createStyles(colors: typeof Colors) {
  return StyleSheet.create({
    root: {
      backgroundColor: colors.bgPrimary,
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: colors.borderSubtle,
    },
    inner: {
      width: '100%',
      maxWidth: 760,
      alignSelf: 'center',
      paddingTop: 8,
      paddingBottom: 8,
      gap: 4,
    },
    searchRow: {
      flexDirection: 'row',
      alignItems: 'center',
      gap: 4,
      paddingHorizontal: 16,
    },
    searchBar: {
      flex: 1,
      minWidth: 0,
      minHeight: 44,
      borderRadius: 8,
      paddingLeft: 12,
      flexDirection: 'row',
      alignItems: 'center',
      gap: 8,
      backgroundColor: colors.inputBackground,
      borderWidth: 1,
      borderColor: colors.border,
    },
    searchBarFocused: {
      borderColor: colors.focusRing,
    },
    searchInput: {
      ...UiTextMetrics,
      ...TypeScale.body,
      flex: 1,
      minWidth: 0,
      color: colors.textPrimary,
      paddingVertical: 0,
    },
    clearButton: {
      width: 44,
      height: 44,
      alignItems: 'center',
      justifyContent: 'center',
      marginRight: -1,
    },
    filterScroll: {
      paddingHorizontal: 16,
      gap: 4,
      alignItems: 'center',
    },
    filterControl: {
      minHeight: 44,
      paddingHorizontal: 12,
      borderRadius: 8,
      flexDirection: 'row',
      alignItems: 'center',
      justifyContent: 'center',
      gap: 6,
      backgroundColor: 'transparent',
    },
    filterControlActive: {
      backgroundColor: colors.accentSoft,
    },
    filterText: {
      ...UiTextMetrics,
      ...TypeScale.label,
      color: colors.textSecondary,
    },
    filterTextActive: {
      color: colors.accentStrong,
    },
    filterCount: {
      ...UiTextMetrics,
      ...TypeScale.caption,
      color: colors.textSecondary,
    },
    filterCountActive: {
      color: colors.accentStrong,
    },
  });
}
