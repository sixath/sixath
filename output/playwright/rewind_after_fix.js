async (page) => {
  const deadline = Date.now() + 180000;
  while (Date.now() < deadline) {
    const n = await page.getByRole('button', { name: 'Rewind here' }).count();
    if (n > 0) break;
    await page.waitForTimeout(2000);
  }
  // Give reloadMessages a moment after stream end.
  await page.waitForTimeout(1500);
  const ids = await page.evaluate(() => {
    const out = [];
    for (const el of document.querySelectorAll('.chat-msg')) {
      const role = el.className.includes('user') ? 'user' : el.className.includes('assistant') ? 'assistant' : '?';
      const btn = el.querySelector('button.chat-rewind-btn, button');
      const text = (el.querySelector('.chat-msg-content')?.innerText || '').slice(0, 40);
      out.push({ role, text, hasRewind: !!(btn && /Rewind/.test(btn.textContent || '')) });
    }
    return out;
  });
  // Probe network: what id would rewind use? Intercept by reading React is hard;
  // instead click after installing dialog + fetch spy.
  const calls = [];
  await page.route('**/rewind', async (route) => {
    const req = route.request();
    calls.push({ url: req.url(), post: req.postData() });
    await route.continue();
  });
  page.once('dialog', (d) => d.accept());
  await page.getByRole('button', { name: 'Rewind here' }).first().click();
  await page.waitForTimeout(2500);
  const afterBubble = await page.locator('.chat-msg').count();
  const afterRewind = await page.getByRole('button', { name: 'Rewind here' }).count();
  const err = await page.locator('.error, [class*="error"]').allTextContents().catch(() => []);
  return { ids, calls, afterBubble, afterRewind, err: err.slice(0, 5), url: page.url() };
}
