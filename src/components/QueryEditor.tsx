import React, { ChangeEvent, useEffect, useMemo, useState } from 'react';
import { ComboboxOption, InlineField, InlineSwitch, Input, MultiCombobox, Stack } from '@grafana/ui';
import { QueryEditorProps } from '@grafana/data';
import { DataSource } from '../datasource';
import { OpenRVDASDataSourceOptions, OpenRVDASQuery } from '../types';

type Props = QueryEditorProps<DataSource, OpenRVDASQuery, OpenRVDASDataSourceOptions>;

export function QueryEditor({ datasource, query, onChange, onRunQuery }: Props) {
  const [available, setAvailable] = useState<string[]>([]);
  const [loadError, setLoadError] = useState<string | undefined>();

  useEffect(() => {
    let cancelled = false;
    datasource
      .getFields()
      .then((f) => !cancelled && setAvailable(f))
      .catch((e) => !cancelled && setLoadError(e?.data?.error ?? e?.message ?? 'Failed to load fields'));
    return () => {
      cancelled = true;
    };
  }, [datasource]);

  const fields = useMemo(() => query.fields ?? [], [query.fields]);

  const options: Array<ComboboxOption<string>> = useMemo(() => {
    // Keep selected values (custom names and wildcard patterns) selectable
    // even when they are not in the CDS field list.
    const names = new Set([...available, ...fields]);
    return [...names].sort().map((name) => ({ label: name, value: name }));
  }, [available, fields]);

  const run = (q: OpenRVDASQuery) => {
    onChange(q);
    onRunQuery();
  };

  return (
    <Stack gap={1} direction="column">
      <InlineField
        label="Fields"
        labelWidth={16}
        grow
        invalid={!!loadError}
        error={loadError}
        tooltip="CachedDataServer fields to stream. Type a pattern with * (e.g. S330*) to match several fields."
      >
        <MultiCombobox
          id="query-editor-fields"
          aria-label="Fields"
          options={options}
          value={fields}
          createCustomValue
          placeholder="Select or type field names"
          onChange={(selected) => run({ ...query, fields: selected.map((o) => o.value) })}
        />
      </InlineField>
      <Stack gap={1}>
        <InlineField
          label="Min interval (ms)"
          labelWidth={16}
          tooltip="Send at most one value (the most recent) per interval. 0 or empty streams at the data source's full rate."
        >
          <Input
            id="query-editor-min-interval"
            type="number"
            min={0}
            step={50}
            width={10}
            placeholder="0"
            value={query.minIntervalMs ?? ''}
            onChange={(e: ChangeEvent<HTMLInputElement>) => {
              const v = parseInt(e.target.value, 10);
              onChange({ ...query, minIntervalMs: Number.isFinite(v) && v > 0 ? v : undefined });
            }}
            onBlur={onRunQuery}
          />
        </InlineField>
        <InlineField label="Stream" tooltip="Stream live updates. When off, only the latest values are returned.">
          <InlineSwitch
            id="query-editor-stream"
            value={query.stream ?? true}
            onChange={(e) => run({ ...query, stream: e.currentTarget.checked })}
          />
        </InlineField>
      </Stack>
    </Stack>
  );
}
