import { DataSourceJsonData } from '@grafana/data';
import { DataQuery } from '@grafana/schema';

/**
 * A query selects one or more CachedDataServer fields. Each field becomes its
 * own frame (a `time` column plus a value column named after the field) and,
 * when streaming, its own Grafana Live channel.
 */
export interface OpenRVDASQuery extends DataQuery {
  /** CDS field names, or patterns using `*` as a wildcard (e.g. `S330*`). */
  fields: string[];
  /** If > 0, stream at most one (the most recent) value per interval. */
  minIntervalMs?: number;
  /** Stream live updates (default). When false, only the latest values are returned. */
  stream?: boolean;
}

export const DEFAULT_QUERY: Partial<OpenRVDASQuery> = {
  fields: [],
  stream: true,
};

/** Options configured for each data source instance (stored in jsonData). */
export interface OpenRVDASDataSourceOptions extends DataSourceJsonData {
  /** CachedDataServer WebSocket URL, e.g. ws://openrvdas.local:8766 */
  url?: string;
  /** CDS subscription interval in milliseconds (default 100 = 10 Hz). */
  pollIntervalMs?: number;
  /** Skip TLS certificate verification for wss:// URLs. */
  tlsSkipVerify?: boolean;
}

export const DEFAULT_POLL_INTERVAL_MS = 100;
