import { test, expect } from '@grafana/plugin-e2e';

// Verifies the full path CDS -> Go backend -> Grafana Live -> browser using
// the provisioned dashboard. Needs a CDS with live data (CDS_URL in .env).
test('provisioned dashboard receives live stream messages', async ({
  gotoDashboardPage,
  readProvisionedDashboard,
  page,
}) => {
  const channel = 'ds/openrvdas-dev/f/0/S330HeadingTrue';
  let messages = 0;
  page.on('websocket', (ws) => {
    ws.on('framereceived', (frame) => {
      if (typeof frame.payload === 'string' && frame.payload.includes(channel)) {
        messages++;
      }
    });
  });

  const dashboard = await readProvisionedDashboard({ fileName: 'dashboard.json' });
  await gotoDashboardPage(dashboard);

  // The query's own frame (initial data) renders the latest value with its unit.
  await expect(page.getByText('°').first()).toBeVisible();

  await expect.poll(() => messages, { timeout: 15_000 }).toBeGreaterThan(3);
});
