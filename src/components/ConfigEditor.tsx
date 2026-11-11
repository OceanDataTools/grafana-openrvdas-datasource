import React, { ChangeEvent } from 'react';
import { InlineField, InlineSwitch, Input } from '@grafana/ui';
import { DataSourcePluginOptionsEditorProps } from '@grafana/data';
import { DEFAULT_POLL_INTERVAL_MS, OpenRVDASDataSourceOptions } from '../types';

interface Props extends DataSourcePluginOptionsEditorProps<OpenRVDASDataSourceOptions> {}

const LABEL_WIDTH = 24;

export function ConfigEditor(props: Props) {
  const { onOptionsChange, options } = props;
  const { jsonData } = options;

  const update = (patch: Partial<OpenRVDASDataSourceOptions>) =>
    onOptionsChange({ ...options, jsonData: { ...jsonData, ...patch } });

  return (
    <>
      <InlineField
        label="CDS URL"
        labelWidth={LABEL_WIDTH}
        interactive
        tooltip="WebSocket URL of the OpenRVDAS CachedDataServer, e.g. ws://openrvdas.local:8766. Grafana's server connects to it, so it must be reachable from the Grafana host, not from browsers."
      >
        <Input
          id="config-editor-url"
          value={jsonData.url ?? ''}
          placeholder="ws://openrvdas.local:8766"
          width={40}
          onChange={(e: ChangeEvent<HTMLInputElement>) => update({ url: e.target.value })}
        />
      </InlineField>
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
    </>
  );
}
