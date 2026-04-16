import React, { useState } from 'react';
import { StyleSheet, Text, TouchableOpacity, View } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import { Colors, Typography } from '../../constants/tokens';
import { addDays, toDueDateString, parseDueDate, formatDueDateShort } from '../../services/dueDate';

interface Props {
  value?: string | null;
  onChange: (dueDate: string | null) => void;
}

type Preset = {
  key: string;
  label: string;
  value: string | null;
};

function buildPresets(): Preset[] {
  const today = new Date();
  return [
    { key: 'none', label: 'No date', value: null },
    { key: 'today', label: 'Today', value: toDueDateString(today) },
    { key: 'tomorrow', label: 'Tomorrow', value: toDueDateString(addDays(today, 1)) },
    { key: 'week', label: 'Next week', value: toDueDateString(addDays(today, 7)) },
    { key: '2weeks', label: 'In 2 weeks', value: toDueDateString(addDays(today, 14)) },
  ];
}

const WEEKDAY_LABELS = ['Su', 'Mo', 'Tu', 'We', 'Th', 'Fr', 'Sa'];
const MONTH_NAMES = [
  'January', 'February', 'March', 'April', 'May', 'June',
  'July', 'August', 'September', 'October', 'November', 'December',
];

function CalendarGrid({
  year,
  month,
  selectedValue,
  onSelect,
}: {
  year: number;
  month: number; // 0-indexed
  selectedValue?: string | null;
  onSelect: (value: string) => void;
}) {
  const today = new Date();
  const todayStr = toDueDateString(today);

  // First day of month weekday (0=Sun)
  const firstWeekday = new Date(year, month, 1).getDay();
  const daysInMonth = new Date(year, month + 1, 0).getDate();

  const cells: (number | null)[] = [];
  for (let i = 0; i < firstWeekday; i++) cells.push(null);
  for (let d = 1; d <= daysInMonth; d++) cells.push(d);
  // pad to full rows
  while (cells.length % 7 !== 0) cells.push(null);

  return (
    <View style={calStyles.grid}>
      {/* Weekday headers */}
      <View style={calStyles.row}>
        {WEEKDAY_LABELS.map((wd) => (
          <View key={wd} style={calStyles.cell}>
            <Text style={calStyles.weekdayLabel}>{wd}</Text>
          </View>
        ))}
      </View>
      {/* Day cells grouped into rows */}
      {Array.from({ length: cells.length / 7 }, (_, rowIdx) => (
        <View key={rowIdx} style={calStyles.row}>
          {cells.slice(rowIdx * 7, rowIdx * 7 + 7).map((day, colIdx) => {
            if (day === null) {
              return <View key={colIdx} style={calStyles.cell} />;
            }
            const dateStr = `${year}-${String(month + 1).padStart(2, '0')}-${String(day).padStart(2, '0')}`;
            const isSelected = dateStr === selectedValue;
            const isToday = dateStr === todayStr;
            const isPast = dateStr < todayStr;

            return (
              <TouchableOpacity
                key={colIdx}
                style={[
                  calStyles.cell,
                  isSelected && calStyles.cellSelected,
                  isToday && !isSelected && calStyles.cellToday,
                ]}
                onPress={() => onSelect(dateStr)}
                activeOpacity={0.75}
              >
                <Text
                  style={[
                    calStyles.dayText,
                    isSelected && calStyles.dayTextSelected,
                    isToday && !isSelected && calStyles.dayTextToday,
                    isPast && !isSelected && calStyles.dayTextPast,
                  ]}
                >
                  {day}
                </Text>
              </TouchableOpacity>
            );
          })}
        </View>
      ))}
    </View>
  );
}

