import { describe, expect, test } from 'bun:test';

import type { Worker } from '../store/workers';
import { formatWorkerSessionPreview } from './sessionPreview';

type PreviewWorker = Pick<
  Worker,
  | 'status'
  | 'summary'
  | 'attention'
  | 'last_output_lines'
  | 'delegated'
  | 'needs_attention'
  | 'phase'
>;

function agent(overrides: Partial<PreviewWorker>): PreviewWorker {
  return {
    status: 'unknown',
    summary: '',
    attention: '',
    last_output_lines: [],
    delegated: false,
    needs_attention: false,
    phase: '',
    ...overrides,
  };
}

describe('Session row preview hierarchy', () => {
  test.each([
    'running',
    'done',
    'blocked',
    'failed',
    'unknown',
  ] as const)('does not synthesize a visible %s status label', (status) => {
    expect(formatWorkerSessionPreview(agent({ status })).text).toBe(
      'No recent output',
    );
  });

  test('does not synthesize Brain ownership into delegated preview text', () => {
    expect(
      formatWorkerSessionPreview(agent({ status: 'running', delegated: true }))
        .text,
    ).toBe('No recent output');
  });

  test.each([
    'blocked',
    'failed',
  ] as const)('prefers meaningful output over generic %s state detail', (status) => {
    expect(
      formatWorkerSessionPreview(
        agent({
          status,
          summary: 'Preserved summary',
          attention: 'Needs review',
          last_output_lines: ['Most recent useful output'],
        }),
      ).text,
    ).toBe('Most recent useful output');
  });
});
