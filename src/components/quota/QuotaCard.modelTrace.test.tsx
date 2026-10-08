import { act, createElement } from 'react';
import { create, type ReactTestRenderer } from 'react-test-renderer';
import { describe, expect, it, vi } from 'vitest';
import { QuotaCard } from './QuotaCard';

vi.mock('react-i18next', () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

describe('QuotaCard independent status slot', () => {
  it.each(['idle', 'loading', 'error', 'success'] as const)('shows ModelTrace even when quota is %s', async (status) => {
    let renderer: ReactTestRenderer;
    await act(async () => {
      renderer = create(createElement(QuotaCard, {
        item: { name: 'codex-a.json', type: 'codex' },
        quota: { status }, resolvedTheme: 'light', i18nPrefix: 'codex_quota',
        cardClassName: '', defaultType: 'codex', renderQuotaItems: () => null,
        statusContent: createElement('span', { 'data-modeltrace': true }, 'ModelTrace result'),
      }));
    });
    expect(renderer!.root.findAllByProps({ 'data-modeltrace': true })).toHaveLength(1);
    await act(async () => renderer!.unmount());
  });
});
