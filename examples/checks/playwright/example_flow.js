/**
 * Example Playwright probe script.
 *
 * @param {import('playwright').Page} page - Playwright page object
 * @param {object} context - Probe context with base_url, credentials, log
 */
module.exports = async function(page, context) {
  await page.goto(context.base_url || 'https://example.com');
  await page.waitForLoadState('networkidle');

  const title = await page.title();
  context.log(`Page title: ${title}`);
};
