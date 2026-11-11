import { DataSourceInstanceSettings } from '@grafana/data';
import { DataSource } from './datasource';
import { OpenRVDASDataSourceOptions, OpenRVDASQuery } from './types';

// A minimal stand-in: loading the real @grafana/runtime in Jest is very slow.
jest.mock('@grafana/runtime', () => ({
  DataSourceWithBackend: class {},
  getTemplateSrv: () => ({
    replace: (s: string) => s.replace('$sensor', 'S330Roll,S330Pitch').replace('$empty', ''),
  }),
}));

const ds = new DataSource({ jsonData: {} } as DataSourceInstanceSettings<OpenRVDASDataSourceOptions>);
const q = (fields: string[], extra: Partial<OpenRVDASQuery> = {}): OpenRVDASQuery => ({ refId: 'A', fields, ...extra });

describe('DataSource', () => {
  it('expands multi-value variables into separate fields', () => {
    expect(ds.applyTemplateVariables(q(['$sensor', 'MwxAirTemp', '$empty']), {}).fields).toEqual([
      'S330Roll',
      'S330Pitch',
      'MwxAirTemp',
    ]);
  });

  it('skips queries with no fields or hidden queries', () => {
    expect(ds.filterQuery(q([]))).toBe(false);
    expect(ds.filterQuery(q(['S330Roll'], { hide: true }))).toBe(false);
    expect(ds.filterQuery(q(['S330Roll']))).toBe(true);
  });
});
