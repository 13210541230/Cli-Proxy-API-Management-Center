import {
  useLayoutEffect,
  useCallback,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type ComponentType,
  type ReactNode,
} from 'react';
import { createPortal } from 'react-dom';
import { useTranslation } from 'react-i18next';
import { usePageTransitionLayer } from '@/components/common/PageTransitionLayer';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Select } from '@/components/ui/Select';
import { ToggleSwitch } from '@/components/ui/ToggleSwitch';
import {
  IconCode,
  IconDiamond,
  IconKey,
  IconSatellite,
  IconSettings,
  IconShield,
  IconTimer,
  IconTrendingUp,
  type IconProps,
} from '@/components/ui/icons';
import { ConfigSection } from '@/components/config/ConfigSection';
import { useMediaQuery } from '@/hooks/useMediaQuery';
import type {
  PayloadFilterRule,
  PayloadParamValidationErrorCode,
  PayloadRule,
  PluginStoreAuthApplyTo,
  PluginStoreAuthRule,
  PluginStoreAuthType,
  VisualConfigFieldPath,
  VisualConfigValidationErrorCode,
  VisualConfigValidationErrors,
  VisualConfigValues,
} from '@/types/visualConfig';
import { makeClientId } from '@/types/visualConfig';
import { PayloadFilterRulesEditor, PayloadRulesEditor } from './VisualConfigEditorBlocks';
import styles from './VisualConfigEditor.module.scss';

type VisualSectionId =
  | 'server'
  | 'tls'
  | 'remote'
  | 'auth'
  | 'system'
  | 'network'
  | 'advanced'
  | 'quota'
  | 'streaming'
  | 'payload';

type VisualSection = {
  id: VisualSectionId;
  title: string;
  description: string;
  icon: ComponentType<IconProps>;
  errorCount: number;
};

interface VisualConfigEditorProps {
  values: VisualConfigValues;
  validationErrors?: VisualConfigValidationErrors;
  hasPayloadValidationErrors?: boolean;
  disabled?: boolean;
  onChange: (values: Partial<VisualConfigValues>) => void;
}

function getValidationMessage(
  t: ReturnType<typeof useTranslation>['t'],
  errorCode?: VisualConfigValidationErrorCode | PayloadParamValidationErrorCode
) {
  if (!errorCode) return undefined;
  return t(`config_management.visual.validation.${errorCode}`);
}

type ToggleRowProps = {
  title: string;
  description?: string;
  checked: boolean;
  disabled?: boolean;
  onChange: (value: boolean) => void;
};

function ToggleRow({ title, description, checked, disabled, onChange }: ToggleRowProps) {
  return (
    <div className={styles.toggleRow}>
      <div className={styles.toggleCopy}>
        <div className={styles.toggleTitle}>{title}</div>
        {description ? <div className={styles.toggleDescription}>{description}</div> : null}
      </div>
      <ToggleSwitch checked={checked} onChange={onChange} disabled={disabled} ariaLabel={title} />
    </div>
  );
}

function SectionGrid({ children }: { children: ReactNode }) {
  return <div className={styles.sectionGrid}>{children}</div>;
}

function SectionStack({ children }: { children: ReactNode }) {
  return <div className={styles.sectionStack}>{children}</div>;
}

function Divider() {
  return <div className={styles.divider} />;
}

function SectionSubsection({
  title,
  description,
  children,
}: {
  title: string;
  description?: string;
  children: ReactNode;
}) {
  return (
    <div className={styles.subsection}>
      <div className={styles.subsectionHeader}>
        <h3 className={styles.subsectionTitle}>{title}</h3>
        {description ? <p className={styles.subsectionDescription}>{description}</p> : null}
      </div>
      {children}
    </div>
  );
}

function FieldShell({
  label,
  labelId,
  htmlFor,
  hint,
  hintId,
  error,
  errorId,
  children,
}: {
  label: string;
  labelId?: string;
  htmlFor?: string;
  hint?: string;
  hintId?: string;
  error?: string;
  errorId?: string;
  children: ReactNode;
}) {
  return (
    <div className={styles.fieldShell}>
      <label id={labelId} htmlFor={htmlFor} className={styles.fieldLabel}>
        {label}
      </label>
      {children}
      {error ? (
        <div id={errorId} className="error-box">
          {error}
        </div>
      ) : null}
      {hint ? (
        <div id={hintId} className={styles.fieldHint}>
          {hint}
        </div>
      ) : null}
    </div>
  );
}

function StringListInput({
  label,
  hint,
  placeholder,
  value,
  disabled,
  onChange,
}: {
  label: string;
  hint?: string;
  placeholder?: string;
  value: string[];
  disabled: boolean;
  onChange: (value: string[]) => void;
}) {
  return (
    <FieldShell label={label} hint={hint}>
      <textarea
        className={`input ${styles.stringListInput}`}
        rows={3}
        value={value.join('\n')}
        placeholder={placeholder}
        disabled={disabled}
        onChange={(event) =>
          onChange(
            event.target.value
              .split('\n')
              .map((item) => item.trim())
              .filter(Boolean)
          )
        }
      />
    </FieldShell>
  );
}

