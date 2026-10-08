import { describe, expect, it } from 'vitest';
import type { ModelTraceRecord } from '@/services/api/modelTrace';
import { getModelTraceLabel, getModelTraceMatch, supportedModelTraceModels } from './modelTracePresentation';

const result = (status: ModelTraceRecord['status'], verdict: ModelTraceRecord['verdict']) =>
  ({ status, verdict } as ModelTraceRecord);

describe('ModelTrace presentation', () => {
  it('never presents a failed or incomplete run as suspected degradation', () => {
    expect(getModelTraceLabel({ latest: result('failed', 'suspected') })).toBe('failed');
    expect(getModelTraceLabel({ latest: result('partial', 'suspected') })).toBe('inconclusive');
    expect(getModelTraceLabel({ latest: result('interrupted', 'consistent') })).toBe('interrupted');
    expect(getModelTraceLabel({ latest: result('cancelled', 'suspected') })).toBe('cancelled');
  });
  it('keeps running state separate from the previous result', () => {
    expect(getModelTraceLabel({ running: { phase: 'collecting' }, latest: result('completed', 'suspected') })).toBe('running');
    expect(getModelTraceLabel({ running: { phase: 'cancelling' } })).toBe('cancelling');
    expect(getModelTraceLabel({ latest: result('completed', 'consistent') })).toBe('consistent');
    expect(getModelTraceLabel({ latest: result('completed', 'inconclusive') })).toBe('inconclusive');
    expect(getModelTraceLabel(undefined)).toBe('never');
  });
  it('compares the selected model independently of the degradation verdict', () => {
    const record: ModelTraceRecord = {
      ...result('completed', 'consistent'), model: 'gpt-5.4',
      attribution: { prediction: 'gpt-6-luna', probability: 0.99, used_outputs: 3 },
    } as ModelTraceRecord;
    expect(getModelTraceMatch(record)).toBe('mismatched');
    expect(getModelTraceLabel({ latest: record })).toBe('consistent');
    expect(getModelTraceMatch({ ...record, attribution: { ...record.attribution!, prediction: 'gpt-5.4' } })).toBe('matched');
    expect(getModelTraceMatch({ ...record, verdict: 'suspected', model: 'gpt-5.6-luna', attribution: { ...record.attribution!, prediction: 'gpt-5.6-luna' } })).toBe('matched');
  });
  it('never claims fingerprint matching from incomplete or low-confidence evidence', () => {
    const record = { ...result('completed', 'consistent'), model: 'gpt-5.4', attribution: { prediction: 'gpt-5.4', probability: .99, used_outputs: 3 } } as ModelTraceRecord;
    expect(getModelTraceMatch(undefined)).toBe('match_unknown');
    expect(getModelTraceMatch({ ...record, attribution: undefined })).toBe('match_unknown');
    expect(getModelTraceMatch({ ...record, status: 'partial' })).toBe('match_unknown');
    expect(getModelTraceMatch({ ...record, verdict: 'inconclusive' })).toBe('match_unknown');
    expect(getModelTraceMatch({ ...record, attribution: { ...record.attribution!, probability: .79 } })).toBe('match_unknown');
    expect(getModelTraceMatch({ ...record, attribution: { ...record.attribution!, used_outputs: 2 } })).toBe('match_unknown');
  });
  it('offers only exact candidate-bank models supported by the credential', () => {
    const bank = [{ id: 'gpt-5.5', display_name: 'GPT 5.5' }, { id: 'gpt-6-luna', display_name: 'GPT 6' }];
    expect(supportedModelTraceModels(bank, [{ id: 'gpt-5.5' }, { id: 'custom-alias' }])).toEqual([bank[0]]);
    expect(supportedModelTraceModels(bank, [])).toEqual([]);
    expect(supportedModelTraceModels(bank, [{ id: 'openai/gpt-5.5' }])).toEqual([]);
  });
});
