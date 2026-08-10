async (page) => {
  const deadline = Date.now() + 120000;
  while (Date.now() < deadline) {
    if ((await page.getByRole('button', { name: 'Rewind here' }).count()) > 0) break;
    await page.waitForTimeout(1500);
  }
  await page.waitForTimeout(2000);

  const before = await page.locator('.chat-msg').count();
  const beforeText = await page.locator('main').innerText();

  await page.evaluate(() => {
    window.confirm = () => true;
  });

  const calls = [];
  await page.route('**/api/v1/sessions/*/rewind', async (route) => {
    calls.push({ url: route.request().url(), post: route.request().postData() });
    await route.continue();
  });

  await page.getByRole('button', { name: 'Rewind here' }).first().click();
  await page.waitForTimeout(3000);

  const after = await page.locator('.chat-msg').count();
  const afterText = await page.locator('main').innerText();
  const errEls = await page.locator('.chat-error, .error').allTextContents();

  return {
    before,
    after,
    shortened: after < before || afterText.length < beforeText.length * 0.8,
    calls,
    errEls: errEls.slice(0, 3),
    beforeSnippet: beforeText.slice(0, 200),
    afterSnippet: afterText.slice(0, 200),
    url: page.url(),
  };
}
