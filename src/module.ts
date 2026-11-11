import { DataSourcePlugin } from '@grafana/data';
import { DataSource } from './datasource';
import { ConfigEditor } from './components/ConfigEditor';
import { QueryEditor } from './components/QueryEditor';
import { OpenRVDASQuery, OpenRVDASDataSourceOptions } from './types';

export const plugin = new DataSourcePlugin<DataSource, OpenRVDASQuery, OpenRVDASDataSourceOptions>(DataSource)
  .setConfigEditor(ConfigEditor)
  .setQueryEditor(QueryEditor);