function PluginStoreAuthEditor({
  value,
  disabled,
  onChange,
}: {
  value: PluginStoreAuthRule[];
  disabled: boolean;
  onChange: (value: PluginStoreAuthRule[]) => void;
}) {
  const { t } = useTranslation();
  const updateRule = (id: string, patch: Partial<PluginStoreAuthRule>) =>
    onChange(value.map((rule) => (rule.id === id ? { ...rule, ...patch } : rule)));
  const applyToOptions: PluginStoreAuthApplyTo[] = ['registry', 'metadata', 'artifact'];
  const authTypes: PluginStoreAuthType[] = ['bearer', 'github-token', 'basic', 'header', 'none'];

  return (
    <div className={styles.pluginAuthList}>
      {value.map((rule, index) => (
        <div key={rule.id} className={styles.pluginAuthRule}>
          <div className={styles.pluginAuthHeader}>
            <strong>
              {rule.match ||
                `${t('config_management.visual.sections.system.store_auth_rule')} ${index + 1}`}
            </strong>
            <Button
              variant="ghost"
              size="sm"
              disabled={disabled}
              onClick={() => onChange(value.filter((item) => item.id !== rule.id))}
            >
              {t('config_management.visual.common.delete')}
            </Button>
          </div>
          <SectionGrid>
            <Input
              label={t('config_management.visual.sections.system.store_auth_match')}
              value={rule.match}
              placeholder="https://example.com/private/"
              disabled={disabled}
              onChange={(event) => updateRule(rule.id, { match: event.target.value })}
            />
            <FieldShell label={t('config_management.visual.sections.system.store_auth_type')}>
              <Select
                value={rule.type}
                options={authTypes.map((type) => ({
                  value: type,
                  label: t(
                    `config_management.visual.sections.system.store_auth_type_${type.replace('-', '_')}`
                  ),
                }))}
                disabled={disabled}
                onChange={(type) => updateRule(rule.id, { type: type as PluginStoreAuthType })}
              />
            </FieldShell>
          </SectionGrid>
          <div className={styles.pluginAuthApplyTo}>
            <span className={styles.fieldLabel}>
              {t('config_management.visual.sections.system.store_auth_apply_to')}
            </span>
            <div className={styles.pluginAuthOptions}>
              {applyToOptions.map((kind) => (
                <label key={kind}>
                  <input
                    type="checkbox"
                    checked={rule.applyTo.includes(kind)}
                    disabled={disabled}
                    onChange={() =>
                      updateRule(rule.id, {
                        applyTo: rule.applyTo.includes(kind)
                          ? rule.applyTo.filter((item) => item !== kind)
                          : [...rule.applyTo, kind],
                      })
                    }
                  />
                  {t(`config_management.visual.sections.system.store_auth_apply_${kind}`)}
                </label>
              ))}
            </div>
            <div className={styles.fieldHint}>
              {t('config_management.visual.sections.system.store_auth_apply_to_hint')}
            </div>
          </div>
          {rule.type === 'bearer' || rule.type === 'github-token' ? (
            <Input
              label={t('config_management.visual.sections.system.store_auth_token_env')}
              value={rule.tokenEnv}
              placeholder="CLIPROXY_PLUGIN_STORE_TOKEN"
              disabled={disabled}
              onChange={(event) => updateRule(rule.id, { tokenEnv: event.target.value })}
            />
          ) : null}
          {rule.type === 'basic' ? (
            <SectionGrid>
              <Input
                label={t('config_management.visual.sections.system.store_auth_username_env')}
                value={rule.usernameEnv}
                disabled={disabled}
                onChange={(event) => updateRule(rule.id, { usernameEnv: event.target.value })}
              />
              <Input
                label={t('config_management.visual.sections.system.store_auth_password_env')}
                value={rule.passwordEnv}
                disabled={disabled}
                onChange={(event) => updateRule(rule.id, { passwordEnv: event.target.value })}
              />
            </SectionGrid>
          ) : null}
          {rule.type === 'header' ? (
            <SectionGrid>
              <Input
                label={t('config_management.visual.sections.system.store_auth_header_name')}
                value={rule.headerName}
                placeholder="X-Plugin-Token"
                disabled={disabled}
                onChange={(event) => updateRule(rule.id, { headerName: event.target.value })}
              />
              <Input
                label={t('config_management.visual.sections.system.store_auth_header_value_env')}
                value={rule.headerValueEnv}
                disabled={disabled}
                onChange={(event) => updateRule(rule.id, { headerValueEnv: event.target.value })}
              />
            </SectionGrid>
          ) : null}
          <label className={styles.pluginAuthOption}>
            <input
              type="checkbox"
              checked={rule.allowInsecure}
              disabled={disabled}
              onChange={(event) => updateRule(rule.id, { allowInsecure: event.target.checked })}
            />
            {t('config_management.visual.sections.system.store_auth_allow_insecure')}
          </label>
        </div>
      ))}
      {value.length === 0 ? (
        <p className={styles.fieldHint}>
          {t('config_management.visual.sections.system.store_auth_empty')}
        </p>
      ) : null}
      <Button
        variant="secondary"
        size="sm"
        disabled={disabled}
        onClick={() =>
          onChange([
            ...value,
            {
              id: makeClientId(),
              match: '',
              applyTo: [],
              type: 'bearer',
              tokenEnv: '',
              usernameEnv: '',
              passwordEnv: '',
              headerName: '',
              headerValueEnv: '',
              allowInsecure: false,
            },
          ])
        }
      >
        {t('config_management.visual.sections.system.store_auth_add')}
      </Button>
    </div>
  );
}

