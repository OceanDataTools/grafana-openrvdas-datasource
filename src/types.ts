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

/**
 * How the plugin reaches the CachedDataServer:
 * - `proxy`: through the OpenRVDAS API's authenticated, read-only proxy
 *   (wss://<host>/api/v1/ws/data-server) with an API key.
 * - `direct`: straight to the CDS (ws://<host>:8766), unauthenticated.
 *   Slated for removal in OpenRVDAS v3.
 * Unset means `direct`, so data sources created before this setting existed
 * keep working; the config editor sets `proxy` for new data sources.
 */
export type ConnectionMode = 'proxy' | 'direct';

/** Options configured for each data source instance (stored in jsonData). */
export interface OpenRVDASDataSourceOptions extends DataSourceJsonData {
  connectionMode?: ConnectionMode;
  /** WebSocket URL of the OpenRVDAS API proxy or the CachedDataServer. */
  url?: string;
  /** CDS subscription interval in milliseconds (default 100 = 10 Hz). */
  pollIntervalMs?: number;
  /** Skip TLS certificate verification for wss:// URLs. */
  tlsSkipVerify?: boolean;
}

export const DEFAULT_POLL_INTERVAL_MS = 100;

/** Secrets, stored encrypted by Grafana and only readable by the backend. */
export interface OpenRVDASSecureJsonData {
  /** OpenRVDAS API key with the WEBSOCKET permission on /api/v1/ws/data-server. */
  apiKey?: string;
}
