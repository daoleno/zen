import { describe, expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { costEstimateMark, statsModelHeading, statsModelKey } from './statsCostPresentation';
import type { ModelStat } from './statsPayload';

const model = (overrides: Partial<ModelStat> = {}): ModelStat => ({
  name: 'gpt-6-astra', id: 'gpt-6-astra', provider: 'openai', cost: 2,
  costKnown: true, costProvenance: 'estimated', totalTokens: 100,
  inputTokens: 80, outputTokens: 20, reasoningTokens: 0, cacheRead: 0,
  cacheCreate: 0, sessions: 1, ...overrides,
});

describe('Stats overview and model details', () => {
  test('estimated and mixed amounts remain distinct from reported and missing prices', () => {
    expect(costEstimateMark(2, true, 'estimated')).toBe('≈');
    expect(costEstimateMark(2, true, 'mixed')).toBe('≈');
    expect(costEstimateMark(2, true, 'reported')).toBe('');
    expect(costEstimateMark(0, false, 'unknown')).toBe('');
    expect(costEstimateMark(0, false, 'estimated')).toBe('');
    expect(costEstimateMark(0, true, 'estimated')).toBe('≈');
    expect(costEstimateMark(0, true, 'reported')).toBe('');
    expect(costEstimateMark(2, false, 'mixed')).toBe('≈');
  });
  test('provider appears only when it disambiguates the same exact model', () => {
    const first = model();
    const other = model({ provider: 'other-provider' });
    expect(statsModelHeading(first, [first])).toBe('gpt-6-astra');
    expect(statsModelHeading(first, [first, other])).toBe('gpt-6-astra (openai)');
    expect(statsModelKey(first)).not.toBe(statsModelKey(other));
  });
  test('overview is two lines without repeated diagnostic text or stub bars', () => {
    const source = readFileSync(new URL('../app/stats.tsx', import.meta.url), 'utf8');
    const overview = source.slice(source.indexOf("{(expandedSections.has('models')"), source.indexOf('{data.models.length > MAX_LIST_ITEMS'));
    expect(overview).toContain('ellipsizeMode="middle"');
    expect(overview).toContain('onPress={() => onSelectModel(m)}');
    expect(overview).toContain('numberOfLines={1}>{rowActivitySummary(m)}');
    expect(overview).not.toContain('<Bar');
    expect(overview).not.toContain('m.estimateSource');
    expect(overview).not.toContain('m.pricingUpdatedAt');
    expect(overview).not.toContain('unpricedReasonLabel');
    expect(overview).not.toContain('[m.provider, rowActivitySummary');
    expect(source).toContain('modelSelection?.serverId === currentServerId');
    expect(source).toContain('useEffect(() => setModelSelection(null), [currentServerId, range])');
  });
  test('details retain full name, exact costs and long missing-context diagnostics', () => {
    const source = readFileSync(new URL('../app/stats.tsx', import.meta.url), 'utf8');
    const details = source.slice(source.indexOf('function ModelDetailSheet'), source.indexOf('function DayDetailSheet'));
    expect(details).toContain('selectable accessibilityRole="header"');
    expect(details).toContain('{model.id || model.name}');
    expect(details).toContain("['Price type', costSourceLabel(model.costProvenance)]");
    expect(details).toContain('model?.costReported ?? model?.reportedCost');
    expect(details).toContain('model?.costEstimated ?? model?.estimatedCost');
    expect(details).toContain('unpricedReasonLabel(model.unpricedReason)');
    expect(details).toContain('model.estimateSource');
    expect(details).toContain('model.pricingUpdatedAt');
    expect(source).toContain("case 'mixed': return 'Mixed'");
    expect(source).toContain('Cost · {costSourceLabel(data.costProvenance)}');
  });
});