export function VisualConfigEditor({
  values,
  validationErrors,
  hasPayloadValidationErrors = false,
  disabled = false,
  onChange,
}: VisualConfigEditorProps) {
  const { t } = useTranslation();
  const pageTransitionLayer = usePageTransitionLayer();
  const isCurrentLayer = pageTransitionLayer ? pageTransitionLayer.isCurrentLayer : true;
  const isMobile = useMediaQuery('(max-width: 768px)');
  const isFloatingSidebar = useMediaQuery('(min-width: 1281px)');
  const shouldRenderFloatingSidebar = !isMobile && isFloatingSidebar && isCurrentLayer;
  const shouldRenderCompactSectionNav = !shouldRenderFloatingSidebar;
  const routingStrategyLabelId = useId();
  const routingStrategyHintId = `${routingStrategyLabelId}-hint`;
  const keepaliveInputId = useId();
  const keepaliveHintId = `${keepaliveInputId}-hint`;
  const keepaliveErrorId = `${keepaliveInputId}-error`;
  const nonstreamKeepaliveInputId = useId();
  const nonstreamKeepaliveHintId = `${nonstreamKeepaliveInputId}-hint`;
  const nonstreamKeepaliveErrorId = `${nonstreamKeepaliveInputId}-error`;
  const [activeSectionId, setActiveSectionId] = useState<VisualSectionId>('server');
  const workspaceRef = useRef<HTMLDivElement | null>(null);
  const sidebarAnchorRef = useRef<HTMLElement | null>(null);
  const floatingSidebarRef = useRef<HTMLDivElement | null>(null);
  const sectionRefs = useRef<Partial<Record<VisualSectionId, HTMLElement | null>>>({});
  const mobileNavScrollerRef = useRef<HTMLDivElement | null>(null);
  const mobileNavButtonRefs = useRef<Partial<Record<VisualSectionId, HTMLButtonElement | null>>>(
    {}
  );

  const isKeepaliveDisabled =
    values.streaming.keepaliveSeconds === '' || values.streaming.keepaliveSeconds === '0';
  const isNonstreamKeepaliveDisabled =
    values.streaming.nonstreamKeepaliveInterval === '' ||
    values.streaming.nonstreamKeepaliveInterval === '0';

  const portError = getValidationMessage(t, validationErrors?.port);
  const logsMaxSizeError = getValidationMessage(t, validationErrors?.logsMaxTotalSizeMb);
  const errorLogsMaxFilesError = getValidationMessage(t, validationErrors?.errorLogsMaxFiles);
  const redisUsageQueueRetentionError = getValidationMessage(
    t,
    validationErrors?.redisUsageQueueRetentionSeconds
  );
  const requestRetryError = getValidationMessage(t, validationErrors?.requestRetry);
  const maxRetryCredentialsError = getValidationMessage(t, validationErrors?.maxRetryCredentials);
  const maxRetryIntervalError = getValidationMessage(t, validationErrors?.maxRetryInterval);
  const authAutoRefreshWorkersError = getValidationMessage(
    t,
    validationErrors?.authAutoRefreshWorkers
  );
  const keepaliveError = getValidationMessage(t, validationErrors?.['streaming.keepaliveSeconds']);
  const bootstrapRetriesError = getValidationMessage(
    t,
    validationErrors?.['streaming.bootstrapRetries']
  );
  const nonstreamKeepaliveError = getValidationMessage(
    t,
    validationErrors?.['streaming.nonstreamKeepaliveInterval']
  );

  const handlePayloadDefaultRulesChange = useCallback(
    (payloadDefaultRules: PayloadRule[]) => onChange({ payloadDefaultRules }),
    [onChange]
  );
  const handlePayloadDefaultRawRulesChange = useCallback(
    (payloadDefaultRawRules: PayloadRule[]) => onChange({ payloadDefaultRawRules }),
    [onChange]
  );
  const handlePayloadOverrideRulesChange = useCallback(
    (payloadOverrideRules: PayloadRule[]) => onChange({ payloadOverrideRules }),
    [onChange]
  );
  const handlePayloadOverrideRawRulesChange = useCallback(
    (payloadOverrideRawRules: PayloadRule[]) => onChange({ payloadOverrideRawRules }),
    [onChange]
  );
  const handlePayloadFilterRulesChange = useCallback(
    (payloadFilterRules: PayloadFilterRule[]) => onChange({ payloadFilterRules }),
    [onChange]
  );

  const countErrors = useCallback(
    (fields: VisualConfigFieldPath[]) =>
      fields.reduce((total, field) => total + (validationErrors?.[field] ? 1 : 0), 0),
    [validationErrors]
  );

  const sections = useMemo<VisualSection[]>(
    () => [
      {
        id: 'server',
        title: t('config_management.visual.sections.server.title'),
        description: t('config_management.visual.sections.server.description'),
        icon: IconSettings,
        errorCount: countErrors(['port']),
      },
      {
        id: 'tls',
        title: t('config_management.visual.sections.tls.title'),
        description: t('config_management.visual.sections.tls.description'),
        icon: IconShield,
        errorCount: 0,
      },
      {
        id: 'remote',
        title: t('config_management.visual.sections.remote.title'),
        description: t('config_management.visual.sections.remote.description'),
        icon: IconSatellite,
        errorCount: 0,
      },
      {
        id: 'auth',
        title: t('config_management.visual.sections.auth.title'),
        description: t('config_management.visual.sections.auth.description'),
        icon: IconKey,
        errorCount: 0,
      },
      {
        id: 'system',
        title: t('config_management.visual.sections.system.title'),
        description: t('config_management.visual.sections.system.description'),
        icon: IconDiamond,
        errorCount: countErrors([
          'logsMaxTotalSizeMb',
          'errorLogsMaxFiles',
          'redisUsageQueueRetentionSeconds',
        ]),
      },
      {
        id: 'network',
        title: t('config_management.visual.sections.network.title'),
        description: t('config_management.visual.sections.network.description'),
        icon: IconTrendingUp,
        errorCount: countErrors([
          'requestRetry',
          'maxRetryCredentials',
          'maxRetryInterval',
          'authAutoRefreshWorkers',
        ]),
      },
      {
        id: 'advanced',
        title: t('config_management.visual.sections.advanced.title'),
        description: t('config_management.visual.sections.advanced.description'),
        icon: IconShield,
        errorCount: 0,
      },
      {
        id: 'quota',
        title: t('config_management.visual.sections.quota.title'),
        description: t('config_management.visual.sections.quota.description'),
        icon: IconTimer,
        errorCount: 0,
      },
      {
        id: 'streaming',
        title: t('config_management.visual.sections.streaming.title'),
        description: t('config_management.visual.sections.streaming.description'),
        icon: IconSatellite,
        errorCount: countErrors([
          'streaming.keepaliveSeconds',
          'streaming.bootstrapRetries',
          'streaming.nonstreamKeepaliveInterval',
        ]),
      },
      {
        id: 'payload',
        title: t('config_management.visual.sections.payload.title'),
        description: t('config_management.visual.sections.payload.description'),
        icon: IconCode,
        errorCount: hasPayloadValidationErrors ? 1 : 0,
      },
    ],
    [countErrors, hasPayloadValidationErrors, t]
  );

  useEffect(() => {
    if (!isCurrentLayer) return undefined;
    if (typeof IntersectionObserver === 'undefined') return undefined;

    const observer = new IntersectionObserver(
      (entries) => {
        const visibleEntries = entries
          .filter((entry) => entry.isIntersecting)
          .sort((left, right) => right.intersectionRatio - left.intersectionRatio);

        if (visibleEntries.length === 0) return;
        setActiveSectionId(visibleEntries[0].target.id as VisualSectionId);
      },
      {
        rootMargin: '-18% 0px -58% 0px',
        threshold: [0.12, 0.3, 0.55],
      }
    );

    for (const section of sections) {
      const element = sectionRefs.current[section.id];
      if (element) observer.observe(element);
    }

    return () => observer.disconnect();
  }, [isCurrentLayer, sections]);

  useEffect(() => {
    if (!isCurrentLayer || !shouldRenderCompactSectionNav) return;
    const scroller = mobileNavScrollerRef.current;
    const button = mobileNavButtonRefs.current[activeSectionId];
    if (!scroller || !button) return;

    const scrollerRect = scroller.getBoundingClientRect();
    const buttonRect = button.getBoundingClientRect();
    const centeredLeft =
      scroller.scrollLeft +
      (buttonRect.left - scrollerRect.left) -
      (scroller.clientWidth - buttonRect.width) / 2;
    const maxScrollLeft = Math.max(scroller.scrollWidth - scroller.clientWidth, 0);
    const targetLeft = Math.min(Math.max(centeredLeft, 0), maxScrollLeft);

    scroller.scrollTo({
      left: targetLeft,
      behavior: 'smooth',
    });
  }, [activeSectionId, isCurrentLayer, shouldRenderCompactSectionNav]);

  const handleSectionJump = useCallback((sectionId: VisualSectionId) => {
    setActiveSectionId(sectionId);
    sectionRefs.current[sectionId]?.scrollIntoView({ behavior: 'smooth', block: 'start' });
  }, []);

  useLayoutEffect(() => {
    const floatingElement = floatingSidebarRef.current;
    const anchorElement = sidebarAnchorRef.current;
    const workspaceElement = workspaceRef.current;
    if (!floatingElement) return undefined;

    const clearFloatingStyles = () => {
      floatingElement.style.removeProperty('transform');
      floatingElement.style.removeProperty('width');
      floatingElement.style.removeProperty('max-height');
      floatingElement.style.removeProperty('opacity');
      floatingElement.style.removeProperty('pointer-events');
    };

    if (!shouldRenderFloatingSidebar || !anchorElement || !workspaceElement) {
      clearFloatingStyles();
      return undefined;
    }

    /* ---- Cache header height – recomputed only on resize ---- */
    const computeHeaderHeight = () => {
      const header = document.querySelector('.main-header') as HTMLElement | null;
      if (header) return header.getBoundingClientRect().height;

      const raw = getComputedStyle(document.documentElement).getPropertyValue('--header-height');
      const parsed = Number.parseFloat(raw);
      return Number.isFinite(parsed) ? parsed : 64;
    };
    let headerHeight = computeHeaderHeight();

    /* ---- Cache content scroller – resolved once ---- */
    const contentScroller = document.querySelector('.content') as HTMLElement | null;

    /* ---- Cache floating height from previous frame ---- */
    let cachedFloatingHeight = floatingElement.getBoundingClientRect().height || 200;

    let frameId = 0;

    const updateFloatingPosition = () => {
      frameId = 0;

      const anchorRect = anchorElement.getBoundingClientRect();
      const workspaceRect = workspaceElement.getBoundingClientRect();
      const stickyTop = headerHeight + 4;
      const viewportPadding = 16;
      const maxTop = workspaceRect.bottom - cachedFloatingHeight;
      const unclampedTop = Math.min(Math.max(anchorRect.top, stickyTop), maxTop);
      const top = Math.max(unclampedTop, viewportPadding);
      const left = Math.max(anchorRect.left, viewportPadding);
      const width = Math.max(
        Math.min(anchorRect.width, window.innerWidth - left - viewportPadding),
        220
      );
      const maxHeight = Math.max(window.innerHeight - top - viewportPadding, 160);
      const isVisible =
        workspaceRect.bottom > stickyTop + 24 && anchorRect.top < window.innerHeight;

      floatingElement.style.transform = `translate3d(${left}px, ${top}px, 0)`;
      floatingElement.style.width = `${width}px`;
      floatingElement.style.maxHeight = `${maxHeight}px`;
      floatingElement.style.opacity = isVisible ? '1' : '0';
      floatingElement.style.pointerEvents = isVisible ? 'auto' : 'none';
    };

    const requestPositionUpdate = () => {
      if (frameId) cancelAnimationFrame(frameId);
      frameId = requestAnimationFrame(updateFloatingPosition);
    };

    const handleResize = () => {
      headerHeight = computeHeaderHeight();
      cachedFloatingHeight = floatingElement.getBoundingClientRect().height || cachedFloatingHeight;
      requestPositionUpdate();
    };

    requestPositionUpdate();

    window.addEventListener('resize', handleResize);
    window.addEventListener('scroll', requestPositionUpdate, { passive: true });
    contentScroller?.addEventListener('scroll', requestPositionUpdate, { passive: true });

    const resizeObserver =
      typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(requestPositionUpdate);
    resizeObserver?.observe(anchorElement);
    resizeObserver?.observe(workspaceElement);

    return () => {
      if (frameId) cancelAnimationFrame(frameId);
      resizeObserver?.disconnect();
      window.removeEventListener('resize', handleResize);
      window.removeEventListener('scroll', requestPositionUpdate);
      contentScroller?.removeEventListener('scroll', requestPositionUpdate);
      clearFloatingStyles();
    };
  }, [shouldRenderFloatingSidebar]);

  const navContent = (
    <div className={styles.navList}>
      {sections.map((section) => {
        const Icon = section.icon;

        return (
          <button
            key={section.id}
            type="button"
            className={`${styles.navButton} ${
              activeSectionId === section.id ? styles.navButtonActive : ''
            }`}
            onClick={() => handleSectionJump(section.id)}
          >
            <span className={styles.navIcon}>
              <Icon size={14} />
            </span>
            <span className={styles.navMain}>
              <span className={styles.navHeadingRow}>
                <span className={styles.navLabelWrap}>
                  <span className={styles.navLabel}>{section.title}</span>
                </span>
                {section.errorCount > 0 ? (
                  <span className={styles.navBadge} aria-hidden="true">
                    {section.errorCount}
                  </span>
                ) : null}
              </span>
              <span className={styles.navDescription}>{section.description}</span>
            </span>
          </button>
        );
      })}
    </div>
  );

  return (
    <div className={styles.visualEditor}>
      <div ref={workspaceRef} className={styles.workspace}>
        {shouldRenderCompactSectionNav ? (
          <div className={styles.mobileSectionNav}>
            <div
              ref={mobileNavScrollerRef}
              className={styles.mobileSectionNavScroller}
              aria-label={t('config_management.visual.quick_jump', { defaultValue: '快速跳转' })}
            >
              {sections.map((section) => {
                const Icon = section.icon;

                return (
                  <button
                    key={section.id}
                    ref={(node) => {
                      mobileNavButtonRefs.current[section.id] = node;
                    }}
                    type="button"
                    className={`${styles.mobileSectionNavButton} ${
                      activeSectionId === section.id ? styles.mobileSectionNavButtonActive : ''
                    }`}
                    onClick={() => handleSectionJump(section.id)}
                  >
                    <span className={styles.mobileSectionNavIcon}>
                      <Icon size={13} />
                    </span>
                    <span className={styles.mobileSectionNavLabel}>{section.title}</span>
                    {section.errorCount > 0 ? (
                      <span className={styles.mobileSectionNavBadge} aria-hidden="true">
                        {section.errorCount}
                      </span>
                    ) : null}
                  </button>
                );
              })}
            </div>
          </div>
        ) : null}

        {shouldRenderFloatingSidebar ? (
          <aside ref={sidebarAnchorRef} className={styles.sidebar}>
            <div className={styles.sidebarPlaceholder} aria-hidden="true" />
          </aside>
        ) : null}

        <div className={styles.sections}>
          <ConfigSection
            id="server"
            ref={(node) => {
              sectionRefs.current.server = node;
            }}
            icon={<IconSettings size={16} />}
            title={t('config_management.visual.sections.server.title')}
            description={t('config_management.visual.sections.server.description')}
          >
            <SectionGrid>
              <Input
                label={t('config_management.visual.sections.server.host')}
                placeholder="0.0.0.0"
                value={values.host}
                onChange={(e) => onChange({ host: e.target.value })}
                disabled={disabled}
              />
              <Input
                label={t('config_management.visual.sections.server.port')}
                type="number"
                placeholder="8317"
                value={values.port}
                onChange={(e) => onChange({ port: e.target.value })}
                disabled={disabled}
                error={portError}
              />
            </SectionGrid>
          </ConfigSection>

          <ConfigSection
            id="tls"
            ref={(node) => {
              sectionRefs.current.tls = node;
            }}
            icon={<IconShield size={16} />}
            title={t('config_management.visual.sections.tls.title')}
            description={t('config_management.visual.sections.tls.description')}
          >
            <SectionStack>
              <ToggleRow
                title={t('config_management.visual.sections.tls.enable')}
                description={t('config_management.visual.sections.tls.enable_desc')}
                checked={values.tlsEnable}
                disabled={disabled}
                onChange={(tlsEnable) => onChange({ tlsEnable })}
              />

              {values.tlsEnable ? (
                <>
                  <Divider />
                  <SectionGrid>
                    <Input
                      label={t('config_management.visual.sections.tls.cert')}
                      placeholder="/path/to/cert.pem"
                      value={values.tlsCert}
                      onChange={(e) => onChange({ tlsCert: e.target.value })}
                      disabled={disabled}
                    />
                    <Input
                      label={t('config_management.visual.sections.tls.key')}
                      placeholder="/path/to/key.pem"
                      value={values.tlsKey}
                      onChange={(e) => onChange({ tlsKey: e.target.value })}
                      disabled={disabled}
                    />
                  </SectionGrid>
                </>
              ) : null}
            </SectionStack>
          </ConfigSection>

          <ConfigSection
            id="remote"
            ref={(node) => {
              sectionRefs.current.remote = node;
            }}
            icon={<IconSatellite size={16} />}
            title={t('config_management.visual.sections.remote.title')}
            description={t('config_management.visual.sections.remote.description')}
          >
            <SectionStack>
              <ToggleRow
                title={t('config_management.visual.sections.remote.allow_remote')}
                description={t('config_management.visual.sections.remote.allow_remote_desc')}
                checked={values.rmAllowRemote}
                disabled={disabled}
                onChange={(rmAllowRemote) => onChange({ rmAllowRemote })}
              />
              <ToggleRow
                title={t('config_management.visual.sections.remote.disable_panel')}
                description={t('config_management.visual.sections.remote.disable_panel_desc')}
                checked={values.rmDisableControlPanel}
                disabled={disabled}
                onChange={(rmDisableControlPanel) => onChange({ rmDisableControlPanel })}
              />
              <ToggleRow
                title={t('config_management.visual.sections.remote.disable_auto_update_panel')}
                description={t(
                  'config_management.visual.sections.remote.disable_auto_update_panel_desc'
                )}
                checked={values.rmDisableAutoUpdatePanel}
                disabled={disabled}
                onChange={(rmDisableAutoUpdatePanel) => onChange({ rmDisableAutoUpdatePanel })}
              />
              <SectionGrid>
                <Input
                  label={t('config_management.visual.sections.remote.secret_key')}
                  type="password"
                  placeholder={t('config_management.visual.sections.remote.secret_key_placeholder')}
                  value={values.rmSecretKey}
                  onChange={(e) => onChange({ rmSecretKey: e.target.value })}
                  disabled={disabled}
                />
                <Input
                  label={t('config_management.visual.sections.remote.panel_repo')}
                  placeholder="https://github.com/router-for-me/Cli-Proxy-API-Management-Center"
                  value={values.rmPanelRepo}
                  onChange={(e) => onChange({ rmPanelRepo: e.target.value })}
                  disabled={disabled}
                />
              </SectionGrid>
            </SectionStack>
          </ConfigSection>

          <ConfigSection
            id="auth"
            ref={(node) => {
              sectionRefs.current.auth = node;
            }}
            icon={<IconKey size={16} />}
            title={t('config_management.visual.sections.auth.title')}
            description={t('config_management.visual.sections.auth.description')}
          >
            <SectionStack>
              <Input
                label={t('config_management.visual.sections.auth.auth_dir')}
                placeholder="~/.cli-proxy-api"
                value={values.authDir}
                onChange={(e) => onChange({ authDir: e.target.value })}
                disabled={disabled}
                hint={t('config_management.visual.sections.auth.auth_dir_hint')}
              />
              <div className={styles.subsection}>
                <div className={styles.helperText}>
                  API Key 已迁移至“企业 Key 管理”面板统一维护，Config 面板不再提供新增/编辑入口。
                </div>
              </div>
            </SectionStack>
          </ConfigSection>

          <ConfigSection
            id="system"
            ref={(node) => {
              sectionRefs.current.system = node;
            }}
            icon={<IconDiamond size={16} />}
            title={t('config_management.visual.sections.system.title')}
            description={t('config_management.visual.sections.system.description')}
          >
            <SectionStack>
              <SectionGrid>
                <ToggleRow
                  title={t('config_management.visual.sections.system.debug')}
                  description={t('config_management.visual.sections.system.debug_desc')}
                  checked={values.debug}
                  disabled={disabled}
                  onChange={(debug) => onChange({ debug })}
                />
                <ToggleRow
                  title={t('config_management.visual.sections.system.commercial_mode')}
                  description={t('config_management.visual.sections.system.commercial_mode_desc')}
                  checked={values.commercialMode}
                  disabled={disabled}
                  onChange={(commercialMode) => onChange({ commercialMode })}
                />
                <ToggleRow
                  title={t('config_management.visual.sections.system.usage_statistics_enabled')}
                  description={t(
                    'config_management.visual.sections.system.usage_statistics_enabled_desc'
                  )}
                  checked={values.usageStatisticsEnabled}
                  disabled={disabled}
                  onChange={(usageStatisticsEnabled) => onChange({ usageStatisticsEnabled })}
                />
                <ToggleRow
                  title={t('config_management.visual.sections.system.logging_to_file')}
                  description={t('config_management.visual.sections.system.logging_to_file_desc')}
                  checked={values.loggingToFile}
                  disabled={disabled}
                  onChange={(loggingToFile) => onChange({ loggingToFile })}
                />
                <ToggleRow
                  title={t('config_management.visual.sections.system.plugins_enabled')}
                  description={t('config_management.visual.sections.system.plugins_enabled_desc')}
                  checked={values.pluginsEnabled}
                  disabled={disabled}
                  onChange={(pluginsEnabled) => onChange({ pluginsEnabled })}
                />
              </SectionGrid>

              <SectionSubsection
                title={t('config_management.visual.sections.system.plugin_store_sources')}
                description={t(
                  'config_management.visual.sections.system.plugin_store_sources_desc'
                )}
              >
                <StringListInput
                  label={t('config_management.visual.sections.system.plugin_store_sources_label')}
                  placeholder={t(
                    'config_management.visual.sections.system.plugin_store_sources_placeholder'
                  )}
                  hint={t('config_management.visual.sections.system.plugin_store_sources_hint')}
                  value={values.pluginStoreSources}
                  disabled={disabled}
                  onChange={(pluginStoreSources) => onChange({ pluginStoreSources })}
                />
              </SectionSubsection>

              <SectionSubsection
                title={t('config_management.visual.sections.system.plugin_store_auth')}
                description={t('config_management.visual.sections.system.plugin_store_auth_desc')}
              >
                <div className={styles.fieldHint}>
                  {t('config_management.visual.sections.system.plugin_store_auth_hint')}
                </div>
                <PluginStoreAuthEditor
                  value={values.pluginStoreAuth}
                  disabled={disabled}
                  onChange={(pluginStoreAuth) => onChange({ pluginStoreAuth })}
                />
              </SectionSubsection>

              <SectionGrid>
                <Input
                  label={t('config_management.visual.sections.system.logs_max_size')}
                  type="number"
                  placeholder="0"
                  value={values.logsMaxTotalSizeMb}
                  onChange={(e) => onChange({ logsMaxTotalSizeMb: e.target.value })}
                  disabled={disabled}
                  error={logsMaxSizeError}
                />
                <Input
                  label={t('config_management.visual.sections.system.error_logs_max_files')}
                  type="number"
                  min="0"
                  placeholder="10"
                  value={values.errorLogsMaxFiles}
                  onChange={(e) => onChange({ errorLogsMaxFiles: e.target.value })}
                  disabled={disabled}
                  error={errorLogsMaxFilesError}
                />
                <Input
                  label={t('config_management.visual.sections.system.redis_usage_queue_retention')}
                  type="number"
                  min="0"
                  max="3600"
                  placeholder="60"
                  value={values.redisUsageQueueRetentionSeconds}
                  onChange={(e) => onChange({ redisUsageQueueRetentionSeconds: e.target.value })}
                  disabled={disabled}
                  hint={t(
                    'config_management.visual.sections.system.redis_usage_queue_retention_hint'
                  )}
                  error={redisUsageQueueRetentionError}
                />
              </SectionGrid>
            </SectionStack>
          </ConfigSection>

          <ConfigSection
            id="network"
            ref={(node) => {
              sectionRefs.current.network = node;
            }}
            icon={<IconTrendingUp size={16} />}
            title={t('config_management.visual.sections.network.title')}
            description={t('config_management.visual.sections.network.description')}
          >
            <SectionStack>
              <SectionGrid>
                <Input
                  label={t('config_management.visual.sections.network.proxy_url')}
                  placeholder="socks5://user:pass@127.0.0.1:1080/"
                  value={values.proxyUrl}
                  onChange={(e) => onChange({ proxyUrl: e.target.value })}
                  disabled={disabled}
                />
                <Input
                  label={t('config_management.visual.sections.network.request_retry')}
                  type="number"
                  placeholder="3"
                  value={values.requestRetry}
                  onChange={(e) => onChange({ requestRetry: e.target.value })}
                  disabled={disabled}
                  error={requestRetryError}
                />
                <Input
                  label={t('config_management.visual.sections.network.max_retry_credentials')}
                  type="number"
                  placeholder="0"
                  value={values.maxRetryCredentials}
                  onChange={(e) => onChange({ maxRetryCredentials: e.target.value })}
                  disabled={disabled}
                  hint={t('config_management.visual.sections.network.max_retry_credentials_hint')}
                  error={maxRetryCredentialsError}
                />
                <Input
                  label={t('config_management.visual.sections.network.max_retry_interval')}
                  type="number"
                  placeholder="30"
                  value={values.maxRetryInterval}
                  onChange={(e) => onChange({ maxRetryInterval: e.target.value })}
                  disabled={disabled}
                  error={maxRetryIntervalError}
                />
                <Input
                  label={t('config_management.visual.sections.network.auth_auto_refresh_workers')}
                  type="number"
                  placeholder="16"
                  value={values.authAutoRefreshWorkers}
                  onChange={(e) => onChange({ authAutoRefreshWorkers: e.target.value })}
                  disabled={disabled}
                  hint={t(
                    'config_management.visual.sections.network.auth_auto_refresh_workers_hint'
                  )}
                  error={authAutoRefreshWorkersError}
                />
                <FieldShell
                  label={t('config_management.visual.sections.network.routing_strategy')}
                  labelId={routingStrategyLabelId}
                  hint={t('config_management.visual.sections.network.routing_strategy_hint')}
                  hintId={routingStrategyHintId}
                >
                  <Select
                    value={values.routingStrategy}
                    options={[
                      {
                        value: 'round-robin',
                        label: t('config_management.visual.sections.network.strategy_round_robin'),
                      },
                      {
                        value: 'weighted-round-robin',
                        label: t(
                          'config_management.visual.sections.network.strategy_weighted_round_robin'
                        ),
                      },
                      {
                        value: 'fill-first',
                        label: t('config_management.visual.sections.network.strategy_fill_first'),
                      },
                    ]}
                    id={`${routingStrategyLabelId}-select`}
                    disabled={disabled}
                    ariaLabelledBy={routingStrategyLabelId}
                    ariaDescribedBy={routingStrategyHintId}
                    onChange={(nextValue) =>
                      onChange({
                        routingStrategy: nextValue as VisualConfigValues['routingStrategy'],
                      })
                    }
                  />
                </FieldShell>
                <Input
                  label={t('config_management.visual.sections.network.session_affinity_ttl')}
                  placeholder="1h"
                  value={values.routingSessionAffinityTTL}
                  onChange={(e) => onChange({ routingSessionAffinityTTL: e.target.value })}
                  disabled={disabled}
                />
                <FieldShell
                  label={t('config_management.visual.sections.network.disable_image_generation')}
                  hint={t(
                    'config_management.visual.sections.network.disable_image_generation_hint'
                  )}
                >
                  <Select
                    value={values.disableImageGeneration}
                    options={(['false', 'true', 'chat', 'passthrough'] as const).map((mode) => ({
                      value: mode,
                      label: t(
                        `config_management.visual.sections.network.disable_image_generation_${mode}`
                      ),
                    }))}
                    disabled={disabled}
                    onChange={(disableImageGeneration) =>
                      onChange({
                        disableImageGeneration:
                          disableImageGeneration as VisualConfigValues['disableImageGeneration'],
                      })
                    }
                  />
                </FieldShell>
                <Input
                  label={t('config_management.visual.sections.network.gpt_image_2_base_model')}
                  placeholder="gpt-image-2-mainline"
                  value={values.gptImage2BaseModel}
                  onChange={(e) => onChange({ gptImage2BaseModel: e.target.value })}
                  disabled={disabled}
                  hint={t('config_management.visual.sections.network.gpt_image_2_base_model_hint')}
                />
              </SectionGrid>

              <SectionGrid>
                <ToggleRow
                  title={t('config_management.visual.sections.network.force_model_prefix')}
                  description={t(
                    'config_management.visual.sections.network.force_model_prefix_desc'
                  )}
                  checked={values.forceModelPrefix}
                  disabled={disabled}
                  onChange={(forceModelPrefix) => onChange({ forceModelPrefix })}
                />
                <ToggleRow
                  title={t('config_management.visual.sections.network.session_affinity')}
                  checked={values.routingSessionAffinity}
                  disabled={disabled}
                  onChange={(routingSessionAffinity) => onChange({ routingSessionAffinity })}
                />
                <ToggleRow
                  title={t('config_management.visual.sections.network.ws_auth')}
                  description={t('config_management.visual.sections.network.ws_auth_desc')}
                  checked={values.wsAuth}
                  disabled={disabled}
                  onChange={(wsAuth) => onChange({ wsAuth })}
                />
                <ToggleRow
                  title={t('config_management.visual.sections.network.passthrough_headers')}
                  description={t(
                    'config_management.visual.sections.network.passthrough_headers_desc'
                  )}
                  checked={values.passthroughHeaders}
                  disabled={disabled}
                  onChange={(passthroughHeaders) => onChange({ passthroughHeaders })}
                />
                <ToggleRow
                  title={t('config_management.visual.sections.network.disable_cooling')}
                  description={t('config_management.visual.sections.network.disable_cooling_desc')}
                  checked={values.disableCooling}
                  disabled={disabled}
                  onChange={(disableCooling) => onChange({ disableCooling })}
                />
              </SectionGrid>
            </SectionStack>
          </ConfigSection>

          <ConfigSection
            id="advanced"
            ref={(node) => {
              sectionRefs.current.advanced = node;
            }}
            icon={<IconShield size={16} />}
            title={t('config_management.visual.sections.advanced.title')}
            description={t('config_management.visual.sections.advanced.description')}
          >
            <SectionStack>
              <SectionSubsection
                title={t('config_management.visual.sections.system.antigravity_sensitive_words')}
                description={t(
                  'config_management.visual.sections.system.antigravity_sensitive_words_desc'
                )}
              >
                <StringListInput
                  label={t(
                    'config_management.visual.sections.system.antigravity_sensitive_words_label'
                  )}
                  placeholder={t(
                    'config_management.visual.sections.system.antigravity_sensitive_words_placeholder'
                  )}
                  hint={t(
                    'config_management.visual.sections.system.antigravity_sensitive_words_hint'
                  )}
                  value={values.antigravitySensitiveWords}
                  disabled={disabled}
                  onChange={(antigravitySensitiveWords) => onChange({ antigravitySensitiveWords })}
                />
              </SectionSubsection>
              <SectionSubsection
                title={t('config_management.visual.sections.system.devin_sensitive_words')}
                description={t(
                  'config_management.visual.sections.system.devin_sensitive_words_desc'
                )}
              >
                <StringListInput
                  label={t('config_management.visual.sections.system.devin_sensitive_words_label')}
                  placeholder={t(
                    'config_management.visual.sections.system.devin_sensitive_words_placeholder'
                  )}
                  hint={t('config_management.visual.sections.system.devin_sensitive_words_hint')}
                  value={values.devinSensitiveWords}
                  disabled={disabled}
                  onChange={(devinSensitiveWords) => onChange({ devinSensitiveWords })}
                />
              </SectionSubsection>
              <SectionSubsection
                title={t('config_management.visual.sections.system.signature_title')}
              >
                <ToggleRow
                  title={t('config_management.visual.sections.system.antigravity_signature_cache')}
                  description={t(
                    'config_management.visual.sections.system.antigravity_signature_cache_desc'
                  )}
                  checked={values.antigravitySignatureCacheEnabled}
                  disabled={disabled}
                  onChange={(antigravitySignatureCacheEnabled) =>
                    onChange({ antigravitySignatureCacheEnabled })
                  }
                />
                <ToggleRow
                  title={t('config_management.visual.sections.system.antigravity_signature_strict')}
                  description={t(
                    'config_management.visual.sections.system.antigravity_signature_strict_desc'
                  )}
                  checked={values.antigravitySignatureBypassStrict}
                  disabled={disabled}
                  onChange={(antigravitySignatureBypassStrict) =>
                    onChange({ antigravitySignatureBypassStrict })
                  }
                />
              </SectionSubsection>
              <SectionSubsection
                title={t('config_management.visual.sections.headers.claude_title')}
                description={t('config_management.visual.sections.headers.description')}
              >
                <SectionGrid>
                  <Input
                    label={t('config_management.visual.sections.headers.user_agent')}
                    value={values.claudeHeaderUserAgent}
                    disabled={disabled}
                    onChange={(event) => onChange({ claudeHeaderUserAgent: event.target.value })}
                  />
                  <Input
                    label={t('config_management.visual.sections.headers.package_version')}
                    value={values.claudeHeaderPackageVersion}
                    disabled={disabled}
                    onChange={(event) =>
                      onChange({ claudeHeaderPackageVersion: event.target.value })
                    }
                  />
                  <Input
                    label={t('config_management.visual.sections.headers.runtime_version')}
                    value={values.claudeHeaderRuntimeVersion}
                    disabled={disabled}
                    onChange={(event) =>
                      onChange({ claudeHeaderRuntimeVersion: event.target.value })
                    }
                  />
                  <Input
                    label={t('config_management.visual.sections.headers.os')}
                    value={values.claudeHeaderOs}
                    disabled={disabled}
                    onChange={(event) => onChange({ claudeHeaderOs: event.target.value })}
                  />
                  <Input
                    label={t('config_management.visual.sections.headers.arch')}
                    value={values.claudeHeaderArch}
                    disabled={disabled}
                    onChange={(event) => onChange({ claudeHeaderArch: event.target.value })}
                  />
                  <Input
                    label={t('config_management.visual.sections.headers.timeout')}
                    type="number"
                    value={values.claudeHeaderTimeout}
                    disabled={disabled}
                    onChange={(event) => onChange({ claudeHeaderTimeout: event.target.value })}
                  />
                </SectionGrid>
                <ToggleRow
                  title={t('config_management.visual.sections.headers.stabilize_device')}
                  description={t('config_management.visual.sections.headers.stabilize_device_desc')}
                  checked={values.claudeHeaderStabilizeDeviceProfile}
                  disabled={disabled}
                  onChange={(claudeHeaderStabilizeDeviceProfile) =>
                    onChange({ claudeHeaderStabilizeDeviceProfile })
                  }
                />
              </SectionSubsection>
              <SectionSubsection title={t('config_management.visual.sections.headers.codex_title')}>
                <SectionGrid>
                  <Input
                    label={t('config_management.visual.sections.headers.user_agent')}
                    value={values.codexHeaderUserAgent}
                    disabled={disabled}
                    onChange={(event) => onChange({ codexHeaderUserAgent: event.target.value })}
                  />
                  <Input
                    label={t('config_management.visual.sections.headers.beta_features')}
                    value={values.codexHeaderBetaFeatures}
                    disabled={disabled}
                    onChange={(event) => onChange({ codexHeaderBetaFeatures: event.target.value })}
                  />
                </SectionGrid>
              </SectionSubsection>
            </SectionStack>
          </ConfigSection>

          <ConfigSection
            id="quota"
            ref={(node) => {
              sectionRefs.current.quota = node;
            }}
            icon={<IconTimer size={16} />}
            title={t('config_management.visual.sections.quota.title')}
            description={t('config_management.visual.sections.quota.description')}
          >
            <SectionGrid>
              <ToggleRow
                title={t('config_management.visual.sections.quota.switch_project')}
                description={t('config_management.visual.sections.quota.switch_project_desc')}
                checked={values.quotaSwitchProject}
                disabled={disabled}
                onChange={(quotaSwitchProject) => onChange({ quotaSwitchProject })}
              />
              <ToggleRow
                title={t('config_management.visual.sections.quota.switch_preview_model')}
                description={t('config_management.visual.sections.quota.switch_preview_model_desc')}
                checked={values.quotaSwitchPreviewModel}
                disabled={disabled}
                onChange={(quotaSwitchPreviewModel) => onChange({ quotaSwitchPreviewModel })}
              />
              <ToggleRow
                title={t('config_management.visual.sections.quota.antigravity_credits')}
                description={t('config_management.visual.sections.quota.antigravity_credits_desc')}
                checked={values.quotaAntigravityCredits}
                disabled={disabled}
                onChange={(quotaAntigravityCredits) => onChange({ quotaAntigravityCredits })}
              />
            </SectionGrid>
          </ConfigSection>

          <ConfigSection
            id="streaming"
            ref={(node) => {
              sectionRefs.current.streaming = node;
            }}
            icon={<IconSatellite size={16} />}
            title={t('config_management.visual.sections.streaming.title')}
            description={t('config_management.visual.sections.streaming.description')}
          >
            <SectionStack>
              <SectionGrid>
                <FieldShell
                  label={t('config_management.visual.sections.streaming.keepalive_seconds')}
                  htmlFor={keepaliveInputId}
                  hint={t('config_management.visual.sections.streaming.keepalive_hint')}
                  hintId={keepaliveHintId}
                  error={keepaliveError}
                  errorId={keepaliveErrorId}
                >
                  <div className={styles.fieldControl}>
                    <input
                      id={keepaliveInputId}
                      className="input"
                      type="number"
                      placeholder="0"
                      value={values.streaming.keepaliveSeconds}
                      onChange={(e) =>
                        onChange({
                          streaming: {
                            ...values.streaming,
                            keepaliveSeconds: e.target.value,
                          },
                        })
                      }
                      disabled={disabled}
                    />
                    {isKeepaliveDisabled ? (
                      <span className={styles.inlinePill}>
                        {t('config_management.visual.sections.streaming.disabled')}
                      </span>
                    ) : null}
                  </div>
                </FieldShell>

                <Input
                  label={t('config_management.visual.sections.streaming.bootstrap_retries')}
                  type="number"
                  placeholder="1"
                  value={values.streaming.bootstrapRetries}
                  onChange={(e) =>
                    onChange({
                      streaming: {
                        ...values.streaming,
                        bootstrapRetries: e.target.value,
                      },
                    })
                  }
                  disabled={disabled}
                  hint={t('config_management.visual.sections.streaming.bootstrap_hint')}
                  error={bootstrapRetriesError}
                />
              </SectionGrid>

              <SectionGrid>
                <FieldShell
                  label={t('config_management.visual.sections.streaming.nonstream_keepalive')}
                  htmlFor={nonstreamKeepaliveInputId}
                  hint={t('config_management.visual.sections.streaming.nonstream_keepalive_hint')}
                  hintId={nonstreamKeepaliveHintId}
                  error={nonstreamKeepaliveError}
                  errorId={nonstreamKeepaliveErrorId}
                >
                  <div className={styles.fieldControl}>
                    <input
                      id={nonstreamKeepaliveInputId}
                      className="input"
                      type="number"
                      placeholder="0"
                      value={values.streaming.nonstreamKeepaliveInterval}
                      onChange={(e) =>
                        onChange({
                          streaming: {
                            ...values.streaming,
                            nonstreamKeepaliveInterval: e.target.value,
                          },
                        })
                      }
                      disabled={disabled}
                    />
                    {isNonstreamKeepaliveDisabled ? (
                      <span className={styles.inlinePill}>
                        {t('config_management.visual.sections.streaming.disabled')}
                      </span>
                    ) : null}
                  </div>
                </FieldShell>
              </SectionGrid>
            </SectionStack>
          </ConfigSection>

          <ConfigSection
            id="payload"
            ref={(node) => {
              sectionRefs.current.payload = node;
            }}
            icon={<IconCode size={16} />}
            title={t('config_management.visual.sections.payload.title')}
            description={t('config_management.visual.sections.payload.description')}
          >
            <SectionStack>
              <SectionSubsection
                title={t('config_management.visual.sections.payload.default_rules')}
                description={t('config_management.visual.sections.payload.default_rules_desc')}
              >
                <PayloadRulesEditor
                  value={values.payloadDefaultRules}
                  disabled={disabled}
                  onChange={handlePayloadDefaultRulesChange}
                />
              </SectionSubsection>

              <SectionSubsection
                title={t('config_management.visual.sections.payload.default_raw_rules')}
                description={t('config_management.visual.sections.payload.default_raw_rules_desc')}
              >
                <PayloadRulesEditor
                  value={values.payloadDefaultRawRules}
                  disabled={disabled}
                  rawJsonValues
                  onChange={handlePayloadDefaultRawRulesChange}
                />
              </SectionSubsection>

              <SectionSubsection
                title={t('config_management.visual.sections.payload.override_rules')}
                description={t('config_management.visual.sections.payload.override_rules_desc')}
              >
                <PayloadRulesEditor
                  value={values.payloadOverrideRules}
                  disabled={disabled}
                  protocolFirst
                  onChange={handlePayloadOverrideRulesChange}
                />
              </SectionSubsection>

              <SectionSubsection
                title={t('config_management.visual.sections.payload.override_raw_rules')}
                description={t('config_management.visual.sections.payload.override_raw_rules_desc')}
              >
                <PayloadRulesEditor
                  value={values.payloadOverrideRawRules}
                  disabled={disabled}
                  protocolFirst
                  rawJsonValues
                  onChange={handlePayloadOverrideRawRulesChange}
                />
              </SectionSubsection>

              <SectionSubsection
                title={t('config_management.visual.sections.payload.filter_rules')}
                description={t('config_management.visual.sections.payload.filter_rules_desc')}
              >
                <PayloadFilterRulesEditor
                  value={values.payloadFilterRules}
                  disabled={disabled}
                  onChange={handlePayloadFilterRulesChange}
                />
              </SectionSubsection>
            </SectionStack>
          </ConfigSection>
        </div>
      </div>

      {shouldRenderFloatingSidebar && typeof document !== 'undefined'
        ? createPortal(
            <div ref={floatingSidebarRef} className={styles.floatingSidebarContainer}>
              <div className={styles.floatingSidebarRail}>{navContent}</div>
            </div>,
            document.body
          )
        : null}
    </div>
  );
}