export function DueDatePicker({ value, onChange }: Props) {
  const presets = buildPresets();
  const isPreset = presets.some((p) => p.value === value);
  const showCalendar = !isPreset && value !== undefined;

  // Calendar month state — start at the selected date's month or today
  const initialDate = parseDueDate(value) ?? new Date();
  const [calYear, setCalYear] = useState(initialDate.getFullYear());
  const [calMonth, setCalMonth] = useState(initialDate.getMonth());
  const [calOpen, setCalOpen] = useState(showCalendar);

  const stepMonth = (delta: number) => {
    let m = calMonth + delta;
    let y = calYear;
    if (m > 11) { m = 0; y++; }
    if (m < 0)  { m = 11; y--; }
    setCalMonth(m);
    setCalYear(y);
  };

  const handlePreset = (preset: Preset) => {
    setCalOpen(false);
    onChange(preset.value);
  };

  const handleCalendarSelect = (dateStr: string) => {
    onChange(dateStr);
    setCalOpen(false);
  };

  const toggleCalendar = () => {
    if (!calOpen) {
      // open calendar anchored to selected date or today
      const anchor = parseDueDate(value) ?? new Date();
      setCalYear(anchor.getFullYear());
      setCalMonth(anchor.getMonth());
    }
    setCalOpen((v) => !v);
  };

  return (
    <View style={styles.container}>
      {/* Preset rows */}
      {presets.map((preset) => {
        const active = preset.value === value || (preset.key === 'none' && !value && !calOpen);
        return (
          <TouchableOpacity
            key={preset.key}
            style={[styles.row, active && styles.rowActive]}
            onPress={() => handlePreset(preset)}
            activeOpacity={0.82}
          >
            <Text style={[styles.label, active && styles.labelActive]}>
              {preset.label}
            </Text>
            {preset.value && !active ? (
              <Text style={styles.meta}>{formatDueDateShort(preset.value)}</Text>
            ) : null}
            {active ? <View style={styles.activeDot} /> : null}
          </TouchableOpacity>
        );
      })}

      {/* Custom / Calendar toggle */}
      <TouchableOpacity
        style={[styles.row, calOpen && styles.rowActive]}
        onPress={toggleCalendar}
        activeOpacity={0.82}
      >
        <Text style={[styles.label, calOpen && styles.labelActive]}>Custom date</Text>
        {value && !isPreset ? (
          <Text style={styles.meta}>{formatDueDateShort(value)}</Text>
        ) : null}
        <Ionicons
          name={calOpen ? 'chevron-up' : 'calendar-outline'}
          size={14}
          color={calOpen ? Colors.accent : Colors.textSecondary}
          style={{ marginLeft: 4 }}
        />
      </TouchableOpacity>

      {/* Inline calendar */}
      {calOpen ? (
        <View style={styles.calendarContainer}>
          {/* Month navigation */}
          <View style={styles.calendarNav}>
            <TouchableOpacity
              style={styles.calNavBtn}
              onPress={() => stepMonth(-1)}
              activeOpacity={0.75}
            >
              <Ionicons name="chevron-back" size={16} color={Colors.textPrimary} />
            </TouchableOpacity>
            <Text style={styles.calMonthLabel}>
              {MONTH_NAMES[calMonth]} {calYear}
            </Text>
            <TouchableOpacity
              style={styles.calNavBtn}
              onPress={() => stepMonth(1)}
              activeOpacity={0.75}
            >
              <Ionicons name="chevron-forward" size={16} color={Colors.textPrimary} />
            </TouchableOpacity>
          </View>

          <CalendarGrid
            year={calYear}
            month={calMonth}
            selectedValue={value}
            onSelect={handleCalendarSelect}
          />
        </View>
      ) : null}
    </View>
  );
}

const CELL_SIZE = 36;

const calStyles = StyleSheet.create({
  grid: {
    paddingHorizontal: 2,
  },
  row: {
    flexDirection: 'row',
  },
  cell: {
    flex: 1,
    height: CELL_SIZE,
    alignItems: 'center',
    justifyContent: 'center',
    borderRadius: 8,
  },
  cellSelected: {
    backgroundColor: Colors.accent,
  },
  cellToday: {
    backgroundColor: 'rgba(91,157,255,0.12)',
  },
  weekdayLabel: {
    color: Colors.textSecondary,
    fontSize: 10,
    fontFamily: Typography.uiFontMedium,
    opacity: 0.6,
  },
  dayText: {
    color: Colors.textPrimary,
    fontSize: 13,
    fontFamily: Typography.uiFont,
  },
  dayTextSelected: {
    color: Colors.bgPrimary,
    fontFamily: Typography.uiFontMedium,
  },
  dayTextToday: {
    color: Colors.accent,
    fontFamily: Typography.uiFontMedium,
  },
  dayTextPast: {
    opacity: 0.35,
  },
});

const styles = StyleSheet.create({
  container: {
    gap: 2,
  },
  row: {
    minHeight: 44,
    paddingHorizontal: 4,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    borderRadius: 10,
  },
  rowActive: {
    backgroundColor: 'rgba(91,157,255,0.08)',
  },
  label: {
    flex: 1,
    color: Colors.textPrimary,
    fontSize: 14,
    fontFamily: Typography.uiFontMedium,
  },
  labelActive: {
    color: Colors.accent,
  },
  meta: {
    color: Colors.textSecondary,
    fontSize: 12,
    fontFamily: Typography.uiFont,
    marginRight: 8,
  },
  activeDot: {
    width: 6,
    height: 6,
    borderRadius: 3,
    backgroundColor: Colors.accent,
  },
  calendarContainer: {
    marginTop: 4,
    borderRadius: 12,
    backgroundColor: 'rgba(255,255,255,0.03)',
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: 'rgba(255,255,255,0.08)',
    paddingHorizontal: 8,
    paddingVertical: 10,
    gap: 8,
  },
  calendarNav: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingHorizontal: 4,
  },
  calNavBtn: {
    width: 32,
    height: 32,
    alignItems: 'center',
    justifyContent: 'center',
    borderRadius: 8,
    backgroundColor: 'rgba(255,255,255,0.05)',
  },
  calMonthLabel: {
    color: Colors.textPrimary,
    fontSize: 13,
    fontFamily: Typography.uiFontMedium,
  },
});
