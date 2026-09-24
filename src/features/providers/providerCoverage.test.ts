import { describe, expect, it } from 'vitest';
import { PROVIDER_BRAND_ORDER } from './descriptors';
import { getSponsorAggregationConflict, isMultiProtocolSponsorBrand } from './sponsorDefinitions';
import {
  buildCodexResponsesEndpoint,
  buildGeminiGenerateContentEndpoint,
  buildInteractionsEndpoint,
} from '@/components/providers/utils';
import { normalizeConfigResponse } from '@/services/api/transformers';

describe('provider workbench coverage', () => {
  it('lists every provider category supported by the CPA management API', () => {
    expect(PROVIDER_BRAND_ORDER).toEqual([
      'kimi',
      'gemini',
      'interactions',
      'codex',
      'meta',
      'xai',
      'claude',
      'vertex',
      'openaiCompatibility',
      'apikeyFun',
      'fennoAI',
      'qiniuCloud',
    ]);
  });

  it('normalizes Interactions, Meta, and xAI lists from CPA config responses', () => {
    const config = normalizeConfigResponse({
      'interactions-api-key': [{ 'api-key': 'interactions-key', weight: 2 }],
      'meta-api-key': [{ 'api-key': 'meta-key', 'fingerprint-profile': 'strict' }],
      'xai-api-key': [{ 'api-key': 'xai-key', 'base-url': 'https://api.x.ai/v1' }],
    });

    expect(config.interactionsApiKeys?.[0]).toMatchObject({ apiKey: 'interactions-key', weight: 2 });
    expect(config.metaApiKeys?.[0]).toMatchObject({ apiKey: 'meta-key', fingerprintProfile: 'strict' });
    expect(config.xaiApiKeys?.[0]).toMatchObject({ apiKey: 'xai-key', baseUrl: 'https://api.x.ai/v1' });
  });

  it('identifies the four multi-protocol sponsor aggregations', () => {
    expect(isMultiProtocolSponsorBrand('apikeyFun')).toBe(true);
    expect(isMultiProtocolSponsorBrand('fennoAI')).toBe(true);
    expect(isMultiProtocolSponsorBrand('qiniuCloud')).toBe(true);
    expect(isMultiProtocolSponsorBrand('kimi')).toBe(true);
    expect(isMultiProtocolSponsorBrand('interactions')).toBe(false);
  });

  it('builds protocol-specific upstream endpoints for provider connectivity checks', () => {
    expect(buildCodexResponsesEndpoint('https://api.example/v1')).toBe(
      'https://api.example/v1/responses'
    );
    expect(buildInteractionsEndpoint('https://generativelanguage.googleapis.com/v1beta/models')).toBe(
      'https://generativelanguage.googleapis.com/v1beta/interactions'
    );
    expect(
      buildGeminiGenerateContentEndpoint('https://generativelanguage.googleapis.com', 'gemini-2.5')
    ).toBe(
      'https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5:generateContent'
    );
  });

  it('flags ambiguous sponsor configurations that cannot be safely edited as one resource', () => {
    expect(
      getSponsorAggregationConflict({
        openai: [
          { index: 0, config: { name: 'one', baseUrl: 'https://api.example', apiKeyEntries: [] } },
          { index: 1, config: { name: 'two', baseUrl: 'https://api.example', apiKeyEntries: [] } },
        ],
        claude: [],
        codex: [],
        gemini: [],
      })
    ).toBe('multiple-configs');
    expect(getSponsorAggregationConflict(null)).toBeNull();
  });
});
