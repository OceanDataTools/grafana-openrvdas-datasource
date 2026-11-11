import { test, expect } from '@grafana/plugin-e2e';

test('smoke: should render config editor', async ({ createDataSourceConfigPage, readProvisionedDataSource, page }) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  await createDataSourceConfigPage({ type: ds.type });
  await expect(page.getByRole('textbox', { name: 'CDS URL' })).toBeVisible();
  await expect(page.getByRole('spinbutton', { name: 'Update interval (ms)' })).toBeVisible();
  await expect(page.getByRole('switch', { name: /Skip TLS verify/i })).toBeVisible();
});

test('"Save & test" should succeed against the provisioned CachedDataServer', async ({
  gotoDataSourceConfigPage,
  readProvisionedDataSource,
}) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  const configPage = await gotoDataSourceConfigPage(ds.uid);
  await expect(configPage.saveAndTest()).toBeOK();
  await expect(configPage).toHaveAlert('success', { hasText: /Connected to CachedDataServer/ });
});

test('"Save & test" should fail when no URL is configured', async ({
  createDataSourceConfigPage,
  readProvisionedDataSource,
}) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  const configPage = await createDataSourceConfigPage({ type: ds.type });
  await expect(configPage.saveAndTest()).not.toBeOK();
  await expect(configPage).toHaveAlert('error', { hasText: 'CDS URL is not configured' });
});
