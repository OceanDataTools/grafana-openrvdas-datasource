import { test, expect } from '@grafana/plugin-e2e';

test('smoke: should render query editor', async ({ panelEditPage, readProvisionedDataSource }) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  await panelEditPage.datasource.set(ds.name);
  const row = panelEditPage.getQueryEditorRow('A');
  await expect(row.getByPlaceholder('Select or type field names')).toBeVisible();
  await expect(row.getByRole('spinbutton', { name: 'Min interval (ms)' })).toBeVisible();
});

test('field picker lists CachedDataServer fields', async ({ panelEditPage, readProvisionedDataSource, page }) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  await panelEditPage.datasource.set(ds.name);
  const fields = panelEditPage.getQueryEditorRow('A').getByPlaceholder('Select or type field names');
  await fields.click();
  // The list is virtualized, so check the first entry, then filter for one further down.
  await expect(page.getByRole('option', { name: 'AvgMwxAirTemp' })).toBeVisible();
  await fields.fill('S330Head');
  await expect(page.getByRole('option', { name: 'S330HeadingTrue' })).toBeVisible();
});
