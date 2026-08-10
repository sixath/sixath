async (page) => {
  await page.goto('http://localhost:5173/?agent=e8107fb3-e40a-4207-9d9a-6768847aaf79');
  await page.waitForTimeout(1500);
  await page.getByPlaceholder(/Type a message/i).fill('Reply with exactly one word: fixedok');
  await page.getByRole('button', { name: 'Send' }).click();

  const deadline = Date.now() + 120000;
  while (Date.now() < deadline) {
    if ((await page.getByRole('button', { name: 'Rewind here' }).count()) > 0) break;
    await page.waitForTimeout(1500);
  }
  // allow reloadMessages after done
  await page.waitForTimeout(2500);

  await page.evaluate(() => { window.confirm = () => true; });
  let status = 0;
  let post = '';
  await page.route('**/api/v1/sessions/*/rewind', async (route) => {
    post = route.request().postData() || '';
    const resp = await route.fetch();
    status = resp.status();
    await route.fulfill({ response: resp });
  });

  const before = await page.locator('.chat-msg').count();
  await page.getByRole('button', { name: 'Rewind here' }).first().click();
  await page.waitForTimeout(2500);
  const after = await page.locator('.chat-msg').count();
  const uuid = /[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/i.test(post);

  return {
    ok: status === 200 && uuid && after < before,
    status,
    uuid,
    post,
    before,
    after,
    url: page.url(),
  };
}
