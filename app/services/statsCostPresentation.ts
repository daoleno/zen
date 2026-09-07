import type { CostProvenance, ModelStat } from './statsPayload';

export function costEstimateMark(cost: number, known: boolean | undefined, provenance?: CostProvenance): string {
  if (known === false && cost === 0) return '';
  return provenance === 'estimated' || provenance === 'mixed' || provenance === undefined
    ? '\u2248'
    : '';
}

export function statsModelKey(model: ModelStat): string {
  return JSON.stringify([model.provider ?? '', model.id || model.name]);
}

export function statsModelHeading(model: ModelStat, models: readonly ModelStat[]): string {
  const name = model.id || model.name;
  const ambiguous = models.some(other => (other.id || other.name) === name && other.provider !== model.provider);
  return ambiguous && model.provider ? `${name} (${model.provider})` : name;
}
