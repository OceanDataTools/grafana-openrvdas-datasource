import React, { ChangeEvent, useEffect } from 'react';
import { Alert, InlineField, InlineSwitch, Input, RadioButtonGroup, SecretInput } from '@grafana/ui';
import { DataSourcePluginOptionsEditorProps } from '@grafana/data';
import {
  ConnectionMode,
  DEFAULT_POLL_INTERVAL_MS,
  OpenRVDASDataSourceOptions,
  OpenRVDASSecureJsonData,
} from '../types';

interface Props extends DataSourcePluginOptionsEditorProps<OpenRVDASDataSourceOptions, OpenRVDASSecureJsonData> {}

const LABEL_WIDTH = 24;

const CONNECTION_OPTIONS: Array<{ label: string; value: ConnectionMode; description: string }> = [
  {
    label: 'OpenRVDAS API',
    value: 'proxy',
    description: 'Authenticated, read-only proxy at /api/v1/ws/data-server. Requires an API key.',
  },
  {
    label: 'Direct (legacy)',
    value: 'direct',
    description: 'Unauthenticated connection straight to the CachedDataServer.',
  },
];

const URL_PLACEHOLDER: Record<ConnectionMode, string> = {
  proxy: 'wss://openrvdas.example.org/api/v1/ws/data-server',
  direct: 'ws://openrvdas.local:8766',
};

/**
 * The effective mode: unset means direct (data sources created before the
 * setting existed), except for brand-new data sources, which start in proxy
 * mode (see the effect below).
 */
export function effectiveMode(jsonData: OpenRVDASDataSourceOptions): ConnectionMode {
  // Mirrors PluginSettings.Mode() in the backend: anything but 'proxy'
  // (including '' from an unset provisioning variable) is direct.
  return jsonData.connectionMode === 'proxy' ? 'proxy' : 'direct';
}

export function ConfigEditor(props: Props) {
  const { onOptionsChange, options } = props;
  const { jsonData, secureJsonFields, secureJsonData } = options;
  const mode = effectiveMode(jsonData);

  // New data sources (no mode and no URL yet) default to the authenticated
  // proxy. Existing ones without a mode keep connecting directly.
  useEffect(() => {
    if (jsonData.connectionMode === undefined && !jsonData.url) {
      onOptionsChange({ ...options, jsonData: { ...jsonData, connectionMode: 'proxy' } });
    }
    // Only on mount: this initializes a new data source.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const update = (patch: Partial<OpenRVDASDataSourceOptions>) =>
    onOptionsChange({ ...options, jsonData: { ...jsonData, ...patch } });

  const onAPIKeyChange = (event: ChangeEvent<HTMLInputElement>) =>
    onOptionsChange({ ...options, secureJsonData: { ...secureJsonData, apiKey: event.target.value } });

  const onAPIKeyReset = () =>
    onOptionsChange({
      ...options,
      secureJsonFields: { ...secureJsonFields, apiKey: false },
      secureJsonData: { ...secureJsonData, apiKey: '' },
    });

  return (
    <>
      <InlineField
        label="Connection"
        labelWidth={LABEL_WIDTH}
        interactive
        tooltip="OpenRVDAS API: connect through the OpenRVDAS web server's authenticated proxy using an API key (recommended). Direct: connect straight to the CachedDataServer without authentication; scheduled for removal in OpenRVDAS v3."
      >
        <RadioButtonGroup<ConnectionMode>
          id="config-editor-connection-mode"
          options={CONNECTION_OPTIONS}
          value={mode}
          onChange={(v) => update({ connectionMode: v })}
        />
      </InlineField>
      <InlineField
        label="URL"
        labelWidth={LABEL_WIDTH}
        interactive
        tooltip={
          mode === 'proxy'
            ? 'WebSocket URL of the OpenRVDAS API data server proxy, e.g. wss://openrvdas.example.org/api/v1/ws/data-server. The Grafana server connects to it, so it must be reachable from the Grafana host, not from browsers.'
            : 'WebSocket URL of the CachedDataServer, e.g. ws://openrvdas.local:8766 (or wss://host/cds-ws through nginx). The Grafana server connects to it, so it must be reachable from the Grafana host, not from browsers.'
        }
      >
        <Input
          id="config-editor-url"
          value={jsonData.url ?? ''}
          placeholder={URL_PLACEHOLDER[mode]}
          width={56}
          onChange={(e: ChangeEvent<HTMLInputElement>) => update({ url: e.target.value })}
        />
      </InlineField>
      {mode === 'proxy' && (
        <InlineField
          label="API key"
          labelWidth={LABEL_WIDTH}
          interactive
          required
          tooltip="An OpenRVDAS API key with the WEBSOCKET permission on /api/v1/ws/data-server, created on the API Keys page of the OpenRVDAS web UI. Stored encrypted; only the Grafana server uses it."
        >
          <SecretInput
            id="config-editor-api-key"
            isConfigured={!!secureJsonFields?.apiKey}
            value={secureJsonData?.apiKey ?? ''}
            placeholder="Enter your OpenRVDAS API key"
            width={56}
            onReset={onAPIKeyReset}
            onChange={onAPIKeyChange}
          />
        </InlineField>
      )}
      <InlineField
        label="Update interval (ms)"
        labelWidth={LABEL_WIDTH}
        interactive
        tooltip="How often the CachedDataServer sends new data. 100 ms = 10 Hz. Panels can request a slower rate per query, but never a faster one."
      >
        <Input
          id="config-editor-poll-interval"
          type="number"
          min={20}
          step={10}
          value={jsonData.pollIntervalMs ?? ''}
          placeholder={String(DEFAULT_POLL_INTERVAL_MS)}
          width={12}
          onChange={(e: ChangeEvent<HTMLInputElement>) => {
            const v = parseInt(e.target.value, 10);
            update({ pollIntervalMs: Number.isFinite(v) ? v : undefined });
          }}
        />
      </InlineField>
      <InlineField
        label="Skip TLS verify"
        labelWidth={LABEL_WIDTH}
        tooltip="Accept self-signed certificates for wss:// URLs."
      >
        <InlineSwitch
          id="config-editor-tls-skip-verify"
          value={jsonData.tlsSkipVerify ?? false}
          onChange={(e) => update({ tlsSkipVerify: e.currentTarget.checked })}
        />
      </InlineField>
      {mode === 'direct' && (
        <Alert severity="warning" title="Unauthenticated connection">
          Anyone who can reach the CachedDataServer can read, and write, its data. Direct access is scheduled for
          removal in OpenRVDAS v3; prefer the OpenRVDAS API connection with an API key.
        </Alert>
      )}
    </>
  );
}
