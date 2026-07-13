// @ts-nocheck
import { describe, expect, it } from 'bun:test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';

const source = readFileSync(join(import.meta.dir, '../app/stats.tsx'), 'utf8');

describe('Stats pager integration contract', () => {
  it('uses the shared native tab-view architecture with bounded platform behavior', () => {
    expect(source).toContain("import { TabView } from 'react-native-tab-view'");
    expect(source).toContain('onIndexChange={selectRangeIndex}');
    expect(source).toContain('onPress={() => jumpTo(opt.key)}');
    expect(source).toContain('animationEnabled={!reducedMotion}');
    expect(source).toContain('overScrollMode="never"');
  });

  it('drives tab feedback from the pager position throughout a swipe or rollback', () => {
    expect(source).toContain('renderTabBar={({ position, jumpTo }) => (');
    expect(source).toContain('<StatsRangeTabBar');
    expect(source).toContain('const translateX = position.interpolate({');
    expect(source).toContain('const activeTextOpacity = position.interpolate({');
    expect(source).toContain('styles.rangeTabIndicator');
    expect(source).not.toContain('active && s.rangeTabActive');
  });

  it('keeps nested horizontal activity scrollers in control of their touches', () => {
    expect(source).toContain('swipeEnabled={!nestedHorizontalScrollActive}');
    expect(source.match(/onTouchStart={onNestedHorizontalGestureStart}/g)?.length).toBe(2);
    expect(source.match(/onTouchEnd={onNestedHorizontalGestureEnd}/g)?.length).toBe(2);
    expect(source.match(/onTouchCancel={onNestedHorizontalGestureEnd}/g)?.length).toBe(2);
  });

  it('hides mounted off-screen scenes from native accessibility traversal', () => {
    expect(source).toContain('accessibilityElementsHidden={!active}');
    expect(source).toContain("importantForAccessibility={active ? 'auto' : 'no-hide-descendants'}");
  });
});
