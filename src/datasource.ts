import { CoreApp, DataSourceInstanceSettings, ScopedVars } from '@grafana/data';
import { DataSourceWithBackend, getTemplateSrv } from '@grafana/runtime';

import { DEFAULT_QUERY, OpenRVDASDataSourceOptions, OpenRVDASQuery } from './types';

/**
 * Almost everything happens in the Go backend. QueryData returns one frame
 * per field tagged with a Grafana Live channel, and DataSourceWithBackend
 * subscribes to those channels automatically, so no custom streaming code is
 * needed here.
 */
export class DataSource extends DataSourceWithBackend<OpenRVDASQuery, OpenRVDASDataSourceOptions> {
  constructor(instanceSettings: DataSourceInstanceSettings<OpenRVDASDataSourceOptions>) {
    super(instanceSettings);
  }

  getDefaultQuery(_: CoreApp): Partial<OpenRVDASQuery> {
    return DEFAULT_QUERY;
  }

  /** Expands dashboard variables; multi-value variables yield several fields. */
  applyTemplateVariables(query: OpenRVDASQuery, scopedVars: ScopedVars): OpenRVDASQuery {
    const srv = getTemplateSrv();
    const fields = (query.fields ?? []).flatMap((f) =>
      srv
        .replace(f, scopedVars, 'csv')
        .split(',')
        .map((s) => s.trim())
        .filter((s) => s.length > 0)
    );
    return { ...query, fields };
  }

  filterQuery(query: OpenRVDASQuery): boolean {
    return !query.hide && (query.fields?.length ?? 0) > 0;
  }

  /** Names of all fields currently in the CachedDataServer cache. */
  async getFields(): Promise<string[]> {
    const res = await this.getResource<{ fields: string[] }>('fields');
    return res.fields ?? [];
  }
}
