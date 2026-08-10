async (page) => {
  await page.evaluate(() => {
    window.confirm = () => true;
  });

  const before = await page.locator('.chat-msg').count();
  const beforeHasRewind = await page.getByRole('button', { name: 'Rewind here' }).count();

  const calls = [];
  let status = 0;
  await page.route('**/api/v1/sessions/*/rewind', async (route) => {
    const req = route.request();
    calls.push({ url: req.url(), post: req.postData() });
    const resp = await route.fetch();
    status = resp.status();
    await route.fulfill({ response: resp });
  });

  if (beforeHasRewind < 1) {
    return { ok: false, reason: 'no_rewind_button', before, beforeHasRewind };
  }

  await page.getByRole('button', { name: 'Rewind here' }).first().click();
  await page.waitForTimeout(2500);

  const after = await page.locator('.chat-msg').count();
  const afterHasRewind = await page.getByRole('button', { name: 'Rewind here' }).count();
  const emptyHint = await page.getByText(/Start a new conversation|Select an Agent/i).count();
  const errText = (await page.locator('.chat-error').allTextContents().catch(() => [])).join(' | ');

  return {
    ok: status === 200 && after < before,
    status,
    before,
    after,
    beforeHasRewind,
    afterHasRewind,
    emptyHint,
    errText,
    calls,
    url: page.url(),
  };
}
