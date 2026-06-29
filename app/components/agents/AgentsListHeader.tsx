import React, { useMemo } from 'react';
import {
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  TouchableOpacity,
  View,
} from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import { Colors, Typography, useAppColors } from '../../constants/tokens';
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

  return (
    <View style={styles.root}>
      <View style={styles.titleRow}>
        <Text style={styles.title}>Zen</Text>
        <IconButton
          icon="ellipsis-vertical"
          tone="ghost"
          size={40}
          iconSize={20}
          color={colors.textSecondary}
          accessibilityLabel="Agents options"
          onPress={onOpenMenu}
        />
      </View>

      <View style={styles.searchBar}>
        <Ionicons name="search" size={17} color={colors.textTertiary} />
        <TextInput
          style={styles.searchInput}
          value={searchQuery}
          onChangeText={onSearchChange}
          placeholder="Search"
          placeholderTextColor={colors.textTertiary}
          autoCapitalize="none"
          autoCorrect={false}
          returnKeyType="search"
          clearButtonMode="never"
        />
        {searchQuery ? (
          <TouchableOpacity onPress={() => onSearchChange('')} activeOpacity={0.75}>
            <Ionicons name="close-circle" size={17} color={colors.textTertiary} />
          </TouchableOpacity>
        ) : null}
      </View>

      <ScrollView
        horizontal
        showsHorizontalScrollIndicator={false}
        contentContainerStyle={styles.filterScroll}
      >
        {filterOptions.map((option) => {
          const selected = statusFilter === option.key;
          return (
            <TouchableOpacity
              key={option.key}
              style={[styles.filterPill, selected && styles.filterPillActive]}
              onPress={() => onFilterChange(option.key)}
              activeOpacity={0.82}
            >
              <Text style={[styles.filterPillText, selected && styles.filterPillTextActive]}>
                {option.label}
              </Text>
              {option.count > 0 ? (
                <View style={[styles.filterBadge, selected && styles.filterBadgeActive]}>
                  <Text style={[styles.filterBadgeText, selected && styles.filterBadgeTextActive]}>
                    {option.count > 99 ? '99+' : option.count}
                  </Text>
                </View>
              ) : null}
            </TouchableOpacity>
          );
        })}
      </ScrollView>
    </View>
  );
}

function createStyles(colors: typeof Colors) {
  return StyleSheet.create({
    root: {
      paddingTop: 4,
      paddingBottom: 10,
      gap: 10,
      backgroundColor: colors.bgPrimary,
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: colors.borderSubtle,
    },
    titleRow: {
      flexDirection: 'row',
      alignItems: 'center',
      justifyContent: 'space-between',
      paddingHorizontal: 16,
      minHeight: 44,
    },
    title: {
      color: colors.textPrimary,
      fontSize: 28,
      lineHeight: 32,
      fontFamily: Typography.uiFontMedium,
      letterSpacing: -0.3,
    },
    searchBar: {
      marginHorizontal: 16,
      height: 36,
      borderRadius: 18,
      paddingHorizontal: 12,
      flexDirection: 'row',
      alignItems: 'center',
      gap: 8,
      backgroundColor: colors.inputBackground,
    },
    searchInput: {
      flex: 1,
      minWidth: 0,
      color: colors.textPrimary,
      fontFamily: Typography.uiFont,
      fontSize: 15,
      paddingVertical: 0,
    },
    filterScroll: {
      paddingHorizontal: 16,
      gap: 8,
      alignItems: 'center',
    },
    filterPill: {
      minHeight: 30,
      paddingHorizontal: 13,
      borderRadius: 15,
      flexDirection: 'row',
      alignItems: 'center',
      justifyContent: 'center',
      gap: 6,
      backgroundColor: colors.surfaceSubtle,
    },
    filterPillActive: {
      backgroundColor: colors.accent,
    },
    filterPillText: {
      color: colors.textSecondary,
      fontFamily: Typography.uiFontMedium,
      fontSize: 13,
      lineHeight: 17,
    },
    filterPillTextActive: {
      color: colors.textOnAccent,
    },
    filterBadge: {
      minWidth: 18,
      height: 18,
      borderRadius: 9,
      paddingHorizontal: 5,
      alignItems: 'center',
      justifyContent: 'center',
      backgroundColor: colors.bgElevated,
    },
    filterBadgeActive: {
      backgroundColor: 'rgba(255,255,255,0.22)',
    },
    filterBadgeText: {
      color: colors.textSecondary,
      fontFamily: Typography.uiFontMedium,
      fontSize: 11,
      lineHeight: 13,
      includeFontPadding: false,
    },
    filterBadgeTextActive: {
      color: colors.textOnAccent,
    },
  });
}