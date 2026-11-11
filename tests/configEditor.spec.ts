import { test, expect } from '@grafana/plugin-e2e';

test('smoke: new data sources default to the OpenRVDAS API connection', async ({
  createDataSourceConfigPage,
  readProvisionedDataSource,
  page,
}) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  await createDataSourceConfigPage({ type: ds.type });
  await expect(page.getByRole('radio', { name: 'OpenRVDAS API' })).toBeChecked();
  await expect(page.getByRole('textbox', { name: 'URL' })).toBeVisible();
  await expect(page.getByPlaceholder('Enter your OpenRVDAS API key')).toBeVisible();
  await expect(page.getByRole('spinbutton', { name: 'Update interval (ms)' })).toBeVisible();
  await expect(page.getByRole('switch', { name: /Skip TLS verify/i })).toBeVisible();
});

test('direct mode hides the API key and warns', async ({
  createDataSourceConfigPage,
  readProvisionedDataSource,
  page,
}) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  await createDataSourceConfigPage({ type: ds.type });
  await page.getByRole('radio', { name: 'Direct (legacy)' }).check({ force: true });
  await expect(page.getByPlaceholder('Enter your OpenRVDAS API key')).toHaveCount(0);
  await expect(page.getByRole('alert', { name: 'Unauthenticated connection' })).toBeVisible();
});

test('"Save & test" should succeed against the provisioned data source', async ({
  gotoDataSourceConfigPage,
  readProvisionedDataSource,
}) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  const configPage = await gotoDataSourceConfigPage(ds.uid);
  await expect(configPage.saveAndTest()).toBeOK();
  await expect(configPage).toHaveAlert('success', { hasText: /Connected to the (OpenRVDAS API|CachedDataServer)/ });
});

test('"Save & test" should fail when no URL is configured', async ({
  createDataSourceConfigPage,
  readProvisionedDataSource,
}) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  const configPage = await createDataSourceConfigPage({ type: ds.type });
  await expect(configPage.saveAndTest()).not.toBeOK();
  await expect(configPage).toHaveAlert('error', { hasText: 'URL is not configured' });
});

test('"Save & test" should require an API key for the OpenRVDAS API', async ({
  createDataSourceConfigPage,
  readProvisionedDataSource,
  page,
}) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  const configPage = await createDataSourceConfigPage({ type: ds.type });
  await page.getByRole('textbox', { name: 'URL' }).fill('ws://localhost:1/api/v1/ws/data-server');
  await expect(configPage.saveAndTest()).not.toBeOK();
  await expect(configPage).toHaveAlert('error', { hasText: 'API key is required' });
});
